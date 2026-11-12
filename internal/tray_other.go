//go:build !darwin && !windows

package internal

func RunSystemTray(sm *ServerManager) {
	select {}
}
