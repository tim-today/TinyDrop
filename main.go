package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tim-today/TinyDrop/internal"
)

var Version = "v1.0.4"

func init() {
	runtime.LockOSThread()
}

func main() {
	portFlag := flag.Int("port", 0, "Custom HTTP share port (default 20999)")
	noBrowser := flag.Bool("no-browser", false, "Do not auto-open browser on startup")
	daemonGuard := flag.Bool("guard", false, "Enable process supervisor auto-restart mode")
	showVer := flag.Bool("version", false, "Print version information")
	flag.Parse()

	if *showVer {
		fmt.Printf("TinyDrop (TD) %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		return
	}

	if *daemonGuard && os.Getenv("TINYDROP_WORKER") != "1" {
		runProcessSupervisor()
		return
	}

	cm, err := internal.NewConfigManager()
	if err != nil {
		log.Fatalf("Failed to initialize config: %v", err)
	}

	if *portFlag > 0 && *portFlag <= 65535 {
		_ = cm.Update(func(c *internal.Config) {
			c.Port = *portFlag
		})
	}

	sm := internal.NewServerManager(cm)
	sm.AppVersion = Version
	sm.StartAndKeepAlive()

	cfg := cm.GetSnapshot()
	adminURL := fmt.Sprintf("http://127.0.0.1:%d/api/admin/entry?secret=%s", cfg.Port, cfg.AdminSecret)

	addrs := internal.GetNetworkAddresses(cfg.Port)
	log.Println("====================================================")
	log.Printf("💧 TinyDrop (TD) %s started!", Version)
	log.Printf("🔑 Share PIN Code: %s", cfg.Password)
	log.Printf("📂 Default Temp Folder: %s", cfg.TempDir)
	for _, a := range addrs {
		log.Printf("🌐 [%s] Client URL: %s", a.Name, a.URL)
	}
	log.Printf("🖥️ Local Host Admin URL: %s", adminURL)
	log.Println("====================================================")

	if !*noBrowser {
		go func() {
			time.Sleep(450 * time.Millisecond)
			_ = internal.OpenBrowser(adminURL)
		}()
	}

	internal.RunSystemTray(sm)
}

func runProcessSupervisor() {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	exe, _ = filepath.Abs(exe)

	for {
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Env = append(os.Environ(), "TINYDROP_WORKER=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err == nil {
			return
		}
		log.Printf("[Supervisor] Process exited unexpectedly (%v), restarting in 2s...", err)
		time.Sleep(2 * time.Second)
	}
}
