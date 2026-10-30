// Package i18ntest, testlerde repo içindeki lang klasöründen dil yükler.
//
//	func TestMain(m *testing.M) {
//		i18ntest.Use("tr")
//		os.Exit(m.Run())
//	}
package i18ntest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"sqlbackup/internal/i18n"
)

// LangDir, repo kökündeki lang klasörünün yoludur.
func LangDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "lang")
}

// Use, dil klasörünü repo içindekine ayarlar ve dili seçer; dil dosyası
// yüklenemez veya bozuksa testleri durdurur. Sonuç makinenin Windows diline
// bağlı olmasın diye sistem dili yok sayılır (Detect → İngilizce).
func Use(code string) {
	i18n.SetDir(LangDir())
	i18n.SetSystemLanguages(func() []string { return nil })
	if err := i18n.SetLanguage(code); err != nil {
		fmt.Fprintln(os.Stderr, "i18ntest:", err)
		os.Exit(2)
	}
}
