// Command ffmpeggui is a Wails desktop front-end for ffmpeg / ffprobe.
package main

import (
	"embed"
	"flag"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"ffmpeggui/internal/store"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

func main() {
	var dataDir string
	flag.StringVar(&dataDir, "data", "", "数据目录（默认为程序同目录下的 data 文件夹）")
	flag.Parse()

	if dataDir != "" {
		store.SetDataDir(dataDir)
	}

	// Identifies this program for the single-instance mutex. Keep it stable
	// across versions: changing it is how you end up with two copies running.
	const singleInstanceID = "ffmpeggui"

	app := NewApp(trayIcon)

	// Read the settings here, not in OnStartup: whether the window may appear at
	// all is a *creation* option. By the time OnStartup fires Wails has already
	// shown the window, so hiding it from there is a race the user sees as a
	// flash.
	launch := store.LoadSettings()

	err := wails.Run(&options.App{
		Title:     "FFmpeg GUI",
		Width:     1340,
		Height:    880,
		MinWidth:  1060,
		MinHeight: 680,
		// Only one copy at a time. A second launch hands its arguments over to
		// the running one and exits before it ever builds a window, so there is
		// no second window to flicker and no second tray icon.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               singleInstanceID,
			OnSecondInstanceLaunch: app.onSecondInstance,
		},
		// StartHidden is the only way to launch without the window ever being
		// on screen. It needs the tray: a hidden window with no way back is an
		// unreachable program, so a lone StartMinimized is ignored.
		StartHidden: launch.StartMinimized && launch.EnableTray,
		// No system title bar: the window draws its own, so the chrome matches the
		// rest of the UI instead of being a light Windows strip above a dark app.
		// The decorations stay on (they are what keeps the drop shadow and the
		// resize border), and the app's own bar carries --wails-draggable:drag.
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 17, G: 19, B: 24, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
		},
		Bind: []interface{}{app},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			// Defensive hardening for managed machines. Endpoint-protection
			// software injects unsigned DLLs into WebView2's child processes and
			// Chromium's RendererCodeIntegrity then terminates them, which
			// surfaces as a black, unresponsive window topped by
			// "The WebView2 process crashed and the application needs to be
			// restarted." This flag keeps integrity checks off our embedded,
			// purely local UI; nothing is ever loaded from the network.
			WebviewDisableRendererCodeIntegrity: true,
			Theme:                               windows.Dark,
		},
	})
	if err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}
