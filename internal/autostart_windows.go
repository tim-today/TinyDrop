//go:build windows

package internal

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32         = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyEx = advapi32.NewProc("RegOpenKeyExW")
	procRegCloseKey  = advapi32.NewProc("RegCloseKey")
	procRegQueryValue = advapi32.NewProc("RegQueryValueExW")
	procRegSetValue   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValue = advapi32.NewProc("RegDeleteValueW")
)

const (
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	keyWrite        = 0x20006
	regSZ           = 1
	runSubKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
)

func checkAutostart() bool {
	var hKey syscall.Handle
	subKeyPtr, _ := syscall.UTF16PtrFromString(runSubKey)
	ret, _, _ := procRegOpenKeyEx.Call(
		uintptr(hkeyCurrentUser),
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		uintptr(keyRead),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return false
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	namePtr, _ := syscall.UTF16PtrFromString(AutostartName)
	var valType uint32
	var bufSize uint32
	ret, _, _ = procRegQueryValue.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(namePtr)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	return ret == 0
}

func enableAutostart(target string) error {
	var hKey syscall.Handle
	subKeyPtr, _ := syscall.UTF16PtrFromString(runSubKey)
	ret, _, _ := procRegOpenKeyEx.Call(
		uintptr(hkeyCurrentUser),
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		uintptr(keyWrite),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return fmt.Errorf("failed to open registry Run key: error code %d", ret)
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	// Quote path in case target has spaces
	cmdVal := target
	if !strings.HasPrefix(cmdVal, "\"") {
		cmdVal = `"` + cmdVal + `"`
	}

	namePtr, _ := syscall.UTF16PtrFromString(AutostartName)
	valUTF16, _ := syscall.UTF16FromString(cmdVal)

	ret, _, _ = procRegSetValue.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(namePtr)),
		0,
		uintptr(regSZ),
		uintptr(unsafe.Pointer(&valUTF16[0])),
		uintptr(len(valUTF16)*2),
	)
	if ret != 0 {
		return fmt.Errorf("failed to set registry Run value: error code %d", ret)
	}
	return nil
}

func disableAutostart() error {
	var hKey syscall.Handle
	subKeyPtr, _ := syscall.UTF16PtrFromString(runSubKey)
	ret, _, _ := procRegOpenKeyEx.Call(
		uintptr(hkeyCurrentUser),
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		uintptr(keyWrite),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return nil
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	namePtr, _ := syscall.UTF16PtrFromString(AutostartName)
	_, _, _ = procRegDeleteValue.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(namePtr)),
	)
	return nil
}
