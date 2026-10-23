// Package cli, komut satırı (penceresiz) çalışma modunu sağlar.
//
//	SqlBackup.exe run  --job <iş adı | dosya yolu> [-v]
//	SqlBackup.exe test --job <iş adı | dosya yolu>
//	SqlBackup.exe list --job <iş adı | dosya yolu> [--json]
//	SqlBackup.exe batch --job <iş adı | dosya yolu> [--path <dosya.bat>]
//	SqlBackup.exe task create|delete|run|status --job <iş> [--elevate] [--json]
//	SqlBackup.exe version | help
//
// Tüm komutlar --lang <kod>, --home <veri klasörü> ve --output <dosya>
// alabilir. Dil verilmezse ayarlar dosyasından, orada da yoksa İngilizce seçilir. Çıkış kodları backup paketindeki
// sözleşmeye uyar (0, 1, 2, 3); kullanım ve iş dosyası hataları 2 döner.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/mssql"
	"sqlbackup/internal/scheduler"
)

// env, komutların dış dünyayla etkileşimidir; testlerde değiştirilir.
type env struct {
	ctx     context.Context
	stdout  io.Writer
	stderr  io.Writer
	version string
	// console, stdout bir konsolsa true'dur (ilerleme satırı \r ile güncellenir).
	console bool
	connect backup.ConnectFunc
	now     func() time.Time
	// keepDays, log ve çalışma kayıtlarının saklama süresidir (ayarlardan).
	keepDays int

	// Zamanlanmış görev komutları için; nil ise gerçek sistem kullanılır.
	tasks      scheduler.Service
	user       string // "current" görev hesabı (boşsa oturumdaki kullanıcı)
	isElevated func() bool
	runAsAdmin func(exe string, args []string) (int, error)
}

// Main, komut satırı argümanlarını (program adı hariç) işler ve çıkış
// kodunu döner. Ctrl+C çalışan işlemi iptal eder.
func Main(args []string, version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	e := &env{
		ctx:     ctx,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		version: version,
		console: isConsole(os.Stdout),
		now:     time.Now,
	}
	// --output: tüm çıktı dosyaya yazılır (yükseltilmiş süreç çıktısını
	// çağırana böyle iletir; konsolu yoktur).
	output, args := extractFlag(args, "output")
	if output != "" {
		if f, err := os.Create(output); err == nil {
			defer f.Close()
			e.stdout, e.stderr, e.console = f, f, false
		}
	}
	args = e.setup(args)
	return e.main(args)
}

// setup, veri klasörünü (--home), dili (--lang → ayarlar → İngilizce) ve
// ayarları yükler; bu argümanları listeden çıkarır. Sorunlar uyarı olarak
// yazılır, çalışmayı durdurmaz.
func (e *env) setup(args []string) []string {
	// --home, SQLBACKUP_HOME'un karşılığıdır: zamanlanmış görev ve
	// yükseltilmiş süreç ortam değişkenini devralmaz.
	home, args := extractFlag(args, "home")
	if home != "" {
		os.Setenv(config.HomeEnv, home)
	}
	langArg, args := extractFlag(args, "lang")
	settings, serr := config.LoadSettings()
	e.keepDays = settings.EffectiveLogKeepDays()
	code := langArg
	if code == "" {
		code = settings.Language
	}
	// Dil, uyarı metinlerinden önce seçilmeli.
	if err := i18n.SetLanguage(code); err != nil {
		fmt.Fprintln(e.stderr, i18n.T("cli.warning", "msg", i18n.T("cli.warn.language", "code", code, "err", err)))
	}
	if serr != nil {
		fmt.Fprintln(e.stderr, i18n.T("cli.warning", "msg", serr))
	}
	return args
}

// extractFlag, argümanlardan her komutta geçerli "--ad değer" / "--ad=değer"
// (tek tire de olur) bayrağını çıkarır ve kalan argümanları döner.
func extractFlag(args []string, flagName string) (string, []string) {
	var value string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || !strings.EqualFold(name, flagName) {
			rest = append(rest, a)
			continue
		}
		if !hasVal && i+1 < len(args) {
			i++
			val = args[i]
		}
		value = val
	}
	return value, rest
}

