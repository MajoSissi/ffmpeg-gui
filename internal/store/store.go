// Package store persists all application data (settings, templates, history)
// into the "data" directory that sits next to the executable.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	dirMu   sync.RWMutex
	dataDir string
)

// SetDataDir overrides the directory used for all persisted data. Used by the
// -data command line flag; when empty data lives in <exeDir>/data.
func SetDataDir(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	dirMu.Lock()
	dataDir = abs
	dirMu.Unlock()
	_ = os.MkdirAll(abs, 0o755)
}

// DataDir returns the data directory, creating it on first use.
func DataDir() string {
	dirMu.RLock()
	d := dataDir
	dirMu.RUnlock()
	if d != "" {
		return d
	}

	dirMu.Lock()
	defer dirMu.Unlock()
	if dataDir != "" {
		return dataDir
	}
	base := ""
	if exe, err := os.Executable(); err == nil {
		base = filepath.Dir(exe)
	}
	if base == "" {
		base, _ = os.Getwd()
	}
	dataDir = filepath.Join(base, "data")
	_ = os.MkdirAll(dataDir, 0o755)
	return dataDir
}

// Path resolves a file name inside the data directory.
func Path(name string) string { return filepath.Join(DataDir(), name) }

// ReadJSON loads a JSON document. The bool reports whether the file existed.
func ReadJSON(path string, v any) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return true, err
	}
	return true, nil
}

// WriteJSON marshals v and writes it atomically (temp file + rename) so a crash
// mid-write can never leave a truncated config behind.
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, b)
}

// WriteFileAtomic writes b to path via a temporary file in the same directory.
func WriteFileAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// randHex returns n random bytes as a lowercase hex string.
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(i * 31)
		}
	}
	return hex.EncodeToString(b)
}

// NewID returns a unique identifier for templates and jobs.
func NewID() string { return randHex(16) }

// ShortID returns a compact identifier used for job ids shown in the UI.
func ShortID() string { return randHex(4) }
