package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/logging"
	"sqlbackup/internal/mssql"
)

type fakeServer struct {
	backups []string
}

func (f *fakeServer) Info() mssql.ServerInfo {
	return mssql.ServerInfo{ServerName: `PC01\SQLEXPRESS`, Edition: "Express Edition (64-bit)", ProductVersion: "16.0.1000.6",
		ProductLevel: "RTM", Protocol: "lpc", LoginName: `PC01\Fatih`, EngineEdition: 4}
}
func (f *fakeServer) ListDatabases(context.Context) ([]mssql.Database, error) {
	last := time.Date(2026, 10, 22, 2, 0, 0, 0, time.Local)
	return []mssql.Database{
		{Name: "master", State: "ONLINE", RecoveryModel: "SIMPLE", SizeMB: 6, IsSystem: true, CanBackup: true},
		{Name: "Satis", State: "ONLINE", RecoveryModel: "FULL", SizeMB: 120.5, CanBackup: true, LastFullBackup: &last},
		{Name: "Arsiv", State: "OFFLINE", RecoveryModel: "FULL", SizeMB: 50},
	}, nil
}
func (f *fakeServer) BackupDatabase(_ context.Context, db, file string, _ mssql.BackupOptions, p mssql.ProgressFunc) error {
	f.backups = append(f.backups, db)
	if p != nil {
		p(50)
		p(100)
	}
	return nil
}
func (f *fakeServer) VerifyBackup(context.Context, string, bool, mssql.ProgressFunc) error {
	return nil
}
func (f *fakeServer) LastBackupSet(context.Context, string, string) (*mssql.BackupSetInfo, error) {
	return &mssql.BackupSetInfo{SizeBytes: 2 << 20, CompressedBytes: 2 << 20}, nil
}
func (f *fakeServer) Close() error { return nil }

var fixed = time.Date(2026, 10, 23, 2, 0, 0, 0, time.Local)

// testEnv, veri klasörünü geçici klasöre yönlendirir ve sahte sunucuyla bir ortam döner.
func testEnv(t *testing.T) (*env, *bytes.Buffer, *bytes.Buffer, *fakeServer) {
	t.Helper()
	t.Setenv(config.HomeEnv, t.TempDir())
	var out, errb bytes.Buffer
	f := &fakeServer{}
	e := &env{
		ctx: context.Background(), stdout: &out, stderr: &errb, version: "1.0.0-test",
		connect: func(context.Context, config.Connection, string) (backup.Server, error) { return f, nil },
		now:     func() time.Time { return fixed },
	}
	return e, &out, &errb, f
}