func (e *env) main(args []string) int {
	if e.keepDays <= 0 {
		e.keepDays = config.DefaultLogKeepDays
	}
	if len(args) == 0 {
		e.usage(e.stderr)
		return backup.ExitFailed
	}
	cmd, rest := strings.ToLower(args[0]), args[1:]
	switch cmd {
	case "run":
		return e.cmdRun(rest)
	case "test":
		return e.cmdTest(rest)
	case "list":
		return e.cmdList(rest)
	case "batch":
		return e.cmdBatch(rest)
	case "task":
		return e.cmdTask(rest)
	case "version", "--version", "-version":
		fmt.Fprintf(e.stdout, "SqlBackup %s\n", e.version)
		return backup.ExitOK
	case "help", "--help", "-help", "-h", "/?":
		e.usage(e.stdout)
		return backup.ExitOK
	}
	e.errorf(i18n.T("cli.err.unknownCommand", "cmd", args[0]))
	fmt.Fprintln(e.stderr)
	e.usage(e.stderr)
	return backup.ExitFailed
}

// errorf ve warnf, stderr'e seçili dilde "HATA:"/"UYARI:" önekiyle yazar.
func (e *env) errorf(msg any) { fmt.Fprintln(e.stderr, i18n.T("cli.error", "msg", msg)) }
func (e *env) warnf(msg any)  { fmt.Fprintln(e.stderr, i18n.T("cli.warning", "msg", msg)) }

func (e *env) usage(w io.Writer) {
	fmt.Fprintln(w, i18n.T("cli.usage", "version", e.version, "jobsDir", config.JobsDir()))
}

// newFlags, komut için ortak --job bayrağıyla bir bayrak kümesi oluşturur.
// Bayrak hataları flag paketi yerine burada (seçili dilde önekle) yazılır.
func (e *env) newFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	job := fs.String("job", "", "")
	return fs, job
}

// parse, bayrakları ayrıştırır; hata veya eksik --job durumunda false döner.
func (e *env) parse(fs *flag.FlagSet, job *string, args []string) bool {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			e.usage(e.stderr)
		} else {
			e.errorf(err)
		}
		return false
	}
	if fs.NArg() > 0 {
		e.errorf(i18n.T("cli.err.unexpectedArg", "arg", fs.Arg(0)))
		return false
	}
	if strings.TrimSpace(*job) == "" {
		e.errorf(i18n.T("cli.err.noJob"))
		return false
	}
	return true
}

// jobPath, --job değerini dosya yoluna çevirir: yol ayırıcı veya .json
// uzantısı içeriyorsa dosya yolu, değilse jobs klasöründeki iş adı sayılır.
func jobPath(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.ContainsAny(arg, `\/:`) || strings.EqualFold(filepath.Ext(arg), ".json") {
		return arg
	}
	return config.JobPath(arg)
}

// loadJob, iş dosyasını okur; hata varsa stderr'e yazar ve nil döner.
func (e *env) loadJob(arg string) (*config.Job, string) {
	p := jobPath(arg)
	j, err := config.LoadJob(p)
	if errors.Is(err, os.ErrNotExist) {
		e.errorf(i18n.T("cli.err.jobNotFound", "path", p))
		return nil, p
	}
	if err != nil {
		e.errorf(err)
		return nil, p
	}
	return j, p
}

// openServer, işin bağlantı ayarlarıyla sunucuya bağlanır.
func (e *env) openServer(j *config.Job) (backup.Server, error) {
	var password string
	if j.Connection.Auth == config.AuthSQL {
		var err error
		if password, err = config.Unprotect(j.Connection.PasswordEnc); err != nil {
			return nil, i18n.Errorf("err.backup.password", "err", err)
		}
	}
	if e.connect != nil {
		return e.connect(e.ctx, j.Connection, password)
	}
	cl, err := mssql.Open(e.ctx, j.Connection, password)
	if err != nil {
		return nil, err
	}
	return cl, nil
}
