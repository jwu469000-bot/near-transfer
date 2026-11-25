package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	crypt32  = syscall.NewLazyDLL("crypt32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	getDiskFreeSpace = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func wide(value string) *uint16 {
	result, _ := syscall.UTF16PtrFromString(value)
	return result
}

func wideString(value *uint16) string {
	if value == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(value), 2*n)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(value, n))
}

func availableDiskBytes(path string) (uint64, error) {
	pathPointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	ok, _, callErr := getDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(pathPointer)),
		uintptr(unsafe.Pointer(&available)),
		0, 0,
	)
	if ok == 0 {
		return 0, callErr
	}
	return available, nil
}

func openPath(path string) error {
	result, _, _ := shell32.NewProc("ShellExecuteW").Call(
		0, uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(path))), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("Windows 无法打开指定位置 (%d)", result)
	}
	return nil
}

func downloadsFolder() (string, error) {
	downloads := syscall.GUID{Data1: 0x374de290, Data2: 0x123f, Data3: 0x4565, Data4: [8]byte{0x91, 0x64, 0x39, 0xc4, 0x92, 0x5e, 0x46, 0x7b}}
	var path *uint16
	result, _, _ := shell32.NewProc("SHGetKnownFolderPath").Call(
		uintptr(unsafe.Pointer(&downloads)), 0x4000, 0, uintptr(unsafe.Pointer(&path)))
	if path != nil {
		defer ole32.NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(path)))
	}
	if result != 0 || path == nil {
		return "", fmt.Errorf("Windows 无法定位下载目录 (0x%x)", uint32(result))
	}
	return wideString(path), nil
}

func moveFileExclusive(source, destination string) error {
	fullName := func(path string) string {
		if len(path) < 240 {
			return path
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return path
		}
		if len(absolute) >= 2 && absolute[:2] == `\\` {
			return `\\?\UNC\` + absolute[2:]
		}
		return `\\?\` + absolute
	}
	result, _, err := kernel32.NewProc("MoveFileW").Call(
		uintptr(unsafe.Pointer(wide(fullName(source)))), uintptr(unsafe.Pointer(wide(fullName(destination)))))
	if result == 0 {
		return err
	}
	return nil
}

type dataBlob struct {
	size uint32
	data *byte
}

func protectData(input []byte, encrypt bool) ([]byte, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("设备绑定文件为空")
	}
	in := dataBlob{size: uint32(len(input)), data: &input[0]}
	out := dataBlob{}
	method := "CryptUnprotectData"
	if encrypt {
		method = "CryptProtectData"
	}
	result, _, err := crypt32.NewProc(method).Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if result == 0 {
		return nil, fmt.Errorf("无法访问已保存的设备绑定: %w", err)
	}
	defer kernel32.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(out.data)))
	return append([]byte(nil), unsafe.Slice(out.data, out.size)...), nil
}

const startupKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const startupName = "NearTransfer"

func startupEnabled() bool {
	var key syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, wide(startupKey), 0, syscall.KEY_QUERY_VALUE, &key) != nil {
		return false
	}
	defer syscall.RegCloseKey(key)
	var size uint32
	var kind uint32
	err := syscall.RegQueryValueEx(key, wide(startupName), nil, &kind, nil, &size)
	if err != nil || kind != syscall.REG_SZ || size < 2 || size > 65536 {
		return false
	}
	value := make([]uint16, (size+1)/2)
	err = syscall.RegQueryValueEx(key, wide(startupName), nil, &kind, (*byte)(unsafe.Pointer(&value[0])), &size)
	exe, exeErr := os.Executable()
	return err == nil && exeErr == nil && syscall.UTF16ToString(value) == "\""+exe+"\""
}

func setStartup(enabled bool) error {
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, wide(startupKey), 0, syscall.KEY_SET_VALUE, &key); err != nil {
		return err
	}
	defer syscall.RegCloseKey(key)
	name := wide(startupName)
	if !enabled {
		result, _, _ := advapi32.NewProc("RegDeleteValueW").Call(uintptr(key), uintptr(unsafe.Pointer(name)))
		if result == 0 || result == 2 {
			return nil
		}
		return syscall.Errno(result)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	value, err := syscall.UTF16FromString("\"" + exe + "\"")
	if err != nil {
		return err
	}
	result, _, _ := advapi32.NewProc("RegSetValueExW").Call(
		uintptr(key), uintptr(unsafe.Pointer(name)), 0, syscall.REG_SZ, uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)*2))
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}
