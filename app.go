package main

import (
	"context"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
)

// App, frontend'e bağlanan (Wails Bind) backend yapısıdır.
type App struct {
	ctx context.Context
}

// NewApp, App oluşturur.
func NewApp() *App {
	return &App{}
}

// startup, uygulama açılırken çağrılır. Dil ayarlar dosyasından yüklenir;
// sorun olursa İngilizce ile devam edilir.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	s, _ := config.LoadSettings()
	_ = i18n.SetLanguage(s.Language)
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
