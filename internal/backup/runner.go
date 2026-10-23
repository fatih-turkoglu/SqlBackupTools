package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"sqlbackup/internal/archive"
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
	PhaseZip    Phase = "zip"
)

// Progress, bir veritabanının yedek/doğrulama ilerlemesidir. Zip aşamasında
// Database, DB başına zip'te veritabanı adı, tek zip'te iş adıdır; Index ve
// Total zip dosyalarını sayar.
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

// ArchiveResult, oluşturulan (veya oluşturulamayan) bir zip dosyasının sonucudur.
type ArchiveResult struct {
	// File, zip dosyasının erişim yolu (accessPath) üzerindeki yoludur.
	File        string   `json:"file"`
	Databases   []string `json:"databases"`
	SizeBytes   int64    `json:"sizeBytes"`
	SourceBytes int64    `json:"sourceBytes"`
	BakDeleted  bool     `json:"bakDeleted"`
	ElapsedMs   int64    `json:"elapsedMs"`
	Error       string   `json:"error,omitempty"`
}

// Result, bir iş çalışmasının özetidir.
type Result struct {
	Job       string          `json:"job"`
	Server    string          `json:"server"`
	Edition   string          `json:"edition,omitempty"`
	Protocol  string          `json:"protocol,omitempty"`
	Start     time.Time       `json:"start"`
	End       time.Time       `json:"end"`
	ElapsedMs int64           `json:"elapsedMs"`
	Databases []DBResult      `json:"databases"`
	Archives  []ArchiveResult `json:"archives"`
	Warnings  []string        `json:"warnings"`
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

func (r *Result) archiveErrors() (n int) {
	for _, a := range r.Archives {
		if a.Error != "" {
			n++
		}
	}
	return
}

func (r *Result) exitCode() int {
	if r.Error != "" {
		return ExitFailed
	}
	ok, failed, _ := r.Counts()
	failed += r.archiveErrors()
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
	for _, a := range r.Archives {
		if a.Error != "" {
			fmt.Fprintf(&b, "  [ZIP HATA] %s  %s\n", a.File, a.Error)
			continue
		}
		extra := ""
		if a.BakDeleted {
			extra = "  .bak silindi"
		}
		fmt.Fprintf(&b, "  [ZIP]  %s  %s → %s  %s%s\n", a.File, formatBytes(a.SourceBytes),
			formatBytes(a.SizeBytes), msDuration(a.ElapsedMs), extra)
	}
	ok, failed, skipped := r.Counts()
	fmt.Fprintf(&b, "Sonuç: %d başarılı, %d hatalı, %d atlandı", ok, failed, skipped)
	if n := r.archiveErrors(); n > 0 {
		fmt.Fprintf(&b, ", %d zip hatası", n)
	}
	fmt.Fprintf(&b, " (çıkış kodu %d)\n", r.ExitCode)
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
	// accessErr, iş başında yapılan erişim yolu kontrolünün sonucudur.
	accessErr error
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
	rn.res = &Result{Job: j.Name, Start: rn.now(), Databases: []DBResult{}, Archives: []ArchiveResult{}, Warnings: []string{}}
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

	// Erişim yolu yalnızca zip için gerekir; yedeği sunucu yazar. Bu yüzden
	// erişim yoksa yedekler yine alınır, yalnızca zip adımı hata verir.
	// Kontrol başta yapılır ki sorun uzun yedeklerden önce loglansın.
	if j.Archive.ZipEnabled {
		if rn.accessErr = checkAccess(j.Backup.EffectiveAccessPath()); rn.accessErr != nil {
			rn.log.Error("yedek klasörüne erişilemiyor; zip yapılmayacak", "error", rn.accessErr)
		}
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

	if j.Archive.ZipEnabled {
		rn.archive(ctx)
	}
}

// archive, başarılı yedekleri zip'ler; seçenek açıksa doğrulanmış zip
// sonrası .bak dosyalarını siler. Zip başarısızsa .bak'a dokunulmaz.
// Doğrulaması başarısız olan yedekler başarılı sayılmadığı için zip'lenmez.
func (rn *runner) archive(ctx context.Context) {
	j := rn.job
	var groups [][]DBResult
	for _, d := range rn.res.Databases {
		if d.Status != StatusSuccess {
			continue
		}
		if j.Archive.ZipPerDatabase || len(groups) == 0 {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], d)
	}

	dir := j.Backup.EffectiveAccessPath()
	for gi, g := range groups {
		label := j.Name
		if j.Archive.ZipPerDatabase {
			label = g[0].Database
		}
		a, err := rn.zipGroup(ctx, dir, g, label, gi+1, len(groups))
		if err != nil {
			a.Error = err.Error()
			rn.log.Error("zip başarısız", "file", a.File, "error", err)
		} else {
			rn.log.Info("zip tamamlandı", "file", a.File, "size", formatBytes(a.SizeBytes),
				"source", formatBytes(a.SourceBytes), "bakDeleted", a.BakDeleted, "elapsed", msDuration(a.ElapsedMs))
		}
		rn.res.Archives = append(rn.res.Archives, a)
	}
}

func (rn *runner) zipGroup(ctx context.Context, dir string, g []DBResult, label string, index, total int) (a ArchiveResult, err error) {
	j := rn.job
	start := rn.now()
	defer func() { a.ElapsedMs = rn.now().Sub(start).Milliseconds() }()

	var files []string
	for _, d := range g {
		a.Databases = append(a.Databases, d.Database)
		files = append(files, filepath.Join(dir, serverBaseName(d.File)))
	}
	// DB başına zip'te .bak adı kullanılır; tek zip'te şablondaki {db}
	// yerine iş adı yazılır.
	name := serverBaseName(g[0].File)
	if !j.Archive.ZipPerDatabase {
		if name, err = ExpandTemplate(j.Backup.FileNameTemplate, TemplateVars{
			Server: rn.res.Server, Database: j.Name, Job: j.Name, Time: rn.res.Start}); err != nil {
			return a, err
		}
	}
	a.File = filepath.Join(dir, name[:len(name)-len(".bak")]+".zip")

	if rn.accessErr != nil {
		return a, rn.accessErr
	}
	if err := ctx.Err(); err != nil {
		return a, errors.New("işlem iptal edildi")
	}
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			a.SourceBytes += st.Size()
		}
	}

	rn.log.Info("zip başlıyor", "file", a.File, "files", len(files))
	lastPct := -1
	a.SizeBytes, err = archive.Create(ctx, a.File, files, func(done, size int64) {
		if rn.opts.Progress == nil {
			return
		}
		pct := 100
		if size > 0 {
			pct = int(done * 100 / size)
		}
		if pct != lastPct {
			lastPct = pct
			rn.opts.Progress(Progress{Database: label, Index: index, Total: total, Phase: PhaseZip, Percent: pct})
		}
	})
	if err != nil {
		return a, err
	}

	if j.Archive.DeleteBakAfterZip {
		var errs []error
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return a, fmt.Errorf("zip oluşturuldu ancak .bak silinemedi: %w", errors.Join(errs...))
		}
		a.BakDeleted = true
	}
	return a, nil
}

// checkAccess, archive.CheckAccess hatasını yapılması gerekene dair bir
// açıklamayla zenginleştirir.
func checkAccess(dir string) error {
	err := archive.CheckAccess(dir)
	if err == nil {
		return nil
	}
	account := "bilinmiyor"
	if u, uerr := user.Current(); uerr == nil {
		account = u.Username
	}
	// Windows hata metinleri noktayla biter ("Access is denied."); çift nokta olmasın.
	msg := strings.TrimRight(err.Error(), ". ")
	return fmt.Errorf("yedek klasörüne erişilemiyor, zip yapılamadı: %s. Klasör hem SQL Server servis hesabının "+
		"hem de bu programı çalıştıran hesabın (%s) okuma/yazma/silme izni olan bir yer olmalı "+
		"(ör. D:\\SqlBackups); uzak sunucuda erişim yolu bir UNC yolu olmalı", msg, account)
}

// serverBaseName, sunucu tarafındaki yolun dosya adını döner.
func serverBaseName(p string) string {
	return p[strings.LastIndexAny(p, `\/`)+1:]
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
