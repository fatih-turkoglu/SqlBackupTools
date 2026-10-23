package scheduler

import (
	"os"
	"strings"
	"testing"

	"sqlbackup/internal/i18n/i18ntest"
)

// helperExitCode, sahte SqlBackup.exe'nin çıkış kodudur (3 olmaz: cmd.exe
// "yol bulunamadı" için 3 döner, testler ikisini ayırt edemezdi).
const helperExitCode = 1

// TestMain: test binary'si "run --job <dosya> ..." ile başlatılırsa .bat ve
// zamanlanmış görev testlerinde çağrılan sahte SqlBackup.exe gibi davranır:
// argümanlarını <dosya>.args'a yazar ve helperExitCode ile çıkar. Ortam
// değişkeni kullanılmaz; zamanlanmış görev onu devralmaz.
func TestMain(m *testing.M) {
	if len(os.Args) > 3 && os.Args[1] == "run" && os.Args[2] == "--job" {
		os.WriteFile(os.Args[3]+".args", []byte(strings.Join(os.Args[1:], "\n")), 0o644)
		os.Exit(helperExitCode)
	}
	i18ntest.Use("tr")
	os.Exit(m.Run())
}
