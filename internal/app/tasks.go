package app

import (
	"errors"
	"os/user"
	"strings"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/scheduler"
)

// TaskInfo, işin zamanlanmış görev durumudur.
type TaskInfo struct {
	Status scheduler.TaskStatus `json:"status"`
	// Outdated, görev var ama işin kayıtlı zamanlamasını, hesabını veya
	// komutunu yansıtmıyor demektir (görev yeniden oluşturulmalı).
	Outdated bool `json:"outdated"`
	// Warnings, işin görev olarak çalışmasıyla ilgili uyarılardır.
	Warnings []string `json:"warnings"`
}

// TaskActionResult, görev işleminin sonucudur.
type TaskActionResult struct {
	// Elevated, işlemin yönetici yetkisiyle (UAC) yapıldığını belirtir.
	Elevated bool     `json:"elevated"`
	Task     TaskInfo `json:"task"`
}

func (a *App) userName() string {
	if a.user != "" {
		return a.user
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

func jobCommand(name string) (scheduler.Command, error) {
	return scheduler.NewCommand(config.JobPath(name))
}

// taskInfo, işin görev durumunu okur ve işle karşılaştırır.
func (a *App) taskInfo(j *config.Job) (TaskInfo, error) {
	st, err := a.tasks.Query(j.Name)
	if err != nil {
		return TaskInfo{}, err
	}
	ti := TaskInfo{Status: st, Warnings: scheduler.Warnings(j, a.userName())}
	if ti.Warnings == nil {
		ti.Warnings = []string{}
	}
	if st.Exists {
		cmd, err := jobCommand(j.Name)
		ti.Outdated = err != nil || !st.Definition.Matches(j, cmd)
	}
	return ti, nil
}

// withElevation, işlemi yapar; Görev Zamanlayıcı erişimi reddederse ve süreç
// yükseltilmemişse aynı işlemi CLI üzerinden yönetici yetkisiyle (UAC) yapar:
// SqlBackup.exe task <sub> --job <ad>.
func (a *App) withElevation(op func() error, sub, name string) (bool, error) {
	err := op()
	if !errors.Is(err, scheduler.ErrAccessDenied) || scheduler.IsElevated() {
		return false, err
	}
	code, out, err := a.elevate([]string{"task", sub, "--job", name})
	if err != nil {
		return true, err
	}
	if code != 0 {
		if out = strings.TrimSpace(out); out != "" {
			return true, errors.New(out) // yükseltilmiş sürecin (seçili dildeki) çıktısı
		}
		return true, i18n.Errorf("err.app.elevatedExit", "code", code)
	}
	return true, nil
}

// GetTaskStatus, işin görev durumunu döner.
func (a *App) GetTaskStatus(name string) (TaskInfo, error) {
	j, err := loadJob(name)
	if err != nil {
		return TaskInfo{}, err
	}
	return a.taskInfo(j)
}

// TaskWarnings, (kaydedilmemiş olabilir) işin görev olarak çalışmasıyla
// ilgili uyarıları döner; zamanlama ekranında gösterilir.
func (a *App) TaskWarnings(job config.Job) []string {
	w := scheduler.Warnings(&job, a.userName())
	if w == nil {
		w = []string{}
	}
	return w
}

// CreateTask, kayıtlı işin zamanlamasıyla görevi oluşturur veya günceller.
// SYSTEM görevi yönetici yetkisi ister; gerekirse UAC ile yapılır.
func (a *App) CreateTask(name string) (TaskActionResult, error) {
	j, err := loadJob(name)
	if err != nil {
		return TaskActionResult{}, err
	}
	if err := j.Validate(); err != nil {
		return TaskActionResult{}, i18n.Errorf("err.backup.invalidJob", "err", err)
	}
	cmd, err := jobCommand(j.Name)
	if err != nil {
		return TaskActionResult{}, err
	}
	x, err := scheduler.TaskXML(j, scheduler.TaskOptions{Command: cmd, User: a.userName(), Now: a.now()})
	if err != nil {
		return TaskActionResult{}, err
	}
	elevated, err := a.withElevation(func() error { return a.tasks.Create(j.Name, x, j.Schedule.RunAs) }, "create", j.Name)
	if err != nil {
		return TaskActionResult{Elevated: elevated}, err
	}
	ti, err := a.taskInfo(j)
	return TaskActionResult{Elevated: elevated, Task: ti}, err
}

// DeleteTask, işin görevini siler.
func (a *App) DeleteTask(name string) (TaskActionResult, error) {
	return a.taskAction(name, "delete", a.tasks.Delete)
}

// RunTask, görevi Görev Zamanlayıcı üzerinden (görevin hesabıyla) hemen başlatır.
func (a *App) RunTask(name string) (TaskActionResult, error) {
	return a.taskAction(name, "run", a.tasks.Run)
}

func (a *App) taskAction(name, sub string, op func(string) error) (TaskActionResult, error) {
	j, err := loadJob(name)
	if err != nil {
		return TaskActionResult{}, err
	}
	elevated, err := a.withElevation(func() error { return op(j.Name) }, sub, j.Name)
	if errors.Is(err, scheduler.ErrTaskNotFound) {
		err = i18n.Errorf("cli.task.notFound", "path", scheduler.TaskPath(j.Name))
	}
	if err != nil {
		return TaskActionResult{Elevated: elevated}, err
	}
	ti, err := a.taskInfo(j)
	return TaskActionResult{Elevated: elevated, Task: ti}, err
}

// DefaultBatchPath, işin varsayılan .bat yoludur (jobs\<ad>.bat).
func (a *App) DefaultBatchPath(name string) string {
	return scheduler.DefaultBatchPath(name)
}

// SaveBatch, işi çalıştıran .bat dosyasını yazar ve yolunu döner; path
// boşsa varsayılan konum kullanılır.
func (a *App) SaveBatch(name, path string) (string, error) {
	j, err := loadJob(name)
	if err != nil {
		return "", err
	}
	cmd, err := jobCommand(j.Name)
	if err != nil {
		return "", err
	}
	content, err := scheduler.Batch(cmd, j.Name)
	if err != nil {
		return "", err
	}
	if path = strings.TrimSpace(path); path == "" {
		path = scheduler.DefaultBatchPath(j.Name)
	}
	return path, scheduler.WriteBatch(path, content)
}
