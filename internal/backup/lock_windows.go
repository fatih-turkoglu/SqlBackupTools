package backup

import (
	"errors"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
)

// ErrAlreadyRunning, aynı işin başka bir çalışması sürerken dönen hatadır.
var ErrAlreadyRunning = i18n.Errorf("err.backup.lockHeld")

// İş kilidi, adlandırılmış bir Windows mutex'idir. Kilidin varlığı mutex
// nesnesinin varlığıdır (sahiplik kullanılmaz; Go'da goroutine'ler iş
// parçacığı değiştirdiği için ReleaseMutex güvenilir değildir). Süreç
// çökerse tutamaç işletim sistemi tarafından kapatılır, artık kilit kalmaz.
// "Global\" öneki sayesinde zamanlanmış görev (SYSTEM, oturum 0) ile
// kullanıcı oturumundaki GUI aynı kilidi görür.
//
// Ad veri klasörünün özetini de içerir: farklı veri klasörlerindeki
// (SQLBACKUP_HOME, taşınabilir kurulum, testler) aynı adlı işler farklı işlerdir.
func lockName(job string) (*uint16, error) {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(filepath.Clean(config.DataDir()))))
	// İş adları dosya adı olarak kullanıldığı için büyük/küçük harf duyarsızdır.
	return windows.UTF16PtrFromString(fmt.Sprintf(`Global\SqlBackup.Job.%08x.%s`, h.Sum32(), strings.ToLower(job)))
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
		return nil, i18n.Errorf("err.backup.lock", "err", err)
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
