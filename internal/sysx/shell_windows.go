//go:build windows

package sysx

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// Revealing a file goes through the shell API rather than spawning
// `explorer.exe /select,<path>`.
//
// Shelling out cannot be made reliable. exec.Command quotes the whole argv
// element as soon as the path contains a space, producing
//
//	explorer.exe "/select,C:\Program Files\x.mp4"
//
// and Explorer's own command-line parser rejects that form -- without reporting
// any error. It simply opens Documents instead, so the button looked broken while
// the command "succeeded". Measured on Windows 11 with a real existing file:
// every path containing a space failed this way, which covers most real media
// (folders like "Program Files", "我的视频 2024", ...). Passing the path with
// quotes around only the path works, but that relies on undocumented parser
// behaviour.
//
// SHOpenFolderAndSelectItems takes a wide-string path, so no command-line parsing
// is involved, and it spawns no child process at all.
//
// A path that no longer exists is rejected before we get here (see existingPath);
// the shell would otherwise fall back to Documents or Desktop, which is the other
// half of the same confusing symptom.
var (
	shell32 = syscall.NewLazyDLL("shell32.dll")
	ole32   = syscall.NewLazyDLL("ole32.dll")

	procILCreateFromPathW          = shell32.NewProc("ILCreateFromPathW")
	procSHOpenFolderAndSelectItems = shell32.NewProc("SHOpenFolderAndSelectItems")
	procShellExecuteW              = shell32.NewProc("ShellExecuteW")
	procCoInitializeEx             = ole32.NewProc("CoInitializeEx")
	procCoUninitialize             = ole32.NewProc("CoUninitialize")
	procCoTaskMemFree              = ole32.NewProc("CoTaskMemFree")
)

const (
	coinitApartmentThreaded = 0x2
	rpcEChangedMode         = 0x80010106
	swShowNormal            = 1
)

// reveal selects an existing path inside its containing folder in Explorer.
func reveal(path string) error {
	return withCOM(func() error {
		ptr, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return fmt.Errorf("路径无法转换为 UTF-16：%s", path)
		}

		// ILCreateFromPathW returns an ITEMIDLIST we own and must free.
		pidl, _, _ := procILCreateFromPathW.Call(uintptr(unsafe.Pointer(ptr)))
		if pidl == 0 {
			return fmt.Errorf("资源管理器无法解析该路径：%s", path)
		}
		defer procCoTaskMemFree.Call(pidl)

		// cidl 0 with a nil apidl means "select the item the pidl points at".
		if hr := hresult(procSHOpenFolderAndSelectItems, pidl, 0, 0, 0); hr != 0 {
			return fmt.Errorf("无法在资源管理器中定位（0x%08X）：%s", hr, path)
		}
		return nil
	})
}

// openPath opens an existing file or directory with its default handler.
func openPath(path string) error {
	return withCOM(func() error {
		verb, err := syscall.UTF16PtrFromString("open")
		if err != nil {
			return err
		}
		target, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return fmt.Errorf("路径无法转换为 UTF-16：%s", path)
		}

		ret, _, _ := procShellExecuteW.Call(
			0,
			uintptr(unsafe.Pointer(verb)),
			uintptr(unsafe.Pointer(target)),
			0,
			0,
			swShowNormal,
		)
		// ShellExecuteW returns a value greater than 32 on success; anything at or
		// below 32 is an error code.
		if ret <= 32 {
			return fmt.Errorf("无法打开（ShellExecute 返回 %d）：%s", ret, path)
		}
		return nil
	})
}

// withCOM runs fn with COM initialised on the calling thread, which the shell
// APIs require. CoInitializeEx is per-thread state, so the goroutine is pinned to
// its OS thread for the duration and released again afterwards.
func withCOM(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	switch hr := hresult(procCoInitializeEx, 0, coinitApartmentThreaded); hr {
	case 0, 1: // S_OK / S_FALSE -- ours to balance with CoUninitialize.
		defer procCoUninitialize.Call()
	case rpcEChangedMode:
		// The thread already sits in another apartment (WebView2 uses one). The
		// shell APIs still work from it, but that initialisation is not ours to
		// undo, so no matching CoUninitialize here.
	default:
		return fmt.Errorf("CoInitializeEx 失败（0x%08X）", hr)
	}
	return fn()
}

// hresult calls a COM-style function and narrows its 32-bit return value. The
// Windows ABI leaves the upper half of the return register undefined for a 32-bit
// return type, so the raw Call result must be truncated before comparison --
// otherwise a failed HRESULT can look non-zero-padded in error messages.
func hresult(proc *syscall.LazyProc, args ...uintptr) uint32 {
	ret, _, _ := proc.Call(args...)
	return uint32(ret)
}
