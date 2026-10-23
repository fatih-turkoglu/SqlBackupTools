package app

import (
	"context"
	"sort"
	"strings"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/jobrun"
)

// Wails olay adları (runtime.EventsOn ile dinlenir).
const (
	EventStarted  = "backup:started"  // StartedEvent
	EventProgress = "backup:progress" // ProgressEvent
	EventDone     = "backup:done"     // DoneEvent
)

// StartedEvent, arayüzden başlatılan bir çalışmanın başladığını bildirir.
type StartedEvent struct {
	Job string `json:"job"`
}

// ProgressEvent, çalışan bir işin ilerlemesidir.
type ProgressEvent struct {
	Job      string          `json:"job"`
	Progress backup.Progress `json:"progress"`
}

// DoneEvent, çalışmanın sonucudur. Warnings, sonuçtan ayrı olarak
// çalışmayı durdurmayan sorunlardır (log dosyası açılamadı vb.).
type DoneEvent struct {
	Job      string         `json:"job"`
	Result   *backup.Result `json:"result"`
	Warnings []string       `json:"warnings"`
}

// runningJob, bu arayüzden başlatılmış bir çalışmadır.
type runningJob struct {
	name   string
	cancel context.CancelFunc
}

// RunJobNow, işi arka planda başlatır ve hemen döner; ilerleme ve sonuç
// olaylarla (EventStarted, EventProgress, EventDone) gelir. İş zaten
// çalışıyorsa (bu arayüzde veya başka bir süreçte, ör. zamanlanmış görev)
// başlatılmaz ve hata döner.
func (a *App) RunJobNow(name string) error {
	j, err := loadJob(name)
	if err != nil {
		return err
	}
	key := strings.ToLower(j.Name)
	a.mu.Lock()
	if _, ok := a.running[key]; ok || backup.IsRunning(j.Name) {
		a.mu.Unlock()
		return i18n.Errorf("err.app.alreadyRunning", "job", j.Name)
	}
	ctx, cancel := context.WithCancel(a.context())
	a.running[key] = runningJob{name: j.Name, cancel: cancel}
	a.wg.Add(1)
	a.mu.Unlock()

	settings, _ := config.LoadSettings()
	a.emit(EventStarted, StartedEvent{Job: j.Name})
	go func() {
		defer a.wg.Done()
		defer cancel()
		warnings := []string{}
		r := jobrun.Execute(ctx, j, jobrun.Options{
			Source:   jobrun.SourceGUI,
			JobFile:  config.JobPath(j.Name),
			Version:  a.version,
			Progress: func(p backup.Progress) { a.emit(EventProgress, ProgressEvent{Job: j.Name, Progress: p}) },
			Connect:  a.connect,
			Now:      a.now,
			KeepDays: settings.EffectiveLogKeepDays(),
			Warn:     func(err error) { warnings = append(warnings, err.Error()) },
		})
		a.mu.Lock()
		delete(a.running, key)
		a.mu.Unlock()
		a.emit(EventDone, DoneEvent{Job: j.Name, Result: r, Warnings: warnings})
	}()
	return nil
}

// CancelJob, bu arayüzden başlatılmış çalışmayı iptal eder; çalışmıyorsa
// false döner. Sonuç yine EventDone ile gelir.
func (a *App) CancelJob(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.running[strings.ToLower(name)]
	if ok {
		r.cancel()
	}
	return ok
}

// IsJobRunning, işin bu arayüzde veya başka bir süreçte çalışıp çalışmadığını döner.
func (a *App) IsJobRunning(name string) bool {
	a.mu.Lock()
	_, ok := a.running[strings.ToLower(name)]
	a.mu.Unlock()
	return ok || backup.IsRunning(name)
}

// RunningJobs, bu arayüzden başlatılıp süren çalışmaları döner (pencere
// kapatılırken uyarı için).
func (a *App) RunningJobs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.running))
	for _, r := range a.running {
		out = append(out, r.name)
	}
	sort.Strings(out)
	return out
}
