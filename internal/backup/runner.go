package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sqlbackup/internal/config"
	"sqlbackup/internal/mssql"
)

// Exit code sözleşmesi (CLI ve zamanlanmış görev için).
const (
	ExitOK      = 0 // tüm veritabanları başarılı
	ExitPartial = 1 // en az biri başarılı, en az biri hatalı
	ExitFailed  = 2 // hiçbiri başarılı değil veya config/bağlantı hatası
)

// Server, çalıştırıcının ihtiyaç duyduğu SQL Server işlemleridir.
// *mssql.Client bu arayüzü sağlar; testlerde sahte sunucu kullanılır.
type Server interface {
	Info() mssql.ServerInfo
	ListDatabases(ctx context.Context) ([]mssql.Database, error)
	BackupDatabase(ctx context.Context, database, file string, opts mssql.BackupOptions, progress mssql.ProgressFunc) error
	VerifyBackup(ctx context.Context, file string, checksum bool, progress mssql.ProgressFunc) error
	LastBackupSet(ctx context.Context, database, file string) (*mssql.BackupSetInfo, error)
	Close() error
}

// ConnectFunc, sunucu bağlantısını açar.
type ConnectFunc func(ctx context.Context, c config.Connection, password string) (Server, error)

func connectMSSQL(ctx context.Context, c config.Connection, password string) (Server, error) {
	cl, err := mssql.Open(ctx, c, password)
	if err != nil {
		return nil, err
	}
	return cl, nil
}

// Phase, ilerleme bildiriminin hangi aşamaya ait olduğunu belirtir.
type Phase string

const (
	PhaseBackup Phase = "backup"
	PhaseVerify Phase = "verify"
)

// Progress, bir veritabanının yedek/doğrulama ilerlemesidir.
type Progress struct {
	Database string `json:"database"`
	Index    int    `json:"index"` // 1'den başlar
	Total    int    `json:"total"`
	Phase    Phase  `json:"phase"`
	Percent  int    `json:"percent"`
}

// Options, Run davranışını özelleştirir. Tüm alanlar opsiyoneldir.
type Options struct {
	Logger   *slog.Logger
	Progress func(Progress)
	// Connect boşsa mssql.Open kullanılır.
	Connect ConnectFunc
	// Now boşsa time.Now kullanılır (testlerde sabit zaman için).
	Now func() time.Time
}

// Status, bir veritabanının çalışma sonucudur.
type Status string

const (
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// DBResult, tek bir veritabanının sonucudur.
type DBResult struct {
	Database string `json:"database"`
	Status   Status `json:"status"`
	// File, yedek dosyasının sunucu tarafındaki yoludur.
	File string `json:"file,omitempty"`
	// SizeBytes, yedeklenen veri boyutu; FileBytes, diskteki (sıkıştırılmış)
	// yaklaşık dosya boyutudur. İkisi de msdb'den okunur.
	SizeBytes  int64     `json:"sizeBytes"`
	FileBytes  int64     `json:"fileBytes"`
	Compressed bool      `json:"compressed"`
	Verified   bool      `json:"verified"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	ElapsedMs  int64     `json:"elapsedMs"`
	Error      string    `json:"error,omitempty"`
}

// Result, bir iş çalışmasının özetidir.
type Result struct {
	Job       string     `json:"job"`
	Server    string     `json:"server"`
	Edition   string     `json:"edition,omitempty"`
	Protocol  string     `json:"protocol,omitempty"`
	Start     time.Time  `json:"start"`
	End       time.Time  `json:"end"`
	ElapsedMs int64      `json:"elapsedMs"`
	Databases []DBResult `json:"databases"`
	Warnings  []string   `json:"warnings"`
	// Error, işin hiç başlayamamasına yol açan hatadır (config, şifre,
	// bağlantı, listeleme). Doluysa Databases genelde boştur.
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exitCode"`
}

// Counts, başarılı, hatalı ve atlanan veritabanı sayılarını döner.
func (r *Result) Counts() (ok, failed, skipped int) {
	for _, d := range r.Databases {
		switch d.Status {
		case StatusSuccess:
			ok++
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}
	return
}

func (r *Result) exitCode() int {
	if r.Error != "" {
		return ExitFailed
	}
	ok, failed, _ := r.Counts()
	switch {
	case ok == 0:
		return ExitFailed
	case failed > 0:
		return ExitPartial
	}
	return ExitOK
}

// Summary, sonucu insan tarafından okunabilir çok satırlı metin olarak döner.
func (r *Result) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "İş: %s  Sunucu: %s  Süre: %s\n", r.Job, r.Server, msDuration(r.ElapsedMs))
	if r.Error != "" {
		fmt.Fprintf(&b, "HATA: %s\n", r.Error)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "UYARI: %s\n", w)
	}
	for _, d := range r.Databases {
		switch d.Status {
		case StatusSuccess:
			extra := ""
			if d.Verified {
				extra = "  doğrulandı"
			}
			fmt.Fprintf(&b, "  [OK]   %s  %s  %s%s\n", d.Database, formatBytes(d.FileBytes), msDuration(d.ElapsedMs), extra)
		case StatusFailed:
			fmt.Fprintf(&b, "  [HATA] %s  %s\n", d.Database, d.Error)
		case StatusSkipped:
			fmt.Fprintf(&b, "  [ATLA] %s  %s\n", d.Database, d.Error)
		}
	}
	ok, failed, skipped := r.Counts()
	fmt.Fprintf(&b, "Sonuç: %d başarılı, %d hatalı, %d atlandı (çıkış kodu %d)\n", ok, failed, skipped, r.ExitCode)
	return b.String()
}

