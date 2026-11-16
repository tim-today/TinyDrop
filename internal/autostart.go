package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AutostartName is the identifier used across system autostart configurations.
const AutostartName = "TinyDrop"

// GetExecutableTarget returns the preferred path to launch TinyDrop on startup.
// On macOS inside an .app bundle (/Applications/TinyDrop.app/Contents/MacOS/TinyDrop),
// it resolves to the parent .app bundle (/Applications/TinyDrop.app) so macOS launches it cleanly.
func GetExecutableTarget() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", err
	}

	if runtime.GOOS == "darwin" {
		// If binary is within a .app bundle, e.g. /Applications/TinyDrop.app/Contents/MacOS/TinyDrop
		parts := filepath.SplitList(abs)
		_ = parts
		dir := abs
		for {
			if filepath.Ext(dir) == ".app" {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir || parent == "." || parent == "/" {
				break
			}
			dir = parent
		}
	}

	return abs, nil
}

// IsAutostartEnabled checks if TinyDrop is currently registered to launch on system boot.
func IsAutostartEnabled() bool {
	return checkAutostart()
}

// SetAutostart enables or disables system autostart.
func SetAutostart(enable bool) error {
	if enable {
		target, err := GetExecutableTarget()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		return enableAutostart(target)
	}
	return disableAutostart()
}
