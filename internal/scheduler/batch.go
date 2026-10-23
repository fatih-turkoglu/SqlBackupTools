package scheduler

import (
	"os"
	"path/filepath"
	"strings"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
)

// Command, işi çalıştıracak exe ve iş dosyasıdır. Hem .bat hem zamanlanmış
// görev aynı komut satırını kullanır.
type Command struct {
	// Exe, SqlBackup.exe'nin tam yoludur.
	Exe string
	// JobFile, iş JSON dosyasının tam yoludur.
	JobFile string
	// Home, varsayılan dışındaki veri klasörüdür (SQLBACKUP_HOME). Boşsa
	// verilmez. Görev ve yükseltilmiş süreç ortam değişkenini miras almadığı
	// için --home bayrağıyla taşınır.
	Home string
}

// NewCommand, çalışan exe ve iş dosyasından komutu oluşturur. Yollar mutlak
// yola çevrilir; SQLBACKUP_HOME tanımlıysa --home olarak eklenir.
func NewCommand(jobFile string) (Command, error) {
	exe, err := os.Executable()
	if err != nil {
		return Command{}, i18n.Errorf("err.scheduler.exePath", "err", err)
	}
	c := Command{Exe: exe, JobFile: jobFile, Home: os.Getenv(config.HomeEnv)}
	return c.abs()
}

func (c Command) abs() (Command, error) {
	for _, p := range []*string{&c.Exe, &c.JobFile, &c.Home} {
		if *p == "" {
			continue
		}
		a, err := filepath.Abs(*p)
		if err != nil {
			return c, i18n.Errorf("err.scheduler.path", "path", *p, "err", err)
		}
		*p = a
	}
	return c, nil
}

// Args, exe'ye verilecek argüman satırıdır: run --job "<iş>" [--home "<klasör>"].
func (c Command) Args() (string, error) {
	job, err := quoteArg(c.JobFile)
	if err != nil {
		return "", err
	}
	args := "run --job " + job
	if c.Home != "" {
		home, err := quoteArg(c.Home)
		if err != nil {
			return "", err
		}
		args += " --home " + home
	}
	return args, nil
}

// quoteArg, yolu her zaman çift tırnak içine alır. Sondaki ters bölüler
// ikilenir; aksi halde CommandLineToArgvW `"C:\"` içindeki `\"`'yi kaçış
// sayar. Windows yollarında çift tırnak olamaz, varsa hata döner.
func quoteArg(s string) (string, error) {
	if s == "" || strings.ContainsAny(s, "\"\r\n") {
		return "", i18n.Errorf("err.scheduler.badPath", "path", s)
	}
	trimmed := strings.TrimRight(s, `\`)
	return `"` + trimmed + strings.Repeat(`\\`, len(s)-len(trimmed)) + `"`, nil
}

// DefaultBatchPath, işin varsayılan .bat yoludur: jobs\<ad>.bat.
func DefaultBatchPath(name string) string {
	return filepath.Join(config.JobsDir(), name+".bat")
}

// Batch, işi çalıştıran .bat içeriğini (CRLF satır sonlu, BOM'suz UTF-8) üretir.
//
// cmd, .bat dosyasını satır satır o anki kod sayfasıyla okur; bu yüzden
// Türkçe karakterli yollar "chcp 65001"den sonra gelir ve ondan önceki
// satırlar yalnızca ASCII'dir (BOM da ilk satırı bozacağı için yazılmaz).
// Önceki kod sayfası geri yüklenir, böylece .bat açık bir konsoldan
// çalıştırıldığında konsolun kod sayfası değişmiş kalmaz. Çıkış kodu
// programınkidir (0, 1, 2, 3).
func Batch(c Command, jobName string) (string, error) {
	exe, err := quoteArg(c.Exe)
	if err != nil {
		return "", err
	}
	args, err := c.Args()
	if err != nil {
		return "", err
	}
	// .bat içinde % değişken başlatır; yollardaki % ikilenmeli.
	esc := func(s string) string { return strings.ReplaceAll(s, "%", "%%") }
	lines := []string{
		"@echo off",
		"setlocal",
		`for /f "tokens=2 delims=:." %%c in ('chcp') do set "SQLBACKUP_CP=%%c"`,
		"chcp 65001 >nul",
		"rem SqlBackup: " + esc(jobName),
		esc(exe + " " + args),
		`set "SQLBACKUP_RC=%ERRORLEVEL%"`,
		"if defined SQLBACKUP_CP chcp %SQLBACKUP_CP% >nul",
		"exit /b %SQLBACKUP_RC%",
	}
	return strings.Join(lines, "\r\n") + "\r\n", nil
}

// WriteBatch, .bat içeriğini dosyaya yazar (klasör yoksa oluşturulur).
func WriteBatch(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return i18n.Errorf("err.scheduler.batchWrite", "path", path, "err", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return i18n.Errorf("err.scheduler.batchWrite", "path", path, "err", err)
	}
	return nil
}
