//go:build windows

package internal

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

//go:embed assets/icon.ico
var winEmbeddedICO []byte

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	shell32                   = syscall.NewLazyDLL("shell32.dll")
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessageW      = user32.NewProc("DispatchMessageW")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenuW           = user32.NewProc("AppendMenuW")
	procTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	procDestroyMenu           = user32.NewProc("DestroyMenu")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procSetForegroundWnd      = user32.NewProc("SetForegroundWindow")
	procLoadIconW             = user32.NewProc("LoadIconW")
	procCreateIconFromResEx   = user32.NewProc("CreateIconFromResourceEx")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procShellNotifyIconW      = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmUserTray    = 0x0400 + 1
	wmCommand     = 0x0111
	wmDestroy     = 0x0002
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	nimAdd        = 0x00000000
	nimDelete     = 0x00000002
	nifMessage    = 0x00000001
	nifIcon       = 0x00000002
	nifTip        = 0x00000004
	idiApplication = 32512
	mfString      = 0x00000000
	mfSeparator   = 0x00000800
	tpmBottomAlign = 0x0020
	tpmLeftAlign   = 0x0000
)

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

type notifyIconDataW struct {
	Size            uint32
	Wnd             syscall.Handle
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            syscall.Handle
	Tip             [128]uint16
}

type point struct {
	X, Y int32
}

type msg struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

var winTraySM *ServerManager
var winHwnd syscall.Handle
var nid notifyIconDataW

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func showWinTrayMenu(hwnd syscall.Handle) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	cfg := winTraySM.CM.GetSnapshot()
	shareText := "✅ Sharing: Enabled (Click to Toggle)"
	if !cfg.ShareEnabled {
		shareText = "⛔ Sharing: Disabled (Click to Enable)"
	}
	editText := "🔒 Client Access: Read-Only (Click to Toggle)"
	if cfg.AllowClientEdit {
		editText = "✏️ Client Access: Read & Write (Click to Toggle)"
	}

	procAppendMenuW.Call(hMenu, mfString, 1, uintptr(unsafe.Pointer(utf16Ptr("🖥️ Open TinyDrop Dashboard"))))
	procAppendMenuW.Call(hMenu, mfString, 2, uintptr(unsafe.Pointer(utf16Ptr("📂 Open Temp Share Folder"))))
	procAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	procAppendMenuW.Call(hMenu, mfString, 3, uintptr(unsafe.Pointer(utf16Ptr("➕ Add Files to Share..."))))
	procAppendMenuW.Call(hMenu, mfString, 4, uintptr(unsafe.Pointer(utf16Ptr("📁 Add Folders to Share..."))))
	procAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	procAppendMenuW.Call(hMenu, mfString, 5, uintptr(unsafe.Pointer(utf16Ptr(shareText))))
	procAppendMenuW.Call(hMenu, mfString, 6, uintptr(unsafe.Pointer(utf16Ptr(editText))))
	procAppendMenuW.Call(hMenu, mfString, 7, uintptr(unsafe.Pointer(utf16Ptr(fmt.Sprintf("🔑 Current PIN: %s (Click to Reset)", cfg.Password)))))
	procAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	procAppendMenuW.Call(hMenu, mfString, 99, uintptr(unsafe.Pointer(utf16Ptr("Quit TinyDrop"))))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWnd.Call(uintptr(hwnd))
	procTrackPopupMenu.Call(hMenu, tpmBottomAlign|tpmLeftAlign, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
}

func handleWinCommand(cmdID int) {
	if winTraySM == nil {
		return
	}
	cfg := winTraySM.CM.GetSnapshot()
	adminURL := fmt.Sprintf("http://127.0.0.1:%d/api/admin/entry?secret=%s", cfg.Port, cfg.AdminSecret)

	switch cmdID {
	case 1:
		_ = OpenBrowser(adminURL)
	case 2:
		_ = os.MkdirAll(cfg.TempDir, 0755)
		_ = OpenInFileManager(cfg.TempDir, true)
	case 3:
		go func() {
			paths, _ := PickSystemPaths(false)
			if len(paths) > 0 {
				_, _ = winTraySM.addLocalPathsToShare(paths)
				_ = OpenBrowser(adminURL)
			}
		}()
	case 4:
		go func() {
			paths, _ := PickSystemPaths(true)
			if len(paths) > 0 {
				_, _ = winTraySM.addLocalPathsToShare(paths)
				_ = OpenBrowser(adminURL)
			}
		}()
	case 5:
		_ = winTraySM.CM.Update(func(c *Config) { c.ShareEnabled = !c.ShareEnabled })
	case 6:
		_ = winTraySM.CM.Update(func(c *Config) { c.AllowClientEdit = !c.AllowClientEdit })
	case 7:
		_ = winTraySM.CM.Update(func(c *Config) {
			c.Password = Generate6DigitPassword()
			c.PasswordVersion = GenerateRandomHex(8)
		})
		_ = OpenBrowser(adminURL)
	case 99:
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		procPostQuitMessage.Call(0)
		os.Exit(0)
	}
}

func wndProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmUserTray:
		if lParam == wmLButtonUp {
			handleWinCommand(1)
		} else if lParam == wmRButtonUp {
			showWinTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		handleWinCommand(int(wParam & 0xffff))
		return 0
	case wmDestroy:
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

func RunSystemTray(sm *ServerManager) {
	runtime.LockOSThread()
	winTraySM = sm

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("TinyDropTrayClass")

	var wc wndClassExW
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.WndProc = syscall.NewCallback(wndProc)
	wc.Instance = syscall.Handle(hInst)
	wc.ClassName = className
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16Ptr("TinyDrop"))),
		0, 0, 0, 0, 0, 0, 0, hInst, 0,
	)
	winHwnd = syscall.Handle(hwnd)

	// Load embedded RT_GROUP_ICON (resource ID 1) or create from embedded ICO bytes
	hIcon, _, _ := procLoadIconW.Call(hInst, 1)
	if hIcon == 0 && len(winEmbeddedICO) > 22 {
		// Fallback to CreateIconFromResourceEx using 32x32 or first PNG entry in ICO
		bytesInRes := binary.LittleEndian.Uint32(winEmbeddedICO[14:18])
		imgOffset := binary.LittleEndian.Uint32(winEmbeddedICO[18:22])
		if int(imgOffset+bytesInRes) <= len(winEmbeddedICO) {
			hIcon, _, _ = procCreateIconFromResEx.Call(
				uintptr(unsafe.Pointer(&winEmbeddedICO[imgOffset])),
				uintptr(bytesInRes),
				1,
				0x00030000,
				32, 32,
				0,
			)
		}
	}
	if hIcon == 0 {
		hIcon, _, _ = procLoadIconW.Call(0, uintptr(idiApplication))
	}
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Wnd = winHwnd
	nid.ID = 1
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = wmUserTray
	nid.Icon = syscall.Handle(hIcon)
	tipSlice, _ := syscall.UTF16FromString("TinyDrop (TD) - Click to Open Dashboard")
	copy(nid.Tip[:], tipSlice)

	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
