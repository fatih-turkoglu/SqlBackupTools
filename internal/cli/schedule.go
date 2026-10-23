package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/scheduler"
)

func (e *env) taskAPI() scheduler.Service {
	if e.tasks != nil {
		return e.tasks
	}
	return scheduler.System{}
}

// userName, "current" hesabıyla çalışacak görevin kullanıcısıdır (DOMAIN\ad).
func (e *env) userName() string {
	if e.user != "" {
		return e.user
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// cmdBatch, işi çalıştıran .bat dosyasını yazar (varsayılan jobs\<ad>.bat).
func (e *env) cmdBatch(args []string) int {
	fs, jobArg := e.newFlags("batch")
	dest := fs.String("path", "", "")
	if !e.parse(fs, jobArg, args) {
		return backup.ExitFailed
	}
	j, path := e.loadJob(*jobArg)
	if j == nil {
		return backup.ExitFailed
	}
	if err := config.ValidateName(j.Name); err != nil {
		e.errorf(err)
		return backup.ExitFailed
	}
	if err := j.Validate(); err != nil {
		e.warnf(i18n.T("cli.warn.jobInvalid", "err", err))
	}
	cmd, err := scheduler.NewCommand(path)
	if err != nil {
		e.errorf(err)
		return backup.ExitFailed
	}
	content, err := scheduler.Batch(cmd, j.Name)
	if err != nil {
		e.errorf(err)
		return backup.ExitFailed
	}
	out := strings.TrimSpace(*dest)
	if out == "" {
		out = scheduler.DefaultBatchPath(j.Name)
	}
	if err := scheduler.WriteBatch(out, content); err != nil {
		e.errorf(err)
		return backup.ExitFailed
	}
	fmt.Fprintln(e.stdout, i18n.T("cli.batch.saved", "path", out))
	return backup.ExitOK
}

// cmdTask, zamanlanmış görev komutlarını (create, delete, run, status) işler.
// Görev Zamanlayıcı erişimi reddederse --elevate ile komut UAC üzerinden
// yönetici yetkisiyle yeniden çalıştırılır. Başarı 0, hata 2; status görev
// yoksa 1 döner.
func (e *env) cmdTask(args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		e.errorf(i18n.T("cli.err.noTaskCommand"))
		return backup.ExitFailed
	}
	sub, rest := strings.ToLower(args[0]), args[1:]
	switch sub {
	case "create", "delete", "run", "status":
	default:
		e.errorf(i18n.T("cli.err.unknownTaskCommand", "cmd", args[0]))
		return backup.ExitFailed
	}
	fs, jobArg := e.newFlags("task " + sub)
	elevate := fs.Bool("elevate", false, "")
	asJSON := fs.Bool("json", false, "")
	if !e.parse(fs, jobArg, rest) {
		return backup.ExitFailed
	}

	var code int
	var err error
	switch sub {
	case "create":
		code, err = e.taskCreate(*jobArg)
	case "delete":
		code, err = e.taskSimple(*jobArg, "cli.task.deleted", e.taskAPI().Delete)
	case "run":
		code, err = e.taskSimple(*jobArg, "cli.task.started", e.taskAPI().Run)
	case "status":
		code, err = e.taskStatus(*jobArg, *asJSON)
	}
	if err == nil {
		return code
	}
	if errors.Is(err, scheduler.ErrAccessDenied) {
		if *elevate && !e.elevated() {
			elevArgs := []string{"task", sub, "--job", elevatedJobArg(*jobArg)}
			if *asJSON {
				elevArgs = append(elevArgs, "--json")
			}
			return e.runElevated(elevArgs)
		}
		e.errorf(i18n.T("cli.err.needAdmin", "err", err))
		return backup.ExitFailed
	}
	e.errorf(err)
	return backup.ExitFailed
}

func (e *env) elevated() bool {
	if e.isElevated != nil {
		return e.isElevated()
	}
	return scheduler.IsElevated()
}

// elevatedJobArg, --job değerini yükseltilmiş sürecin de çözebileceği hale
// getirir: dosya yolu mutlak yola çevrilir (çalışma klasörü farklı olabilir).
func elevatedJobArg(arg string) string {
	p := jobPath(arg)
	if p == config.JobPath(strings.TrimSpace(arg)) {
		return strings.TrimSpace(arg)
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// runElevated, komutu yönetici yetkisiyle çalıştırır ve çıktısını aktarır.
func (e *env) runElevated(args []string) int {
	code, out, err := RunElevated(args, e.runAsAdmin)
	if err != nil {
		e.errorf(err)
		return backup.ExitFailed
	}
	io.WriteString(e.stdout, out)
	return code
}

// RunElevated, programın komut satırını verilen argümanlarla yönetici
// yetkisiyle (UAC) çalıştırır, bitmesini bekler; çıkış kodunu ve çıktısını
// döner. Yükseltilmiş süreç konsolu ve ortam değişkenlerini devralmadığı
// için dil, veri klasörü ve çıktı dosyası argümanla verilir. Arayüz de
// görev işlemlerinde bunu kullanır. run nil ise scheduler.RunElevated.
func RunElevated(args []string, run func(exe string, args []string) (int, error)) (int, string, error) {
	tmp, err := os.CreateTemp("", "sqlbackup-elevated-*.txt")
	if err != nil {
		return 0, "", i18n.Errorf("err.scheduler.elevate", "err", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	args = append(append([]string(nil), args...), "--lang", i18n.Language(), "--output", tmp.Name())
	if h := os.Getenv(config.HomeEnv); h != "" {
		args = append(args, "--home", h)
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, "", i18n.Errorf("err.scheduler.exePath", "err", err)
	}
	if run == nil {
		run = scheduler.RunElevated
	}
	code, err := run(exe, args)
	if err != nil {
		return 0, "", err
	}
	out, _ := os.ReadFile(tmp.Name())
	return code, string(out), nil
}

// taskJobName, --job değerinden görev adını bulur. Düz iş adıysa iş dosyası
// okunmaz (görev, iş silinmiş olsa da yönetilebilsin); dosya yoluysa ad
// dosyadan okunur.
func (e *env) taskJobName(arg string) (string, bool) {
	name := strings.TrimSpace(arg)
	if jobPath(name) != config.JobPath(name) {
		j, _ := e.loadJob(arg)
		if j == nil {
			return "", false
		}
		name = j.Name
	}
	if err := config.ValidateName(name); err != nil {
		e.errorf(err)
		return "", false
	}
	return name, true
}

// taskCreate, işin zamanlamasıyla görevi oluşturur veya günceller.
func (e *env) taskCreate(jobArg string) (int, error) {
	j, path := e.loadJob(jobArg)
	if j == nil {
		return backup.ExitFailed, nil
	}
	if err := j.Validate(); err != nil {
		e.errorf(err)
		return backup.ExitFailed, nil
	}
	cmd, err := scheduler.NewCommand(path)
	if err != nil {
		return 0, err
	}
	u := e.userName()
	x, err := scheduler.TaskXML(j, scheduler.TaskOptions{Command: cmd, User: u, Now: e.now()})
	if err != nil {
		return 0, err
	}
	if err := e.taskAPI().Create(j.Name, x, j.Schedule.RunAs); err != nil {
		return 0, err
	}
	fmt.Fprintln(e.stdout, i18n.T("cli.task.created", "path", scheduler.TaskPath(j.Name)))
	for _, w := range scheduler.Warnings(j, u) {
		e.warnf(w)
	}
	if st, err := e.taskAPI().Query(j.Name); err == nil && st.NextRun != nil {
		fmt.Fprintln(e.stdout, i18n.T("cli.task.nextRunAt", "time", st.NextRun.Format("2006-01-02 15:04")))
	}
	return backup.ExitOK, nil
}

// taskSimple, yalnızca görev adı gerektiren bir işlemi (sil, çalıştır) yapar.
func (e *env) taskSimple(jobArg, doneKey string, op func(string) error) (int, error) {
	name, ok := e.taskJobName(jobArg)
	if !ok {
		return backup.ExitFailed, nil
	}
	if err := op(name); err != nil {
		if errors.Is(err, scheduler.ErrTaskNotFound) {
			e.errorf(i18n.T("cli.task.notFound", "path", scheduler.TaskPath(name)))
			return backup.ExitFailed, nil
		}
		return 0, err
	}
	fmt.Fprintln(e.stdout, i18n.T(doneKey, "path", scheduler.TaskPath(name)))
	return backup.ExitOK, nil
}

// taskStatus, görevin durumunu yazar; görev yoksa 1 döner.
func (e *env) taskStatus(jobArg string, asJSON bool) (int, error) {
	name, ok := e.taskJobName(jobArg)
	if !ok {
		return backup.ExitFailed, nil
	}
	st, err := e.taskAPI().Query(name)
	if err != nil {
		return 0, err
	}
	if asJSON {
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(st); err != nil {
			return 0, err
		}
	} else if !st.Exists {
		fmt.Fprintln(e.stdout, i18n.T("cli.task.notFound", "path", st.Path))
	} else {
		printTaskStatus(e.stdout, st)
		if exe, err := os.Executable(); err == nil && st.Definition.Command != "" &&
			!strings.EqualFold(filepath.Clean(st.Definition.Command), filepath.Clean(exe)) {
			e.warnf(i18n.T("cli.warn.taskExeMismatch", "exe", st.Definition.Command))
		}
	}
	if !st.Exists {
		return backup.ExitPartial, nil
	}
	return backup.ExitOK, nil
}

func printTaskStatus(w0 io.Writer, st scheduler.TaskStatus) {
	w := tabwriter.NewWriter(w0, 0, 0, 2, ' ', 0)
	row := func(key, v string) { fmt.Fprintf(w, "%s:\t%s\n", i18n.T(key), v) }
	row("cli.task.path", st.Path)
	state := taskStateLabel(st.State)
	if !st.Enabled {
		state = i18n.T("cli.task.state.disabled")
	}
	row("cli.task.state", state)
	account := st.Definition.User
	if st.Definition.RunAs == config.RunAsSystem {
		account = "SYSTEM"
	}
	row("cli.task.account", account)
	row("cli.task.lastRun", formatTime(st.LastRun))
	if st.LastRun != nil {
		row("cli.task.lastResult", taskResultLabel(st.LastResult))
	}
	row("cli.task.nextRun", formatTime(st.NextRun))
	row("cli.task.command", strings.TrimSpace(`"`+st.Definition.Command+`" `+st.Definition.Arguments))
	w.Flush()
}

func formatTime(t *time.Time) string {
	if t == nil {
		return i18n.T("cli.task.never")
	}
	return t.Format("2006-01-02 15:04:05")
}

func taskStateLabel(s string) string {
	switch s {
	case scheduler.StateReady:
		return i18n.T("cli.task.state.ready")
	case scheduler.StateRunning:
		return i18n.T("cli.task.state.running")
	case scheduler.StateQueued:
		return i18n.T("cli.task.state.queued")
	case scheduler.StateDisabled:
		return i18n.T("cli.task.state.disabled")
	}
	return i18n.T("common.unknown")
}

// taskResultLabel, son çalışmanın çıkış kodunu açıklar: programın kodları
// (0-3) ve Görev Zamanlayıcı'nın kendi kodları.
func taskResultLabel(code uint32) string {
	var text string
	switch code {
	case backup.ExitOK:
		text = i18n.T("cli.task.result.ok")
	case backup.ExitPartial:
		text = i18n.T("cli.task.result.partial")
	case backup.ExitFailed:
		text = i18n.T("cli.task.result.failed")
	case backup.ExitBusy:
		text = i18n.T("cli.task.result.alreadyRunning")
	case scheduler.ResultRunning:
		return i18n.T("cli.task.result.running")
	case scheduler.ResultNotRun:
		return i18n.T("cli.task.never")
	default:
		return i18n.T("cli.task.result.other", "code", fmt.Sprintf("0x%08X", code))
	}
	return i18n.T("cli.task.result.code", "code", code, "text", text)
}
