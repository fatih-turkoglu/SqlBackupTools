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
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/logging"
	"sqlbackup/internal/mssql"
)

// cmdRun, işi çalıştırır: log dosyasını açar, yedeği alır, özeti yazar,
// çalışma kaydını kaydeder ve eski logları temizler.
func (e *env) cmdRun(args []string) int {
	fs, jobArg := e.newFlags("run")
	verbose := fs.Bool("v", false, "")
	if !e.parse(fs, jobArg, args) {
		return backup.ExitFailed
	}
	j, path := e.loadJob(*jobArg)
	if j == nil {
		return backup.ExitFailed
	}
	// Ad log ve kayıt dosyası adında kullanılır; geçersizse dosya açılmaz.
	if err := config.ValidateName(j.Name); err != nil {
		e.errorf(err)
		return backup.ExitFailed
	}

	// Klasörler log açılmadan önce oluşturulmalı: log klasörünün izinleri
	// yalnızca EnsureDirs ile ilk oluşturmada verilir.
	if err := config.EnsureDirs(); err != nil {
		e.warnf(err)
	}
	logsDir := config.LogsDir()
	start := e.now()
	var handlers []slog.Handler
	jl, err := logging.OpenJobLog(logsDir, j.Name, start)
	if err != nil {
		e.warnf(i18n.T("cli.warn.noLogFile", "err", err))
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
	log.Info(i18n.T("log.cli.started"), "job", j.Name, "file", path, "version", e.version,
		"account", account, "lang", i18n.Language())

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
		e.warnf(err)
		log.Warn(i18n.T("log.run.saveFailed"), "error", err)
	} else {
		log.Info(i18n.T("log.run.saved"), "file", p)
	}
	if n, err := logging.Cleanup(logsDir, e.keepDays, e.now()); err != nil {
		log.Warn(i18n.T("log.cleanup.failed"), "error", err)
	} else if n > 0 {
		log.Info(i18n.T("log.cleanup.done"), "deleted", n, "keepDays", e.keepDays)
	}
	if m := i18n.Missing(); len(m) > 0 {
		log.Warn(i18n.T("log.i18n.missing"), "keys", strings.Join(m, ","))
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
		e.errorf(i18n.T("cli.err.connect", "err", err))
		return backup.ExitFailed
	}
	defer srv.Close()

	info := srv.Info()
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	row := func(key, v string) { fmt.Fprintf(w, "%s:\t%s\n", i18n.T(key), v) }
	row("cli.test.server", info.ServerName)
	row("cli.test.version", strings.TrimSpace(info.ProductVersion+" "+info.ProductLevel))
	row("cli.test.edition", info.Edition)
	row("cli.test.protocol", mssql.ProtocolLabel(info.Protocol))
	row("cli.test.login", i18n.T("cli.test.loginValue", "login", info.LoginName, "sysadmin", yesNo(info.IsSysAdmin)))
	compression := i18n.T("cli.test.compressionNo")
	if info.SupportsCompression() {
		compression = i18n.T("cli.test.compressionYes")
	}
	row("cli.test.compression", compression)
	if info.DefaultBackup != "" {
		row("cli.test.defaultBackupDir", info.DefaultBackup)
	}
	if j.Archive.ZipEnabled || j.Retention.Mode != config.RetentionNone {
		dir := j.Backup.EffectiveAccessPath()
		if err := backup.CheckAccess(dir); err != nil {
			row("cli.test.access", i18n.T("cli.test.accessFailed"))
			w.Flush()
			e.errorf(err)
			return backup.ExitPartial
		}
		row("cli.test.access", i18n.T("cli.test.accessOK", "path", dir))
	}
	w.Flush()
	fmt.Fprintln(e.stdout, i18n.T("cli.test.success"))
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
	asJSON := fs.Bool("json", false, "")
	if !e.parse(fs, jobArg, args) {
		return backup.ExitFailed
	}
	j, _ := e.loadJob(*jobArg)
	if j == nil {
		return backup.ExitFailed
	}
	srv, err := e.openServer(j)
	if err != nil {
		e.errorf(i18n.T("cli.err.connect", "err", err))
		return backup.ExitFailed
	}
	defer srv.Close()
	dbs, err := srv.ListDatabases(e.ctx)
	if err != nil {
		e.errorf(err)
		return backup.ExitFailed
	}

	rows := listRows(j, dbs)
	if *asJSON {
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			e.errorf(err)
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
	fmt.Fprintln(tw, i18n.T("cli.list.header"))
	count := 0
	for _, r := range rows {
		last := "-"
		if r.LastFullBackup != nil {
			last = r.LastFullBackup.Local().Format("2006-01-02 15:04")
		}
		backupCol := i18n.T("cli.no")
		switch {
		case r.Backup:
			backupCol = i18n.T("cli.yes")
			count++
		case r.Failed:
			backupCol = i18n.T("cli.list.failed", "reason", r.Note)
		case r.Note != "":
			backupCol = i18n.T("cli.list.skipped", "reason", r.Note)
		}
		if r.Missing {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\t-\t%s\n", r.Name, backupCol)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%.1f\t%s\t%s\t%s\n", r.Name, r.State, r.RecoveryModel, r.SizeMB,
			yesNo(r.IsSystem), last, backupCol)
	}
	tw.Flush()
	fmt.Fprintln(w, i18n.T("cli.list.footer", "count", len(rows), "backup", count))
}

func yesNo(b bool) string {
	if b {
		return i18n.T("cli.yes")
	}
	return i18n.T("cli.no")
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

func phaseLabel(p backup.Phase) string {
	switch p {
	case backup.PhaseBackup:
		return i18n.T("cli.phase.backup")
	case backup.PhaseVerify:
		return i18n.T("cli.phase.verify")
	case backup.PhaseZip:
		return i18n.T("cli.phase.zip")
	}
	return string(p)
}

func (p *progressPrinter) update(pr backup.Progress) {
	line := fmt.Sprintf("[%d/%d] %s  %s  %%%d", pr.Index, pr.Total, pr.Database, phaseLabel(pr.Phase), pr.Percent)
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
