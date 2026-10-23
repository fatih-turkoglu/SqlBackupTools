package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/scheduler"
)

// fakeTasks, Görev Zamanlayıcı yerine kullanılır.
type fakeTasks struct {
	created map[string]string // ad → XML
	runAs   string
	err     error // tüm işlemlerin döneceği hata
	status  scheduler.TaskStatus
	deleted []string
	started []string
}

func (f *fakeTasks) Create(name, x, runAs string) error {
	if f.err != nil {
		return f.err
	}
	if f.created == nil {
		f.created = map[string]string{}
	}
	f.created[name], f.runAs = x, runAs
	return nil
}

func (f *fakeTasks) Delete(name string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *fakeTasks) Run(name string) error {
	if f.err != nil {
		return f.err
	}
	f.started = append(f.started, name)
	return nil
}

func (f *fakeTasks) Query(name string) (scheduler.TaskStatus, error) {
	if f.err != nil {
		return scheduler.TaskStatus{}, f.err
	}
	st := f.status
	st.Path = scheduler.TaskPath(name)
	return st, nil
}

func taskEnv(t *testing.T) (*env, *fakeTasks, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	e, out, errb, _ := testEnv(t)
	f := &fakeTasks{}
	e.tasks, e.user = f, `PC01\Fatih TÜRKOĞLU`
	e.isElevated = func() bool { return false }
	e.runAsAdmin = func(string, []string) (int, error) {
		t.Fatal("yükseltme beklenmiyordu")
		return 0, nil
	}
	return e, f, out, errb
}

func TestBatchCommand(t *testing.T) {
	e, _, out, errb := taskEnv(t)
	saveJob(t, "gece", nil)
	if code := e.main([]string{"batch", "--job", "gece"}); code != 0 {
		t.Fatalf("kod=%d %s", code, errb)
	}
	bat := filepath.Join(config.JobsDir(), "gece.bat")
	data, err := os.ReadFile(bat)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Batch dosyası kaydedildi: "+bat) ||
		!strings.Contains(string(data), `run --job "`+config.JobPath("gece")+`" --home "`+config.DataDir()+`"`) ||
		!strings.Contains(string(data), "\r\nexit /b %SQLBACKUP_RC%\r\n") {
		t.Errorf("çıktı=%s\nbat=%s", out, data)
	}

	// Farklı konum + geçersiz ayarlı iş (uyarı verilir, dosya yine yazılır).
	saveJob(t, "bozuk", func(j *config.Job) { j.Backup.ServerPath = "" })
	other := filepath.Join(t.TempDir(), "Toplu İş", "yedek.bat")
	if code := e.main([]string{"batch", "--job", "bozuk", "--path", other}); code != 0 {
		t.Fatalf("kod=%d %s", code, errb)
	}
	if _, err := os.Stat(other); err != nil || !strings.Contains(errb.String(), "UYARI: işin ayarlarında hata var") {
		t.Errorf("%v %s", err, errb)
	}
}

func TestTaskCreate(t *testing.T) {
	e, f, out, errb := taskEnv(t)
	saveJob(t, "gece", func(j *config.Job) { j.Schedule.RunAs = config.RunAsCurrent })
	next := time.Date(2026, 10, 24, 2, 0, 0, 0, time.Local)
	f.status = scheduler.TaskStatus{Exists: true, NextRun: &next}
	if code := e.main([]string{"task", "create", "--job", "gece"}); code != 0 {
		t.Fatalf("kod=%d %s", code, errb)
	}
	x := f.created["gece"]
	if f.runAs != config.RunAsCurrent || !strings.Contains(x, `<UserId>PC01\Fatih TÜRKOĞLU</UserId>`) ||
		!strings.Contains(x, `<StartBoundary>2026-10-23T02:00:00</StartBoundary>`) {
		t.Errorf("runAs=%s xml=%s", f.runAs, x)
	}
	for _, want := range []string{`Zamanlanmış görev oluşturuldu: \SqlBackup\gece`, "Sonraki çalışma: 2026-10-24 02:00"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("çıktıda yok: %s\n%s", want, out)
		}
	}
	if !strings.Contains(errb.String(), `PC01\Fatih TÜRKOĞLU oturum açmışken`) {
		t.Errorf("uyarı: %s", errb)
	}
}

func TestTaskCreateInvalidJob(t *testing.T) {
	e, f, _, errb := taskEnv(t)
	saveJob(t, "gece", func(j *config.Job) { j.Schedule.Time = "2:00" })
	if code := e.main([]string{"task", "create", "--job", "gece"}); code != 2 || f.created != nil ||
		!strings.Contains(errb.String(), "SS:DD") {
		t.Errorf("kod=%d %s", code, errb)
	}
}

func TestTaskAccessDenied(t *testing.T) {
	e, f, _, errb := taskEnv(t)
	saveJob(t, "gece", nil)
	f.err = scheduler.ErrAccessDenied
	if code := e.main([]string{"task", "create", "--job", "gece"}); code != 2 ||
		!strings.Contains(errb.String(), "yönetici yetkisi gerekiyor") || !strings.Contains(errb.String(), "--elevate") {
		t.Errorf("kod=%d %s", code, errb)
	}
}

