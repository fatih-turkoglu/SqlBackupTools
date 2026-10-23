// Package cli, komut satırı (penceresiz) çalışma modunu sağlar.
//
//	SqlBackup.exe run  --job <iş adı | dosya yolu> [-v]
//	SqlBackup.exe test --job <iş adı | dosya yolu>
//	SqlBackup.exe list --job <iş adı | dosya yolu> [--json]
//	SqlBackup.exe version | help
//
// Çıkış kodları backup paketindeki sözleşmeye uyar (0, 1, 2, 3); kullanım ve
// iş dosyası hataları 2 döner.
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
	"sqlbackup/internal/mssql"
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
	return e.main(args)
}

func (e *env) main(args []string) int {
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
	case "version", "--version", "-version":
		fmt.Fprintf(e.stdout, "SqlBackup %s\n", e.version)
		return backup.ExitOK
	case "help", "--help", "-help", "-h", "/?":
		e.usage(e.stdout)
		return backup.ExitOK
	}
	fmt.Fprintf(e.stderr, "HATA: bilinmeyen komut: %s\n\n", args[0])
	e.usage(e.stderr)
	return backup.ExitFailed
}

func (e *env) usage(w io.Writer) {
	fmt.Fprintf(w, `SqlBackup %s – MSSQL otomatik yedekleme

Kullanım:
  SqlBackup.exe                         arayüzü açar
  SqlBackup.exe run  --job <iş> [-v]    işi çalıştırır (yedek, doğrulama, zip, saklama)
  SqlBackup.exe test --job <iş>         bağlantıyı ve yedek klasörü erişimini sınar
  SqlBackup.exe list --job <iş> [--json] sunucudaki veritabanlarını listeler
  SqlBackup.exe version                 sürümü yazar

<iş>: iş adı (%s içinde aranır) veya .json dosya yolu
  -v       log kayıtlarını konsola da yazar
  --json   listeyi JSON olarak yazar

Çıkış kodları: 0 başarılı, 1 kısmi hata, 2 başarısız/ayar hatası, 3 iş zaten çalışıyor
`, e.version, config.JobsDir())
}

// newFlags, komut için ortak --job bayrağıyla bir bayrak kümesi oluşturur.
func (e *env) newFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.Usage = func() { e.usage(e.stderr) }
	job := fs.String("job", "", "iş adı veya .json dosya yolu")
	return fs, job
}

// parse, bayrakları ayrıştırır; hata veya eksik --job durumunda false döner.
func (e *env) parse(fs *flag.FlagSet, job *string, args []string) bool {
	if err := fs.Parse(args); err != nil {
		return false
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(e.stderr, "HATA: beklenmeyen argüman: %s\n", fs.Arg(0))
		return false
	}
	if strings.TrimSpace(*job) == "" {
		fmt.Fprintln(e.stderr, "HATA: --job belirtilmedi")
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
		fmt.Fprintf(e.stderr, "HATA: iş dosyası bulunamadı: %s\n", p)
		return nil, p
	}
	if err != nil {
		fmt.Fprintf(e.stderr, "HATA: %v\n", err)
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
			return nil, fmt.Errorf("kayıtlı şifre çözülemedi; iş dosyası başka bir makineden kopyalandıysa şifreyi yeniden girin: %w", err)
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
