//go:build darwin

package internal

/*
#cgo CFLAGS: -x objective-c -mmacosx-version-min=11.0
#cgo LDFLAGS: -framework Cocoa -mmacosx-version-min=11.0

void runMacTray(const void *iconBytes, int iconLen);
void stopMacTray(void);
*/
import "C"
import (
	_ "embed"
	"fmt"
	"os"
	"unsafe"
)

//go:embed assets/tray_icon.png
var macTrayIconPNG []byte

var globalTraySM *ServerManager

//export goTrayCallback
func goTrayCallback(actionID C.int) {
	if globalTraySM == nil {
		return
	}
	cfg := globalTraySM.CM.GetSnapshot()
	adminURL := fmt.Sprintf("http://127.0.0.1:%d/api/admin/entry?secret=%s", cfg.Port, cfg.AdminSecret)

	switch int(actionID) {
	case 1:
		_ = OpenBrowser(adminURL)
	case 2:
		_ = os.MkdirAll(cfg.TempDir, 0755)
		_ = OpenInFileManager(cfg.TempDir, true)
	case 3:
		go func() {
			paths, _ := PickSystemPaths(false)
			if len(paths) > 0 {
				_, _ = globalTraySM.addLocalPathsToShare(paths)
				_ = OpenBrowser(adminURL)
			}
		}()
	case 4:
		go func() {
			paths, _ := PickSystemPaths(true)
			if len(paths) > 0 {
				_, _ = globalTraySM.addLocalPathsToShare(paths)
				_ = OpenBrowser(adminURL)
			}
		}()
	case 5:
		_ = globalTraySM.CM.Update(func(c *Config) {
			c.ShareEnabled = !c.ShareEnabled
		})
	case 6:
		_ = globalTraySM.CM.Update(func(c *Config) {
			c.AllowClientEdit = !c.AllowClientEdit
		})
	case 7:
		_ = globalTraySM.CM.Update(func(c *Config) {
			c.Password = Generate6DigitPassword()
			c.PasswordVersion = GenerateRandomHex(8)
		})
		_ = OpenBrowser(adminURL)
	case 99:
		C.stopMacTray()
		os.Exit(0)
	}
}

func RunSystemTray(sm *ServerManager) {
	globalTraySM = sm
	var ptr unsafe.Pointer
	if len(macTrayIconPNG) > 0 {
		ptr = unsafe.Pointer(&macTrayIconPNG[0])
	}
	C.runMacTray(ptr, C.int(len(macTrayIconPNG)))
}
