// Package app, arayüzün (Wails) backend köprüsüdür. App'in dışa açık
// yöntemleri frontend'e bağlanır (frontend/wailsjs/go/app/App). Yaşam
// döngüsü işlevleri (Startup, BeforeClose, Shutdown) paket düzeyindedir; App'in yöntemi
// olsalardı frontend'e de açılırlardı.
//
// Hatalar Wails üzerinden frontend'e metin olarak gider (Promise reddi);
// i18n hataları seçili dilde üretilir. Birden çok doğrulama hatası olan
// işlemler hataları liste olarak sonuçta döner.
package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/cli"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/scheduler"
)

// App, frontend'e bağlanan backend yapısıdır.
type App struct {
	ctx        context.Context
	version    string
	startupErr error

	// Dış dünya; testlerde değiştirilir.
	emit    func(event string, data any)
	connect backup.ConnectFunc // nil ise mssql
	now     func() time.Time
	tasks   scheduler.Service
	elevate func(args []string) (code int, output string, err error)
	user    string // "current" görev hesabı; boşsa oturumdaki kullanıcı
	confirm func(title, message string) bool

	unsaved atomic.Bool // iş düzenleyicide kaydedilmemiş değişiklik (SetUnsavedChanges)

	mu      sync.Mutex
	running map[string]runningJob // küçük harf iş adı → çalışma
	wg      sync.WaitGroup
}

// New, App oluşturur. version, CLI ile aynı sürüm bilgisidir.
func New(version string) *App {
	a := &App{
		version: version,
		emit:    func(string, any) {},
		now:     time.Now,
		tasks:   scheduler.System{},
		elevate: func(args []string) (int, string, error) { return cli.RunElevated(args, nil) },
		running: map[string]runningJob{},
	}
	a.confirm = a.confirmDialog
	return a
}

// Startup, Wails OnStartup işlevidir: olayları bağlar, veri klasörlerini
// oluşturur ve dili ayarlardan yükler; ayarda dil yoksa Windows diline
// uyan dil, o da yoksa İngilizce seçilir.
func Startup(a *App) func(context.Context) {
	return func(ctx context.Context) {
		a.ctx = ctx
		a.emit = func(event string, data any) { runtime.EventsEmit(ctx, event, data) }
		s, err := config.LoadSettings()
		a.startupErr = errors.Join(config.EnsureDirs(), err)
		code := s.Language
		if code == "" {
			code = i18n.Detect()
		}
		_ = i18n.SetLanguage(code)
	}
}

// Shutdown, Wails OnShutdown işlevidir: çalışan yedekleri iptal eder ve
// çalışma kayıtları yazılana kadar bekler.
func Shutdown(a *App) func(context.Context) {
	return func(context.Context) {
		a.mu.Lock()
		for _, r := range a.running {
			r.cancel()
		}
		a.mu.Unlock()
		a.wg.Wait()
	}
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// AppInfo, arayüzün genel bilgileridir.
type AppInfo struct {
	Version  string `json:"version"`
	DataDir  string `json:"dataDir"`
	JobsDir  string `json:"jobsDir"`
	LogsDir  string `json:"logsDir"`
	Elevated bool   `json:"elevated"`
	// StartupError, açılışta veri klasörü/ayarlarla ilgili sorundur; boşsa yok.
	StartupError string `json:"startupError"`
}

// GetAppInfo, sürüm, klasörler ve açılış hatasını döner.
func (a *App) GetAppInfo() AppInfo {
	info := AppInfo{Version: a.version, DataDir: config.DataDir(), JobsDir: config.JobsDir(),
		LogsDir: config.LogsDir(), Elevated: scheduler.IsElevated()}
	if a.startupErr != nil {
		info.StartupError = a.startupErr.Error()
	}
	return info
}

// LanguageState, frontend'in dil seçimi için ihtiyaç duyduğu bilgilerdir.
type LanguageState struct {
	Current   string              `json:"current"`
	Languages []i18n.LanguageInfo `json:"languages"`
}

// GetLanguages, seçili dili ve lang klasöründeki kullanılabilir dilleri döner.
func (a *App) GetLanguages() LanguageState {
	return LanguageState{Current: i18n.Language(), Languages: i18n.Languages()}
}

// GetTranslations, seçili dilin tüm metinlerini döner (eksikler İngilizce'den).
func (a *App) GetTranslations() map[string]string {
	return i18n.All()
}

// SetLanguage, dili değiştirir ve ayarlar dosyasına kaydeder; böylece
// zamanlanmış görevler (CLI) da aynı dili kullanır. Frontend ardından
// GetTranslations ile metinleri yeniden yükleyip ekranı yeniden çizer.
func (a *App) SetLanguage(code string) error {
	if err := i18n.SetLanguage(code); err != nil {
		return err
	}
	s, err := config.LoadSettings()
	if err != nil {
		return err
	}
	s.Language = i18n.Language()
	return config.SaveSettings(s)
}

// GetSettings, uygulama ayarlarını döner.
func (a *App) GetSettings() (config.Settings, error) {
	return config.LoadSettings()
}

// SaveSettings, uygulama ayarlarını kaydeder; dil değiştiyse uygulanır.
// Dil boşsa boş kalır (açılışta Windows dili kullanılmaya devam eder).
func (a *App) SaveSettings(s config.Settings) error {
	if s.LogKeepDays < 0 {
		return i18n.Errorf("err.app.keepDays")
	}
	if s.Theme != "" && s.Theme != config.ThemeDark && s.Theme != config.ThemeLight {
		return i18n.Errorf("err.app.theme", "theme", s.Theme)
	}
	if s.Language != "" {
		if s.Language != i18n.Language() {
			if err := i18n.SetLanguage(s.Language); err != nil {
				return err
			}
		}
		s.Language = i18n.Language()
	}
	return config.SaveSettings(s)
}

// errorList, errors.Join ile birleştirilmiş hataları metin listesine açar.
// i18n.Error da Unwrap() []error sağlar (argümanlarındaki hatalar); onun
// metni bütündür, açılmaz.
func errorList(err error) []string {
	if err == nil {
		return nil
	}
	var out []string
	if _, isText := err.(*i18n.Error); isText {
		return []string{err.Error()}
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range j.Unwrap() {
			out = append(out, errorList(e)...)
		}
		return out
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
