package backup

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// ErrAlreadyRunning, aynı işin başka bir çalışması sürerken dönen hatadır.
var ErrAlreadyRunning = errors.New("iş zaten çalışıyor")

// İş kilidi, adlandırılmış bir Windows mutex'idir. Kilidin varlığı mutex
// nesnesinin varlığıdır (sahiplik kullanılmaz; Go'da goroutine'ler iş
// parçacığı değiştirdiği için ReleaseMutex güvenilir değildir). Süreç
// çökerse tutamaç işletim sistemi tarafından kapatılır, artık kilit kalmaz.
// "Global\" öneki sayesinde zamanlanmış görev (SYSTEM, oturum 0) ile
// kullanıcı oturumundaki GUI aynı kilidi görür.
func lockName(job string) (*uint16, error) {
	// İş adları dosya adı olarak kullanıldığı için büyük/küçük harf duyarsızdır.
	return windows.UTF16PtrFromString(`Global\SqlBackup.Job.` + strings.ToLower(job))
}

// jobLock, tutulan bir iş kilididir.
type jobLock struct{ h windows.Handle }

// acquireJobLock, iş kilidini alır. Kilit başka bir süreçte (veya aynı
// süreçte) tutuluyorsa ErrAlreadyRunning döner.
func acquireJobLock(job string) (*jobLock, error) {
	name, err := lockName(job)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	switch {
	case errors.Is(err, windows.ERROR_ALREADY_EXISTS):
		windows.CloseHandle(h)
		return nil, ErrAlreadyRunning
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		// Mutex başka bir hesapla (ör. SYSTEM) oluşturulmuş ve bize açık değil.
		return nil, ErrAlreadyRunning
	case err != nil:
		return nil, fmt.Errorf("iş kilidi alınamadı: %w", err)
	}
	return &jobLock{h: h}, nil
}

// release, kilidi bırakır.
func (l *jobLock) release() {
	if l != nil && l.h != 0 {
		windows.CloseHandle(l.h)
		l.h = 0
	}
}

// IsRunning, işin şu anda (herhangi bir süreçte) çalışıp çalışmadığını
// döner. Arayüz "Şimdi çalıştır" öncesi uyarı ve durum gösterimi için kullanır.
func IsRunning(job string) bool {
	name, err := lockName(job)
	if err != nil {
		return false
	}
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if err == nil {
		windows.CloseHandle(h)
		return true
	}
	return errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