// Run, işi çalıştırır: bağlanır, hedef veritabanlarını çözümler ve sırayla
// yedekler (opsiyonel doğrulama ile). Bir veritabanının hatası diğerlerini
// durdurmaz. Dönen sonuç hiçbir zaman nil değildir; ExitCode alanı doludur.
func Run(ctx context.Context, j *config.Job, opts Options) *Result {
	rn := newRunner(j, opts)
	rn.run(ctx)
	r := rn.res
	r.End = rn.now()
	r.ElapsedMs = r.End.Sub(r.Start).Milliseconds()
	r.ExitCode = r.exitCode()
	rn.log.Info("iş tamamlandı", "exitCode", r.ExitCode, "elapsed", msDuration(r.ElapsedMs))
	return r
}

type runner struct {
	job      *config.Job
	opts     Options
	log      *slog.Logger
	now      func() time.Time
	res      *Result
	srv      Server
	bopts    mssql.BackupOptions
	usedFile map[string]bool
}

func newRunner(j *config.Job, opts Options) *runner {
	rn := &runner{job: j, opts: opts, usedFile: map[string]bool{}}
	rn.now = opts.Now
	if rn.now == nil {
		rn.now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	rn.log = logger.With("job", j.Name)
	if rn.opts.Connect == nil {
		rn.opts.Connect = connectMSSQL
	}
	rn.res = &Result{Job: j.Name, Start: rn.now(), Databases: []DBResult{}, Warnings: []string{}}
	return rn
}

func (rn *runner) fail(err error) {
	rn.res.Error = err.Error()
	rn.log.Error("iş çalıştırılamadı", "error", err)
}

func (rn *runner) warn(msg string) {
	rn.res.Warnings = append(rn.res.Warnings, msg)
	rn.log.Warn(msg)
}

func (rn *runner) run(ctx context.Context) {
	j := rn.job
	rn.res.Server = configServerName(j.Connection)
	rn.log.Info("iş başladı", "server", rn.res.Server)

	if err := j.Validate(); err != nil {
		rn.fail(fmt.Errorf("iş ayarları geçersiz: %w", err))
		return
	}
	if err := ValidateTemplate(j.Backup.FileNameTemplate); err != nil {
		rn.fail(err)
		return
	}

	var password string
	if j.Connection.Auth == config.AuthSQL {
		var err error
		if password, err = config.Unprotect(j.Connection.PasswordEnc); err != nil {
			rn.fail(fmt.Errorf("kayıtlı şifre çözülemedi; iş dosyası başka bir makineden kopyalandıysa şifreyi yeniden girin: %w", err))
			return
		}
	}

	srv, err := rn.opts.Connect(ctx, j.Connection, password)
	if err != nil {
		rn.fail(err)
		return
	}
	defer srv.Close()
	rn.srv = srv

	info := srv.Info()
	if info.ServerName != "" {
		rn.res.Server = info.ServerName
	}
	rn.res.Edition, rn.res.Protocol = info.Edition, info.Protocol
	rn.log.Info("sunucuya bağlanıldı", "server", rn.res.Server, "edition", info.Edition,
		"version", info.ProductVersion, "protocol", info.Protocol, "login", info.LoginName)

	rn.bopts = mssql.BackupOptions{
		Compression: j.Backup.Compression,
		Checksum:    j.Backup.Checksum,
		CopyOnly:    j.Backup.CopyOnly,
		Description: "SqlBackup: " + j.Name,
	}
	if rn.bopts.Compression && !info.SupportsCompression() {
		rn.warn(fmt.Sprintf("sunucu sürümü (%s) yedek sıkıştırmayı desteklemiyor; sıkıştırma kapatıldı", info.Edition))
		rn.bopts.Compression = false
	}

	dbs, err := srv.ListDatabases(ctx)
	if err != nil {
		rn.fail(err)
		return
	}
	targets := ResolveDatabases(j, dbs)
	if len(targets) == 0 {
		rn.fail(errors.New("yedeklenecek veritabanı bulunamadı"))
		return
	}

	total := 0
	for _, t := range targets {
		if t.Reason == "" {
			total++
		}
	}
	rn.log.Info("hedef veritabanları belirlendi", "count", total, "listed", len(targets))

	index := 0
	for _, t := range targets {
		if t.Reason != "" {
			rn.addUnplanned(t)
			continue
		}
		index++
		if ctx.Err() != nil {
			at := rn.now()
			rn.res.Databases = append(rn.res.Databases, DBResult{Database: t.Name, Status: StatusFailed,
				Start: at, End: at, Error: "işlem iptal edildi"})
			continue
		}
		rn.res.Databases = append(rn.res.Databases, rn.backupOne(ctx, t.Name, index, total))
	}
}

// addUnplanned, çözümleme aşamasında elenen veritabanını sonuca ekler.
func (rn *runner) addUnplanned(t Target) {
	at := rn.now()
	d := DBResult{Database: t.Name, Status: StatusSkipped, Start: at, End: at, Error: t.Reason}
	if t.Failed {
		d.Status = StatusFailed
		rn.log.Error("veritabanı yedeklenemiyor", "db", t.Name, "reason", t.Reason)
	} else {
		rn.log.Warn("veritabanı atlandı", "db", t.Name, "reason", t.Reason)
	}
	rn.res.Databases = append(rn.res.Databases, d)
}

func (rn *runner) backupOne(ctx context.Context, name string, index, total int) (d DBResult) {
	j := rn.job
	d = DBResult{Database: name, Start: rn.now(), Compressed: rn.bopts.Compression}
	log := rn.log.With("db", name)
	defer func() {
		d.End = rn.now()
		d.ElapsedMs = d.End.Sub(d.Start).Milliseconds()
		if d.Error != "" {
			d.Status = StatusFailed
			log.Error("yedek başarısız", "error", d.Error, "elapsed", msDuration(d.ElapsedMs))
		} else {
			d.Status = StatusSuccess
			log.Info("yedek tamamlandı", "file", d.File, "size", formatBytes(d.FileBytes),
				"verified", d.Verified, "elapsed", msDuration(d.ElapsedMs))
		}
	}()

	// Tüm dosyalar çalışma başlangıç zamanıyla adlandırılır; böylece aynı
	// çalışmanın dosyaları birlikte gruplanabilir (zip, retention).
	fileName, err := ExpandTemplate(j.Backup.FileNameTemplate, TemplateVars{
		Server: rn.res.Server, Database: name, Job: j.Name, Time: rn.res.Start})
	if err != nil {
		d.Error = err.Error()
		return
	}
	d.File = joinServerPath(j.Backup.ServerPath, rn.uniqueFileName(fileName))
	log.Info("yedek başlıyor", "file", d.File, "index", index, "total", total)

	cctx, cancel := rn.commandContext(ctx)
	err = rn.srv.BackupDatabase(cctx, name, d.File, rn.bopts, rn.progress(name, index, total, PhaseBackup))
	cancel()
	if err != nil {
		d.Error = err.Error()
		return
	}

	if info, err := rn.srv.LastBackupSet(ctx, name, d.File); err != nil {
		log.Warn("yedek boyutu okunamadı", "error", err)
	} else if info != nil {
		d.SizeBytes, d.FileBytes = info.SizeBytes, info.CompressedBytes
	}

	if j.Backup.Verify {
		cctx, cancel := rn.commandContext(ctx)
		err := rn.srv.VerifyBackup(cctx, d.File, rn.bopts.Checksum, rn.progress(name, index, total, PhaseVerify))
		cancel()
		if err != nil {
			d.Error = err.Error()
			return
		}
		d.Verified = true
	}
	return
}

// commandContext, iş ayarındaki komut zaman aşımını uygular (0 = sınırsız).
func (rn *runner) commandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if m := rn.job.Backup.CommandTimeoutMin; m > 0 {
		return context.WithTimeout(ctx, time.Duration(m)*time.Minute)
	}
	return context.WithCancel(ctx)
}

