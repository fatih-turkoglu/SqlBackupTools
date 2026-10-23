package scheduler

import (
	"errors"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"

	"sqlbackup/internal/i18n"
)

// ErrElevationCancelled, kullanıcının UAC isteğini reddettiğini belirtir.
var ErrElevationCancelled = i18n.Errorf("err.scheduler.elevateCancelled")

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo, SHELLEXECUTEINFOW yapısıdır (x/sys/windows'ta yok).
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.Handle
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	swHide                = 0
)

// IsElevated, sürecin yönetici yetkisiyle (UAC yükseltilmiş) çalışıp çalışmadığını döner.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// RunElevated, exe'yi verilen argümanlarla "runas" ile (UAC isteği) başlatır,
// bitmesini bekler ve çıkış kodunu döner. Yükseltilmiş süreç ortam
// değişkenlerini ve standart çıktıyı devralmaz; gerekli bilgiler argümanla
// verilmeli, çıktı dosya üzerinden alınmalıdır (CLI --home, --output).
func RunElevated(exe string, args []string) (int, error) {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, i18n.Errorf("err.scheduler.elevate", "err", err)
	}
	params, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return 0, i18n.Errorf("err.scheduler.elevate", "err", err)
	}
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		lpDirectory:  dir,
		nShow:        swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(e, windows.ERROR_CANCELLED) {
			return 0, ErrElevationCancelled
		}
		return 0, i18n.Errorf("err.scheduler.elevate", "err", e)
	}
	if info.hProcess == 0 {
		return 0, i18n.Errorf("err.scheduler.elevate", "err", windows.ERROR_INVALID_HANDLE)
	}
	defer windows.CloseHandle(info.hProcess)
	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return 0, i18n.Errorf("err.scheduler.elevate", "err", err)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return 0, i18n.Errorf("err.scheduler.elevate", "err", err)
	}
	return int(code), nil
}
