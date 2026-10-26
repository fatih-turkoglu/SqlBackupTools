package app

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"sqlbackup/internal/config"
)

// Pencere arka planı; frontend/src/styles/theme.css içindeki --bg ile aynı
// olmalı (sayfa yüklenene kadar görünen renk).
var (
	darkBackground  = options.RGBA{R: 0x0f, G: 0x11, B: 0x15, A: 255}
	lightBackground = options.RGBA{R: 0xf4, G: 0xf5, B: 0xf8, A: 255}
)

// WindowTheme, kayıtlı temaya göre pencerenin açılış arka planını ve başlık
// çubuğu temasını döner; böylece açılışta renk sıçraması olmaz. Tema
// arayüzde değiştirilince frontend başlık çubuğunu runtime ile günceller.
func WindowTheme() (*options.RGBA, *windows.Options) {
	s, _ := config.LoadSettings()
	if s.EffectiveTheme() == config.ThemeLight {
		bg := lightBackground
		return &bg, &windows.Options{Theme: windows.Light}
	}
	bg := darkBackground
	return &bg, &windows.Options{Theme: windows.Dark}
}