func (rn *runner) progress(db string, index, total int, phase Phase) mssql.ProgressFunc {
	if rn.opts.Progress == nil {
		return nil
	}
	return func(pct int) {
		rn.opts.Progress(Progress{Database: db, Index: index, Total: total, Phase: phase, Percent: pct})
	}
}

// uniqueFileName, aynı çalışmada iki veritabanı aynı dosya adına düşerse
// (ör. şablonda {db} temizlendikten sonra çakışma) sonuna _2, _3 ekler.
// Windows dosya adları büyük/küçük harf duyarsız olduğu için karşılaştırma da öyledir.
func (rn *runner) uniqueFileName(name string) string {
	base, ext := name[:len(name)-len(".bak")], name[len(name)-len(".bak"):]
	candidate := name
	for i := 2; rn.usedFile[strings.ToLower(candidate)]; i++ {
		candidate = fmt.Sprintf("%s_%d%s", base, i, ext)
	}
	rn.usedFile[strings.ToLower(candidate)] = true
	return candidate
}

// joinServerPath, sunucu tarafındaki klasör ile dosya adını birleştirir.
// Yol sunucunun bakış açısıyla yorumlandığı için filepath kullanılmaz.
func joinServerPath(dir, name string) string {
	return strings.TrimRight(strings.TrimSpace(dir), `\/`) + `\` + name
}

func configServerName(c config.Connection) string {
	s := strings.TrimSpace(c.Server)
	if c.Instance != "" && !strings.Contains(s, `\`) {
		s += `\` + c.Instance
	}
	return s
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func msDuration(ms int64) string {
	return (time.Duration(ms) * time.Millisecond).Round(100 * time.Millisecond).String()
}
