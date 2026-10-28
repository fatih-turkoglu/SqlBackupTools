package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"sqlbackup/internal/i18n"
)

// DefaultLogKeepDays, log ve çalışma kayıtlarının varsayılan saklama süresidir.
const DefaultLogKeepDays = 30

// Arayüz temaları; boş tema koyu sayılır.
const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// Settings, işlerden bağımsız uygulama ayarlarıdır (settings.json). CLI
// (zamanlanmış görev) ve GUI aynı dosyayı kullanır.
type Settings struct {
	// Language, arayüz ve CLI dili (lang\<kod>.lng); boşsa İngilizce.
	Language string `json:"language"`
	// LogKeepDays, log ve çalışma kayıtlarının saklama süresi (gün); 0 = varsayılan.
	LogKeepDays int `json:"logKeepDays"`
	// Theme, arayüz teması (ThemeDark/ThemeLight); boşsa koyu. CLI kullanmaz.
	Theme string `json:"theme"`
	// DefaultServerPath / DefaultAccessPath, yeni işlerde önerilen yedek
	// klasörleridir (Backup.ServerPath / AccessPath). CLI kullanmaz.
	DefaultServerPath string `json:"defaultServerPath"`
	DefaultAccessPath string `json:"defaultAccessPath"`
}

// SettingsPath, ayarlar dosyasının yoludur.
func SettingsPath() string { return filepath.Join(DataDir(), "settings.json") }

// EffectiveLogKeepDays, ayarlanmamışsa varsayılanı döner.
func (s Settings) EffectiveLogKeepDays() int {
	if s.LogKeepDays > 0 {
		return s.LogKeepDays
	}
	return DefaultLogKeepDays
}

// EffectiveTheme, ayarlanmamış veya bilinmeyen temada koyu temayı döner.
func (s Settings) EffectiveTheme() string {
	if s.Theme == ThemeLight {
		return ThemeLight
	}
	return ThemeDark
}

// LoadSettings, ayarları okur. Dosya yoksa varsayılan ayarlar döner (hata
// değildir); bozuksa varsayılan ayarlar ve hata döner.
func LoadSettings() (Settings, error) {
	var s Settings
	data, err := os.ReadFile(SettingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, i18n.Errorf("err.config.settingsRead", "err", err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, i18n.Errorf("err.config.settingsParse", "path", SettingsPath(), "err", err)
	}
	return s, nil
}

// SaveSettings, ayarları atomik olarak yazar.
func SaveSettings(s Settings) error {
	return writeJSONAtomic(SettingsPath(), s)
}
