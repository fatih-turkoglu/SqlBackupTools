package archive

import (
	"os"
	"testing"

	"sqlbackup/internal/i18n/i18ntest"
)

// Testler mesaj içeriklerini Türkçe olarak kontrol eder; aynı zamanda
// lang/tr.lng dosyasını da sınamış olur.
func TestMain(m *testing.M) {
	i18ntest.Use("tr")
	os.Exit(m.Run())
}
