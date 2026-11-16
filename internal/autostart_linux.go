//go:build !darwin && !windows

package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

func getXDGAutostartDesktopPath() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configHome = filepath.Join(home, ".config")
	}
	dir := filepath.Join(configHome, "autostart")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "tinydrop.desktop"), nil
}

func checkAutostart() bool {
	p, err := getXDGAutostartDesktopPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func enableAutostart(target string) error {
	p, err := getXDGAutostartDesktopPath()
	if err != nil {
		return err
	}
	desktopContent := fmt.Sprintf(`[Desktop Entry]
Type=Application
Version=1.0
Name=TinyDrop
Comment=TinyDrop Cross-Platform File & Text Sharing Utility
Exec="%s"
Terminal=false
Categories=Utility;Network;
StartupNotify=false
`, target)
	return os.WriteFile(p, []byte(desktopContent), 0644)
}

func disableAutostart() error {
	p, err := getXDGAutostartDesktopPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