func TestTaskElevate(t *testing.T) {
	e, f, out, errb := taskEnv(t)
	saveJob(t, "gece", nil)
	f.err = scheduler.ErrAccessDenied
	var gotArgs []string
	e.runAsAdmin = func(exe string, args []string) (int, error) {
		gotArgs = args
		i := slices.Index(args, "--output")
		if i < 0 {
			t.Fatal("--output yok")
		}
		os.WriteFile(args[i+1], []byte("yükseltilmiş çıktı\n"), 0o644)
		return 0, nil
	}
	jobFile := filepath.Join(config.JobsDir(), "gece.json")
	if code := e.main([]string{"task", "create", "--job", jobFile, "--elevate"}); code != 0 {
		t.Fatalf("kod=%d %s", code, errb)
	}
	want := []string{"task", "create", "--job", jobFile, "--lang", "tr", "--home", config.DataDir()}
	for _, w := range want {
		if !slices.Contains(gotArgs, w) {
			t.Errorf("argüman yok: %s (%q)", w, gotArgs)
		}
	}
	if slices.Contains(gotArgs, "--elevate") {
		t.Errorf("--elevate tekrar verilmemeli: %q", gotArgs)
	}
	if !strings.Contains(out.String(), "yükseltilmiş çıktı") {
		t.Errorf("çıktı aktarılmadı: %s", out)
	}

	// Zaten yükseltilmişse tekrar denenmez.
	e.isElevated = func() bool { return true }
	gotArgs = nil
	if code := e.main([]string{"task", "delete", "--job", "gece", "--elevate"}); code != 2 || gotArgs != nil {
		t.Errorf("kod=%d args=%q", code, gotArgs)
	}
}

func TestTaskStatus(t *testing.T) {
	e, f, out, errb := taskEnv(t)
	// İş dosyası olmadan da (düz ad) görev sorgulanabilir.
	if code := e.main([]string{"task", "status", "--job", "gece"}); code != 1 ||
		!strings.Contains(out.String(), `Zamanlanmış görev bulunamadı: \SqlBackup\gece`) {
		t.Errorf("kod=%d %s %s", code, out, errb)
	}

	last := time.Date(2026, 10, 23, 2, 0, 5, 0, time.Local)
	f.status = scheduler.TaskStatus{Exists: true, Enabled: true, State: scheduler.StateReady, LastRun: &last,
		LastResult: 1, Definition: scheduler.TaskDefinition{RunAs: config.RunAsSystem, User: "S-1-5-18",
			Command: `C:\Eski\SqlBackup.exe`, Arguments: `run --job "x.json"`}}
	out.Reset()
	if code := e.main([]string{"task", "status", "--job", "gece"}); code != 0 {
		t.Fatalf("kod=%d %s", code, errb)
	}
	for _, want := range []string{`\SqlBackup\gece`, "hazır", "SYSTEM", "2026-10-23 02:00:05", "1 (kısmi hata)", "Sonraki çalışma:  hiç",
		`"C:\Eski\SqlBackup.exe" run --job "x.json"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("çıktıda yok: %q\n%s", want, out)
		}
	}
	if !strings.Contains(errb.String(), `farklı bir program dosyasını çalıştırıyor: C:\Eski\SqlBackup.exe`) {
		t.Errorf("exe uyarısı: %s", errb)
	}

	out.Reset()
	if code := e.main([]string{"task", "status", "--job", "gece", "--json"}); code != 0 {
		t.Fatalf("kod=%d", code)
	}
	var st scheduler.TaskStatus
	if err := json.Unmarshal([]byte(out.String()), &st); err != nil || !st.Exists || st.LastResult != 1 {
		t.Errorf("%v %+v", err, st)
	}
}

func TestTaskDeleteAndRun(t *testing.T) {
	e, f, out, errb := taskEnv(t)
	if code := e.main([]string{"task", "delete", "--job", "gece"}); code != 0 || !slices.Equal(f.deleted, []string{"gece"}) ||
		!strings.Contains(out.String(), `Zamanlanmış görev silindi: \SqlBackup\gece`) {
		t.Errorf("kod=%d %s %s", code, out, errb)
	}
	if code := e.main([]string{"task", "run", "--job", "gece"}); code != 0 || !slices.Equal(f.started, []string{"gece"}) {
		t.Errorf("kod=%d %s", code, errb)
	}
	f.err = scheduler.ErrTaskNotFound
	if code := e.main([]string{"task", "run", "--job", "gece"}); code != 2 ||
		!strings.Contains(errb.String(), `Zamanlanmış görev bulunamadı: \SqlBackup\gece`) {
		t.Errorf("kod=%d %s", code, errb)
	}
}

func TestTaskUsageErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"task"}, "görev komutu belirtilmedi"},
		{[]string{"task", "--job", "gece"}, "görev komutu belirtilmedi"},
		{[]string{"task", "sil", "--job", "gece"}, "bilinmeyen görev komutu: sil"},
		{[]string{"task", "status"}, "--job belirtilmedi"},
		{[]string{"task", "status", "--job", "a/b"}, "iş dosyası bulunamadı"},
		{[]string{"task", "status", "--job", "..."}, "yalnızca noktadan"},
	}
	for _, tt := range tests {
		e, _, _, errb := taskEnv(t)
		if code := e.main(tt.args); code != 2 || !strings.Contains(errb.String(), tt.want) {
			t.Errorf("%v: kod=%d %s", tt.args, code, errb)
		}
	}
}

func TestHomeFlag(t *testing.T) {
	e, _, _, _ := taskEnv(t)
	dir := filepath.Join(t.TempDir(), "Veri Ş")
	t.Setenv(config.HomeEnv, "")                 // test sonunda geri yüklenir
	t.Cleanup(func() { i18n.SetLanguage("tr") }) // setup dili ayarlardan seçer
	rest := e.setup([]string{"task", "status", "--home", dir, "--job", "gece"})
	if config.DataDir() != dir || strings.Join(rest, " ") != "task status --job gece" {
		t.Errorf("veri klasörü=%s rest=%v", config.DataDir(), rest)
	}
}
