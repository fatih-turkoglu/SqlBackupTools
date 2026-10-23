package scheduler

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sqlbackup/internal/config"
)

func TestQuoteArg(t *testing.T) {
	tests := map[string]string{
		`C:\Program Files\SqlBackup.exe`:  `"C:\Program Files\SqlBackup.exe"`,
		`D:\`:                             `"D:\\"`,
		`\\nas\paylaşım\`:                 `"\\nas\paylaşım\\"`,
		`C:\Users\Fatih TÜRKOĞLU\iş.json`: `"C:\Users\Fatih TÜRKOĞLU\iş.json"`,
	}
	for in, want := range tests {
		got, err := quoteArg(in)
		if err != nil || got != want {
			t.Errorf("%q: %q %v, beklenen %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", `C:\a"b`, "C:\\a\nb"} {
		if _, err := quoteArg(bad); err == nil {
			t.Errorf("%q kabul edilmemeli", bad)
		}
	}
}

func TestBatchContent(t *testing.T) {
	c := Command{
		Exe:     `C:\Program Files\SqlBackup\SqlBackup.exe`,
		JobFile: `C:\Users\Fatih TÜRKOĞLU\%100\gece.json`,
		Home:    `D:\`,
	}
	got, err := Batch(c, "gece")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"@echo off",
		"setlocal",
		`for /f "tokens=2 delims=:." %%c in ('chcp') do set "SQLBACKUP_CP=%%c"`,
		"chcp 65001 >nul",
		"rem SqlBackup: gece",
		`"C:\Program Files\SqlBackup\SqlBackup.exe" run --job "C:\Users\Fatih TÜRKOĞLU\%%100\gece.json" --home "D:\\"`,
		`set "SQLBACKUP_RC=%ERRORLEVEL%"`,
		"if defined SQLBACKUP_CP chcp %SQLBACKUP_CP% >nul",
		"exit /b %SQLBACKUP_RC%",
		"",
	}, "\r\n")
	if got != want {
		t.Errorf("içerik:\n%s\nbeklenen:\n%s", got, want)
	}
	// chcp'den önceki satırlar ASCII olmalı (o anki kod sayfasıyla okunurlar).
	head := got[:strings.Index(got, "chcp 65001")]
	for _, r := range head {
		if r > 127 {
			t.Fatalf("chcp öncesinde ASCII olmayan karakter: %q", r)
		}
	}
}

func TestDefaultBatchPath(t *testing.T) {
	t.Setenv(config.HomeEnv, `C:\veri`)
	if got := DefaultBatchPath("gece"); got != `C:\veri\jobs\gece.bat` {
		t.Errorf("yol: %s", got)
	}
}

func TestNewCommandHome(t *testing.T) {
	t.Setenv(config.HomeEnv, "")
	c, err := NewCommand("gece.json")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(c.Exe) || !filepath.IsAbs(c.JobFile) || c.Home != "" {
		t.Errorf("komut: %+v", c)
	}
	home := t.TempDir()
	t.Setenv(config.HomeEnv, home)
	if c, _ = NewCommand("gece.json"); c.Home != home {
		t.Errorf("home: %+v", c)
	}
	args, _ := c.Args()
	if !strings.HasPrefix(args, `run --job "`) || !strings.HasSuffix(args, ` --home "`+home+`"`) {
		t.Errorf("argümanlar: %s", args)
	}
}

// helperExe, test binary'sini dir adlı (Türkçe karakter, boşluk, % içeren) bir
// klasöre SqlBackup.exe olarak kopyalar (bkz. TestMain) ve iş dosyası için
// (yardımcı .args dosyasını onun yanına yazar) var olan bir klasör döner.
func helperExe(t *testing.T, root, dir string) (exe, jobFile string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exeDir := filepath.Join(root, dir)
	jobDir := filepath.Join(root, "İşler")
	for _, d := range []string{exeDir, jobDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe = filepath.Join(exeDir, "SqlBackup.exe")
	copyFile(t, self, exe)
	return exe, filepath.Join(jobDir, "gece yedeği.json")
}

// TestBatchRuns, üretilen .bat'ı gerçekten cmd.exe ile çalıştırır: Türkçe
// karakter, boşluk ve % içeren yollar exe'ye bozulmadan ulaşmalı, çıkış kodu
// iletilmeli ve konsolun kod sayfası geri yüklenmeli.
func TestBatchRuns(t *testing.T) {
	root := t.TempDir()
	exe, jobFile := helperExe(t, root, "Program Ğüşİöç %PATH% 100%")
	c := Command{Exe: exe, JobFile: jobFile, Home: filepath.Join(root, "Veri Ş")}
	content, err := Batch(c, "gece")
	if err != nil {
		t.Fatal(err)
	}
	// .bat'ın kendi yolu sarmalayıcıda (437 ile okunur) geçer; ASCII olmalı.
	bat := filepath.Join(root, "toplu is", "gece.bat")
	if strings.ContainsFunc(bat, func(r rune) bool { return r > 127 }) {
		t.Skipf("geçici klasör ASCII değil: %s", bat)
	}
	if err := WriteBatch(bat, content); err != nil {
		t.Fatal(err)
	}

	// Sarmalayıcı: kod sayfasını 437 yapar, .bat'ı çağırır, sonra kod sayfasını yazar.
	wrapper := filepath.Join(root, "wrapper.bat")
	w := "@chcp 437 >nul\r\n@call \"" + bat + "\"\r\n@echo RC=%ERRORLEVEL%\r\n@chcp\r\n"
	if err := os.WriteFile(wrapper, []byte(w), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("cmd.exe", "/c", wrapper).CombinedOutput()
	if err != nil {
		t.Fatalf("cmd: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), fmt.Sprintf("RC=%d", helperExitCode)) {
		t.Errorf("çıkış kodu iletilmedi: %s", out)
	}
	if !strings.Contains(string(out), "437") {
		t.Errorf("kod sayfası geri yüklenmedi: %s", out)
	}
	got, err := os.ReadFile(jobFile + ".args")
	if err != nil {
		t.Fatalf("yardımcı exe çalışmadı: %v\n%s", err, out)
	}
	want := strings.Join([]string{"run", "--job", c.JobFile, "--home", c.Home}, "\n")
	if string(got) != want {
		t.Errorf("argümanlar:\n%s\nbeklenen:\n%s", got, want)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
