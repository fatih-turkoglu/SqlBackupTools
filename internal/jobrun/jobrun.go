// Package jobrun, bir işin log ve çalışma kaydıyla birlikte çalıştırılmasıdır.
// CLI (zamanlanmış görev dahil) ve arayüz aynı akışı kullanır:
//
//	EnsureDirs → OpenJobLog → backup.Run → SaveRun → Cleanup
package jobrun

import (
	"context"
	"log/slog"
	"os/user"
	"strings"
	"time"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/logging"
)

// Source, çalışmayı kimin başlattığıdır (log kaydında belirtilir).
type Source int

const (
	SourceCLI Source = iota
	SourceGUI
)

// Options, Execute davranışını belirler. Tüm alanlar opsiyoneldir.
type Options struct {
	Source Source
	// JobFile ve Version, başlangıç log kaydına yazılır.
	JobFile string
	Version string
	// Handlers, log dosyasına ek olarak kayıtların gönderileceği işleyicilerdir (CLI -v).
	Handlers []slog.Handler
	Progress func(backup.Progress)
	Connect  backup.ConnectFunc
	Now      func() time.Time
	// KeepDays, log temizliğinin saklama süresidir; 0 ise varsayılan.
	KeepDays int
	// Warn, çalışmayı durdurmayan sorunları (log dosyası açılamadı, kayıt
	// yazılamadı vb.) bildirir.
	Warn func(error)
}

// Execute, işi çalıştırır ve sonucu döner (hiçbir zaman nil değil). İş adı
// geçersizse log ve kayıt dosyası açılmaz (ad dosya adında kullanılır).
func Execute(ctx context.Context, j *config.Job, o Options) *backup.Result {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	warn := o.Warn
	if warn == nil {
		warn = func(error) {}
	}
	keepDays := o.KeepDays
	if keepDays <= 0 {
		keepDays = config.DefaultLogKeepDays
	}
	if err := config.ValidateName(j.Name); err != nil {
		t := now()
		return &backup.Result{Job: j.Name, Start: t, End: t, Error: err.Error(), Warnings: []string{},
			Databases: []backup.DBResult{}, Archives: []backup.ArchiveResult{}, ExitCode: backup.ExitFailed}
	}

	// Klasörler log açılmadan önce oluşturulmalı: log klasörünün izinleri
	// yalnızca EnsureDirs ile ilk oluşturmada verilir.
	if err := config.EnsureDirs(); err != nil {
		warn(err)
	}
	logsDir := config.LogsDir()
	handlers := append([]slog.Handler(nil), o.Handlers...)
	jl, err := logging.OpenJobLog(logsDir, j.Name, now())
	if err != nil {
		warn(i18n.Errorf("warn.run.noLogFile", "err", err))
	} else {
		defer jl.Close()
		handlers = append(handlers, jl.Handler(slog.LevelInfo))
	}
	log := slog.New(slog.NewMultiHandler(handlers...))
	account := ""
	if u, err := user.Current(); err == nil {
		account = u.Username
	}
	startedKey := "log.cli.started"
	if o.Source == SourceGUI {
		startedKey = "log.gui.started"
	}
	log.Info(i18n.T(startedKey), "job", j.Name, "file", o.JobFile, "version", o.Version,
		"account", account, "lang", i18n.Language())

	r := backup.Run(ctx, j, backup.Options{
		Logger:   log,
		Progress: o.Progress,
		Connect:  o.Connect,
		Now:      now,
	})

	if p, err := logging.SaveRun(logsDir, j.Name, r.Start, r); err != nil {
		warn(err)
		log.Warn(i18n.T("log.run.saveFailed"), "error", err)
	} else {
		log.Info(i18n.T("log.run.saved"), "file", p)
	}
	if n, err := logging.Cleanup(logsDir, keepDays, now()); err != nil {
		log.Warn(i18n.T("log.cleanup.failed"), "error", err)
	} else if n > 0 {
		log.Info(i18n.T("log.cleanup.done"), "deleted", n, "keepDays", keepDays)
	}
	if m := i18n.Missing(); len(m) > 0 {
		log.Warn(i18n.T("log.i18n.missing"), "keys", strings.Join(m, ","))
	}
	return r
}
