package backup

import (
	"os"
	"testing"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n/i18ntest"
)

// Testler mesaj içeriklerini Türkçe olarak kontrol eder; aynı zamanda
// lang/tr.lng dosyasını da sınamış olur. Veri klasörü geçici bir klasöre
// yönlendirilir; böylece iş kilitleri gerçek işlerle ve paralel çalışan
// diğer test paketleriyle çakışmaz.
func TestMain(m *testing.M) {
	i18ntest.Use("tr")
	// Kilit testinin alt süreci ebeveynin veri klasörünü (ortamdan) kullanmalı.
	if os.Getenv("SQLBACKUP_LOCK_HELPER") != "" {
		os.Exit(m.Run())
	}
	home, err := os.MkdirTemp("", "sqlbackup-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv(config.HomeEnv, home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
