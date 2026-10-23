package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/user"
	"strings"
	"text/tabwriter"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/logging"
	"sqlbackup/internal/mssql"
)

// cmdRun, işi çalıştırır: log dosyasını açar, yedeği alır, özeti yazar,
// çalışma kaydını kaydeder ve eski logları temizler.
func (e *env) cmdRun(args []string) int {
	fs, jobArg := e.newFlags("run")
	verbose := fs.Bool("v", false, "log kayıtlarını konsola da yaz")
	if !e.parse(fs, jobArg, args) {
		return backup.ExitFailed
	}
	j, path := e.loadJob(*jobArg)
	if j == nil {
		return backup.ExitFailed
	}
	// Ad log ve kayıt dosyası adında kullanılır; geçersizse dosya açılmaz.
	if err := config.ValidateName(j.Name); err != nil {
		fmt.Fprintf(e.stderr, "HATA: %v\n", err)
		return backup.ExitFailed
	}

	logsDir := config.LogsDir()
	start := e.now()
	var handlers []slog.Handler
	jl, err := logging.OpenJobLog(logsDir, j.Name, start)
	if err != nil {
		fmt.Fprintf(e.stderr, "UYARI: %v; log dosyasına yazılmayacak\n", err)
	} else {
		defer jl.Close()
		handlers = append(handlers, jl.Handler(slog.LevelInfo))
	}
	if *verbose {
		handlers = append(handlers, slog.NewTextHandler(e.stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	log := slog.New(slog.NewMultiHandler(handlers...))
	account := ""
	if u, err := user.Current(); err == nil {
		account = u.Username
	}
	log.Info("komut satırından çalıştırıldı", "job", j.Name, "file", path, "version", e.version, "account", account)

	pp := &progressPrinter{w: e.stdout, console: e.console && !*verbose}
	r := backup.Run(e.ctx, j, backup.Options{
		Logger:   log,
		Progress: pp.update,
		Connect:  e.connect,
		Now:      e.now,
	})
	pp.finish()
	fmt.Fprint(e.stdout, r.Summary())

	if p, err := logging.SaveRun(logsDir, j.Name, r.Start, r); err != nil {
		fmt.Fprintf(e.stderr, "UYARI: %v\n", err)
		log.Warn("çalışma kaydı yazılamadı", "error", err)
	} else {
		log.Info("çalışma kaydı yazıldı", "file", p)
	}
	if n, err := logging.Cleanup(logsDir, logging.DefaultKeepDays, e.now()); err != nil {
		log.Warn("eski loglar temizlenemedi", "error", err)
	} else if n > 0 {
		log.Info("eski loglar temizlendi", "deleted", n, "keepDays", logging.DefaultKeepDays)
	}
	return r.ExitCode
}

// cmdTest, bağlantıyı sınar ve sunucu bilgilerini yazar. Zip veya saklama
// açıksa yedek klasörüne erişim de denenir. Bağlantı hatası 2, erişim
// hatası 1 döner.
func (e *env) cmdTest(args []string) int {
	fs, jobArg := e.newFlags("test")
	if !e.parse(fs, jobArg, args) {
		return backup.ExitFailed
	}
	j, _ := e.loadJob(*jobArg)
	if j == nil {
		return backup.ExitFailed
	}
	srv, err := e.openServer(j)
	if err != nil {
		fmt.Fprintf(e.stderr, "HATA: bağlantı kurulamadı: %v\n", err)
		return backup.ExitFailed
	}
	defer srv.Close()

	info := srv.Info()
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	row := func(k, v string) { fmt.Fprintf(w, "%s:\t%s\n", k, v) }
	row("Sunucu", info.ServerName)
	row("Sürüm", strings.TrimSpace(info.ProductVersion+" "+info.ProductLevel))
	row("Edition", info.Edition)
	row("Protokol", mssql.ProtocolLabel(info.Protocol))
	row("Oturum", fmt.Sprintf("%s (sysadmin: %s)", info.LoginName, yesNo(info.IsSysAdmin)))
	row("Sıkıştırma", map[bool]string{true: "destekleniyor", false: "desteklenmiyor"}[info.SupportsCompression()])
	if info.DefaultBackup != "" {
		row("Varsayılan yedek klasörü", info.DefaultBackup)
	}
	if j.Archive.ZipEnabled || j.Retention.Mode != config.RetentionNone {
		dir := j.Backup.EffectiveAccessPath()
		if err := backup.CheckAccess(dir); err != nil {
			row("Yedek klasörü erişimi", "HATA")
			w.Flush()
			fmt.Fprintf(e.stderr, "HATA: %v\n", err)
			return backup.ExitPartial
		}
		row("Yedek klasörü erişimi", "OK ("+dir+")")
	}
	w.Flush()
	fmt.Fprintln(e.stdout, "Bağlantı başarılı.")
	return backup.ExitOK
}

// listRow, list komutunun tek satırıdır (--json çıktısı da bu yapıdadır).
type listRow struct {
	mssql.Database
	// Backup, işin bu veritabanını yedekleyip yedeklemeyeceğidir.
	Backup bool `json:"backup"`
	// Note, yedeklenmeyecekse nedeni; Failed ise çalışmada hata sayılır.
	Note   string `json:"note,omitempty"`
	Failed bool   `json:"failed,omitempty"`
	// Missing, işte seçili olduğu halde sunucuda bulunmadığını belirtir.
	Missing bool `json:"missing,omitempty"`
}

// cmdList, sunucudaki veritabanlarını ve işin hangilerini yedekleyeceğini listeler.
func (e *env) cmdList(args []string) int {
	fs, jobArg := e.newFlags("list")
	asJSON := fs.Bool("json", false, "JSON olarak yaz")
	if !e.parse(fs, jobArg, args) {
		return backup.ExitFailed
	}
	j, _ := e.loadJob(*jobArg)
	if j == nil {
		return backup.ExitFailed
	}
	srv, err := e.openServer(j)
	if err != nil {
		fmt.Fprintf(e.stderr, "HATA: bağlantı kurulamadı: %v\n", err)
		return backup.ExitFailed
	}
	defer srv.Close()
	dbs, err := srv.ListDatabases(e.ctx)
	if err != nil {
		fmt.Fprintf(e.stderr, "HATA: %v\n", err)
		return backup.ExitFailed
	}

	rows := listRows(j, dbs)
	if *asJSON {
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			fmt.Fprintf(e.stderr, "HATA: %v\n", err)
			return backup.ExitFailed
		}
		return backup.ExitOK
	}
	printList(e.stdout, rows)
	return backup.ExitOK
}

// listRows, sunucu listesini işin çözümlemesiyle birleştirir. İşte seçili
// olup sunucuda bulunmayan veritabanları sona eklenir.
func listRows(j *config.Job, dbs []mssql.Database) []listRow {
	resolved := backup.ResolveDatabases(j, dbs)
	targets := map[string]backup.Target{}
	for _, t := range resolved {
		targets[strings.ToLower(t.Name)] = t
	}
	onServer := map[string]bool{}
	rows := make([]listRow, 0, len(dbs))
	for _, d := range dbs {
		key := strings.ToLower(d.Name)
		onServer[key] = true
		r := listRow{Database: d}
		if t, ok := targets[key]; ok {
			r.Backup, r.Note, r.Failed = t.Reason == "", t.Reason, t.Failed
		}
		rows = append(rows, r)
	}
	for _, t := range resolved {
		if !onServer[strings.ToLower(t.Name)] {
			rows = append(rows, listRow{Database: mssql.Database{Name: t.Name}, Note: t.Reason, Failed: t.Failed, Missing: true})
		}
	}
	return rows
}

func printList(w io.Writer, rows []listRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "VERİTABANI\tDURUM\tKURTARMA\tBOYUT (MB)\tSİSTEM\tSON FULL\tYEDEK")
	count := 0
	for _, r := range rows {
		last := "-"
		if r.LastFullBackup != nil {
			last = r.LastFullBackup.Local().Format("2006-01-02 15:04")
		}
		backupCol := "hayır"
		switch {
		case r.Backup:
			backupCol = "evet"
			count++
		case r.Failed:
			backupCol = "HATA: " + r.Note
		case r.Note != "":
			backupCol = "hayır (" + r.Note + ")"
		}
		if r.Missing {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\t-\t%s\n", r.Name, backupCol)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%.1f\t%s\t%s\t%s\n", r.Name, r.State, r.RecoveryModel, r.SizeMB,
			yesNo(r.IsSystem), last, backupCol)
	}
	tw.Flush()
	fmt.Fprintf(w, "%d veritabanı, %d tanesi yedeklenecek\n", len(rows), count)
}

func yesNo(b bool) string {
	if b {
		return "evet"
	}
	return "hayır"
}

// progressPrinter, yedek ilerlemesini konsola yazar. Konsolda tek satır \r
// ile güncellenir; konsol değilse (yönlendirilmiş çıktı, zamanlanmış görev)
// her aşama yalnızca tamamlandığında bir satır yazılır.
type progressPrinter struct {
	w       io.Writer
	console bool
	last    backup.Progress
	open    bool // konsolda bitmemiş bir satır var
}

var phaseLabels = map[backup.Phase]string{
	backup.PhaseBackup: "yedek",
	backup.PhaseVerify: "doğrulama",
	backup.PhaseZip:    "zip",
}

func (p *progressPrinter) update(pr backup.Progress) {
	line := fmt.Sprintf("[%d/%d] %s  %s  %%%d", pr.Index, pr.Total, pr.Database, phaseLabels[pr.Phase], pr.Percent)
	if p.console {
		if p.open && (pr.Database != p.last.Database || pr.Phase != p.last.Phase) {
			fmt.Fprintln(p.w)
		}
		// Önceki satır daha uzunsa artığı boşlukla silinir.
		fmt.Fprintf(p.w, "\r%-60s", line)
		p.open = true
	} else if pr.Percent == 100 && p.last != pr {
		fmt.Fprintln(p.w, line)
	}
	p.last = pr
}

func (p *progressPrinter) finish() {
	if p.open {
		fmt.Fprintln(p.w)
		p.open = false
	}
}
