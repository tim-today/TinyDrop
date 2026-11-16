//go:build darwin

package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const macLaunchAgentPlist = "com.tinydrop.launcher.plist"

func getLaunchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, macLaunchAgentPlist), nil
}

func checkAutostart() bool {
	plistPath, err := getLaunchAgentPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(plistPath)
	return err == nil
}

func enableAutostart(target string) error {
	plistPath, err := getLaunchAgentPath()
	if err != nil {
		return err
	}

	var programArgsXML string
	if strings.HasSuffix(target, ".app") {
		programArgsXML = fmt.Sprintf(`        <string>/usr/bin/open</string>
        <string>-gj</string>
        <string>%s</string>`, target)
	} else {
		programArgsXML = fmt.Sprintf(`        <string>%s</string>`, target)
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.tinydrop.launcher</string>
    <key>ProgramArguments</key>
    <array>
%s
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
    <key>ProcessType</key>
    <string>Interactive</string>
</dict>
</plist>
`, programArgsXML)

	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return err
	}

	// Try loading with launchctl (ignore error if already loaded)
	_ = exec.Command("launchctl", "load", plistPath).Run()
	return nil
}

func disableAutostart() error {
	plistPath, err := getLaunchAgentPath()
	if err != nil {
		return err
	}

	// Unload if running
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