// saveJob, jobs klasörüne bir iş kaydeder.
func saveJob(t *testing.T, name string, edit func(*config.Job)) *config.Job {
	t.Helper()
	j := config.NewJob(name)
	j.Connection.Server = `.\SQLEXPRESS`
	j.Connection.Auth = config.AuthWindows
	j.Backup.ServerPath = `D:\SqlBackups`
	j.Databases = []string{"Satis"}
	j.ExcludeSystemDatabases = true
	if edit != nil {
		edit(j)
	}
	if err := config.SaveJob(config.JobPath(name), j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestUsageAndVersion(t *testing.T) {
	tests := []struct {
		args []string
		code int
		out  string // stdout veya stderr'de aranır
	}{
		{nil, 2, "Kullanım:"},
		{[]string{"help"}, 0, "Kullanım:"},
		{[]string{"/?"}, 0, "Kullanım:"},
		{[]string{"version"}, 0, "SqlBackup 1.0.0-test"},
		{[]string{"yedekle"}, 2, "bilinmeyen komut: yedekle"},
		{[]string{"run"}, 2, "--job belirtilmedi"},
		{[]string{"run", "--job", "gece", "fazla"}, 2, "beklenmeyen argüman: fazla"},
		{[]string{"test", "--bilinmeyen"}, 2, "bilinmeyen"},
		{[]string{"RUN", "--job", "yok"}, 2, "iş dosyası bulunamadı"},
	}
	for _, tt := range tests {
		e, out, errb, _ := testEnv(t)
		code := e.main(tt.args)
		all := out.String() + errb.String()
		if code != tt.code || !strings.Contains(all, tt.out) {
			t.Errorf("%v: kod=%d çıktı=%q", tt.args, code, all)
		}
	}
}

func TestJobPath(t *testing.T) {
	t.Setenv(config.HomeEnv, `C:\veri`)
	tests := map[string]string{
		"gece":                            `C:\veri\jobs\gece.json`,
		" gece.v2 ":                       `C:\veri\jobs\gece.v2.json`,
		"gece.json":                       "gece.json",
		`D:\isler\gece.json`:              `D:\isler\gece.json`,
		`.\gece`:                          `.\gece`,
		`C:\Users\Fatih TÜRKOĞLU\iş.json`: `C:\Users\Fatih TÜRKOĞLU\iş.json`,
	}
	for in, want := range tests {
		if got := jobPath(in); got != want {
			t.Errorf("%q: %q, beklenen %q", in, got, want)
		}
	}
}

func TestRun(t *testing.T) {
	e, out, errb, f := testEnv(t)
	saveJob(t, "gece", nil)
	code := e.main([]string{"run", "--job", "gece"})
	t.Log("\n" + out.String() + errb.String())
	if code != backup.ExitOK || len(f.backups) != 1 || f.backups[0] != "Satis" {
		t.Fatalf("kod=%d yedekler=%v", code, f.backups)
	}
	// Konsol değil: aşama sonunda tek satır, ardından özet.
	if !strings.Contains(out.String(), "[1/1] Satis  yedek  %100\n") || !strings.Contains(out.String(), "[OK]   Satis") ||
		strings.Contains(out.String(), "\r") {
		t.Errorf("çıktı: %q", out.String())
	}

	logs := config.LogsDir()
	data, err := os.ReadFile(filepath.Join(logs, "gece_20261023.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"komut satırından çalıştırıldı", "version=1.0.0-test", "yedek tamamlandı", "çalışma kaydı yazıldı"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("logda %q yok:\n%s", want, data)
		}
	}
	runs, err := logging.ListRuns(logs, "gece")
	if err != nil || len(runs) != 1 {
		t.Fatalf("çalışma kayıtları: %v %v", runs, err)
	}
	var r backup.Result
	if err := logging.ReadRun(runs[0].Path, &r); err != nil || r.ExitCode != 0 || len(r.Databases) != 1 {
		t.Errorf("kayıt: %+v %v", r, err)
	}
}

func TestRunVerbose(t *testing.T) {
	e, out, _, _ := testEnv(t)
	e.console = true // -v ile ilerleme satırı yazılmaz, loglar konsola gider
	saveJob(t, "gece", nil)
	if code := e.main([]string{"run", "-v", "--job", "gece"}); code != backup.ExitOK {
		t.Fatalf("kod=%d", code)
	}
	if !strings.Contains(out.String(), `msg="yedek tamamlandı"`) || strings.Contains(out.String(), "\r") {
		t.Errorf("çıktı: %q", out.String())
	}
}

func TestRunConfigErrorIsRecorded(t *testing.T) {
	e, out, _, f := testEnv(t)
	saveJob(t, "bozuk", func(j *config.Job) { j.Backup.ServerPath = "" })
	if code := e.main([]string{"run", "--job", "bozuk"}); code != backup.ExitFailed || len(f.backups) != 0 {
		t.Fatalf("kod=%d", code)
	}
	if !strings.Contains(out.String(), "HATA: iş ayarları geçersiz") {
		t.Errorf("çıktı: %s", out.String())
	}
	if runs, _ := logging.ListRuns(config.LogsDir(), "bozuk"); len(runs) != 1 {
		t.Errorf("hatalı çalışma da kaydedilmeli: %v", runs)
	}
}

func TestRunInvalidName(t *testing.T) {
	e, _, errb, _ := testEnv(t)
	p := filepath.Join(t.TempDir(), "x.json")
	j := config.NewJob(`..\kötü`)
	if err := config.SaveJob(p, j); err != nil {
		t.Fatal(err)
	}
	if code := e.main([]string{"run", "--job", p}); code != backup.ExitFailed || errb.Len() == 0 {
		t.Fatalf("kod=%d stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(config.LogsDir()); !errors.Is(err, os.ErrNotExist) {
		t.Error("geçersiz adla log dosyası oluşturulmamalı")
	}
}

func TestTestCommand(t *testing.T) {
	e, out, _, _ := testEnv(t)
	saveJob(t, "gece", nil)
	if code := e.main([]string{"test", "--job", "gece"}); code != backup.ExitOK {
		t.Fatalf("kod=%d", code)
	}
	for _, want := range []string{`PC01\SQLEXPRESS`, "16.0.1000.6 RTM", "Shared Memory", "desteklenmiyor", "Bağlantı başarılı."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("çıktıda %q yok:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "erişimi") {
		t.Error("zip/saklama kapalıyken erişim denenmemeli")
	}

	// Zip açık ve klasör yoksa erişim hatası → 1.
	e, out, errb, _ := testEnv(t)
	saveJob(t, "zipli", func(j *config.Job) {
		j.Archive.ZipEnabled = true
		j.Backup.ServerPath = filepath.Join(t.TempDir(), "yok")
	})
	if code := e.main([]string{"test", "--job", "zipli"}); code != backup.ExitPartial ||
		!strings.Contains(errb.String(), "erişilemiyor") || !strings.Contains(out.String(), "HATA") {
		t.Errorf("kod=%d\n%s\n%s", code, out.String(), errb.String())
	}
}

func TestTestCommandConnectError(t *testing.T) {
	e, _, errb, _ := testEnv(t)
	e.connect = func(context.Context, config.Connection, string) (backup.Server, error) {
		return nil, fmt.Errorf("sunucu bulunamadı")
	}
	saveJob(t, "gece", nil)
	if code := e.main([]string{"test", "--job", "gece"}); code != backup.ExitFailed ||
		!strings.Contains(errb.String(), "bağlantı kurulamadı: sunucu bulunamadı") {
		t.Errorf("kod=%d stderr=%s", code, errb.String())
	}
}

func TestListCommand(t *testing.T) {
	e, out, _, _ := testEnv(t)
	saveJob(t, "gece", func(j *config.Job) { j.Databases = []string{"Satis", "Arsiv", "Silinmis"} })
	if code := e.main([]string{"list", "--job", "gece"}); code != backup.ExitOK {
		t.Fatalf("kod=%d", code)
	}
	s := out.String()
	t.Log("\n" + s)
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) != 6 || !strings.HasPrefix(lines[0], "VERİTABANI") ||
		!strings.Contains(lines[1], "master") || !strings.HasSuffix(lines[1], "hayır") ||
		!strings.Contains(lines[2], "120.5") || !strings.Contains(lines[2], "2026-10-22 02:00") || !strings.HasSuffix(lines[2], "evet") ||
		!strings.Contains(lines[3], "HATA:") || !strings.HasPrefix(lines[4], "Silinmis") ||
		lines[5] != "4 veritabanı, 1 tanesi yedeklenecek" {
		t.Errorf("liste beklenen biçimde değil")
	}

	e, out, _, _ = testEnv(t)
	saveJob(t, "gece", nil)
	if code := e.main([]string{"list", "--json", "--job", "gece"}); code != backup.ExitOK {
		t.Fatalf("kod=%d", code)
	}
	var rows []listRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil || len(rows) != 3 || !rows[1].Backup || rows[1].Name != "Satis" {
		t.Errorf("json: %v %+v", err, rows)
	}
}

func TestProgressPrinterConsole(t *testing.T) {
	var b bytes.Buffer
	p := &progressPrinter{w: &b, console: true}
	p.update(backup.Progress{Database: "Satis", Index: 1, Total: 2, Phase: backup.PhaseBackup, Percent: 50})
	p.update(backup.Progress{Database: "Satis", Index: 1, Total: 2, Phase: backup.PhaseBackup, Percent: 100})
	p.update(backup.Progress{Database: "Satis", Index: 1, Total: 2, Phase: backup.PhaseVerify, Percent: 100})
	p.finish()
	got := strings.ReplaceAll(b.String(), " ", "")
	want := "\r[1/2]Satisyedek%50\r[1/2]Satisyedek%100\n\r[1/2]Satisdoğrulama%100\n"
	if got != want {
		t.Errorf("çıktı: %q", got)
	}
}

func TestExtractFlag(t *testing.T) {
	tests := []struct {
		in   []string
		code string
		rest string
	}{
		{[]string{"run", "--job", "gece"}, "", "run --job gece"},
		{[]string{"--lang", "tr", "run", "--job", "gece"}, "tr", "run --job gece"},
		{[]string{"run", "--job", "gece", "-lang=en"}, "en", "run --job gece"},
		{[]string{"run", "--LANG=tr", "-v"}, "tr", "run -v"},
		{[]string{"run", "--job", "lang"}, "", "run --job lang"},
	}
	for _, tt := range tests {
		code, rest := extractFlag(tt.in, "lang")
		if code != tt.code || strings.Join(rest, " ") != tt.rest {
			t.Errorf("%v: %q %v", tt.in, code, rest)
		}
	}
}

// langEnv, dil seçimini sınar; test sonunda Türkçe'ye döner.
func langEnv(t *testing.T) (*env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	e, out, errb, _ := testEnv(t)
	t.Cleanup(func() { i18n.SetLanguage("tr") })
	return e, out, errb
}

func TestLanguageSelection(t *testing.T) {
	// Ayar yok, --lang yok → İngilizce.
	e, out, _ := langEnv(t)
	e.main(e.setup([]string{"help"}))
	if !strings.Contains(out.String(), "Usage:") || e.keepDays != config.DefaultLogKeepDays {
		t.Errorf("varsayılan İngilizce olmalı: %s", out.String())
	}

	// Ayarlardaki dil ve log saklama süresi kullanılır.
	e, out, _ = langEnv(t)
	if err := config.SaveSettings(config.Settings{Language: "tr", LogKeepDays: 7}); err != nil {
		t.Fatal(err)
	}
	e.main(e.setup([]string{"help"}))
	if !strings.Contains(out.String(), "Kullanım:") || e.keepDays != 7 {
		t.Errorf("ayarlardaki dil: %s (keepDays=%d)", out.String(), e.keepDays)
	}

	// --lang ayarı geçersiz kılar.
	out.Reset()
	e.main(e.setup([]string{"--lang", "en", "help"}))
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("--lang en: %s", out.String())
	}

	// Bilinmeyen dil → İngilizce + uyarı.
	e, out, errb := langEnv(t)
	e.main(e.setup([]string{"help", "--lang=zz"}))
	if !strings.Contains(out.String(), "Usage:") || !strings.Contains(errb.String(), "WARNING: could not load language (zz)") {
		t.Errorf("bilinmeyen dil:\n%s\n%s", out.String(), errb.String())
	}
}

func TestRunEnglish(t *testing.T) {
	e, out, _ := langEnv(t)
	saveJob(t, "gece", nil)
	if code := e.main(e.setup([]string{"run", "--job", "gece", "--lang", "en"})); code != backup.ExitOK {
		t.Fatalf("kod=%d", code)
	}
	for _, want := range []string{"[1/1] Satis  backup  %100", "Job: gece", "[OK]   Satis", "Result: 1 succeeded, 0 failed, 0 skipped (exit code 0)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("çıktıda %q yok:\n%s", want, out.String())
		}
	}
	data, _ := os.ReadFile(filepath.Join(config.LogsDir(), "gece_20261023.log"))
	if !strings.Contains(string(data), `msg="backup finished"`) || !strings.Contains(string(data), "lang=en") {
		t.Errorf("log İngilizce olmalı:\n%s", data)
	}
}
