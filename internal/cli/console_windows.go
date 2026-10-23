package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachParentProcess, AttachConsole için (DWORD)-1 değeridir.
const attachParentProcess = ^uint32(0)

// AttachConsole, GUI alt sistemiyle derlenmiş exe komut satırından
// çalıştırıldığında çıktının görünmesi için üst sürecin konsoluna bağlanır.
// Standart çıktı zaten geçerliyse (dosyaya/boruya yönlendirilmiş veya konsol
// uygulaması olarak derlenmiş) dokunulmaz. Üst süreçte konsol yoksa
// (zamanlanmış görev, Gezgin) hiçbir şey yapılmaz; çıktı log dosyasındadır.
//
// Not: GUI exe'yi etkileşimli cmd beklemez, çıktı komut istemiyle karışabilir;
// beklemek için "start /wait SqlBackup.exe run ..." kullanılmalı. Batch
// dosyaları ise programın bitmesini bekler ve %ERRORLEVEL% doğru gelir.
func AttachConsole() bool {
	outOK, errOK := validStdHandle(windows.STD_OUTPUT_HANDLE), validStdHandle(windows.STD_ERROR_HANDLE)
	if outOK && errOK {
		return false
	}
	if r, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); r == 0 {
		return false
	}
	if !outOK {
		if f := openConsoleOut(); f != nil {
			os.Stdout = f
		}
	}
	if !errOK {
		if f := openConsoleOut(); f != nil {
			os.Stderr = f
		}
	}
	return true
}

func validStdHandle(id uint32) bool {
	h, err := windows.GetStdHandle(id)
	if err != nil || h == 0 || h == windows.InvalidHandle {
		return false
	}
	t, _ := windows.GetFileType(h)
	return t != windows.FILE_TYPE_UNKNOWN
}

func openConsoleOut() *os.File {
	name, _ := windows.UTF16PtrFromString("CONOUT$")
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil
	}
	// os.NewFile konsol tutamacını tanır ve UTF-8 metni WriteConsoleW ile
	// yazar; Türkçe karakterler kod sayfasından bağımsız doğru görünür.
	return os.NewFile(uintptr(h), "CONOUT$")
}

// isConsole, f'nin bir konsol olup olmadığını döner.
func isConsole(f *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}
