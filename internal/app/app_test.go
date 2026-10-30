package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/mssql"
	"sqlbackup/internal/scheduler"
)

type fakeServer struct {
	block chan struct{} // doluysa yedek bu kanal kapanana (veya iptale) kadar bekler
}

func (f *fakeServer) Info() mssql.ServerInfo {
	return mssql.ServerInfo{ServerName: `PC01\SQLEXPRESS`, Edition: "Express Edition (64-bit)", Protocol: "lpc", EngineEdition: 4}
}
func (f *fakeServer) ListDatabases(context.Context) ([]mssql.Database, error) {
	return []mssql.Database{
		{Name: "master", State: "ONLINE", IsSystem: true, CanBackup: true},
		{Name: "Satis", State: "ONLINE", CanBackup: true},
	}, nil
}
func (f *fakeServer) BackupDatabase(ctx context.Context, db, file string, _ mssql.BackupOptions, p mssql.ProgressFunc) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if p != nil {
		p(50)
		p(100)
	}
	return nil
}
func (f *fakeServer) VerifyBackup(context.Context, string, bool, mssql.ProgressFunc) error {
	return nil
}
func (f *fakeServer) LastBackupSet(context.Context, string, string) (*mssql.BackupSetInfo, error) {
	return &mssql.BackupSetInfo{SizeBytes: 1 << 20}, nil
}
func (f *fakeServer) Close() error { return nil }

type fakeTasks struct {
	mu      sync.Mutex
	tasks   map[string]scheduler.TaskStatus
	xml     map[string]string
	err     error // Create/Delete/Run hatası
	calls   []string
	queries int
}

func (f *fakeTasks) Create(name, x, runAs string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "create "+name)
	if f.err != nil {
		return f.err
	}
	f.xml[key(name)] = x
	f.tasks[key(name)] = scheduler.TaskStatus{Exists: true, Path: scheduler.TaskPath(name)}
	return nil
}
func (f *fakeTasks) Delete(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "delete "+name)
	if f.err != nil {
		return f.err
	}
	if _, ok := f.tasks[key(name)]; !ok {
		return scheduler.ErrTaskNotFound
	}
	delete(f.tasks, key(name))
	return nil
}
func (f *fakeTasks) Run(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "run "+name)
	if f.err != nil {
		return f.err
	}
	if _, ok := f.tasks[key(name)]; !ok {
		return scheduler.ErrTaskNotFound
	}
	return nil
}

// key: görev adları Görev Zamanlayıcı'da harf duyarsızdır.
func key(name string) string { return strings.ToLower(name) }

// Query, gerçek görevdeki gibi tanımı kaydedilen XML'den okur.
func (f *fakeTasks) Query(name string) (scheduler.TaskStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries++
	st, ok := f.tasks[key(name)]
	if !ok {
		return scheduler.TaskStatus{Path: scheduler.TaskPath(name)}, nil
	}
	if x, ok := f.xml[key(name)]; ok {
		def, err := scheduler.ParseTaskXML(x)
		if err != nil {
			return st, err
		}
		st.Definition = def
	}
	return st, nil
}

type event struct {
	name string
	data any
}

type testApp struct {
	*App
	srv    *fakeServer
	tasks  *fakeTasks
	mu     sync.Mutex
	events []event
	// passwords, bağlanırken kullanılan şifrelerdir.
	passwords []string
	elevated  [][]string
}

var fixed = time.Date(2026, 10, 23, 2, 0, 0, 0, time.Local)

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	t.Setenv(config.HomeEnv, filepath.Join(t.TempDir(), "Veri Ş"))
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ta := &testApp{App: New("1.0.0-test"), srv: &fakeServer{},
		tasks: &fakeTasks{tasks: map[string]scheduler.TaskStatus{}, xml: map[string]string{}}}
	ta.App.tasks = ta.tasks
	ta.App.user = `PC01\Fatih TÜRKOĞLU`
	ta.App.now = func() time.Time { return fixed }
	ta.App.emit = func(name string, data any) {
		ta.mu.Lock()
		ta.events = append(ta.events, event{name, data})
		ta.mu.Unlock()
	}
	ta.App.connect = func(_ context.Context, _ config.Connection, pw string) (backup.Server, error) {
		ta.mu.Lock()
		ta.passwords = append(ta.passwords, pw)
		ta.mu.Unlock()
		return ta.srv, nil
	}
	ta.App.elevate = func(args []string) (int, string, error) {
		ta.elevated = append(ta.elevated, args)
		return 0, "", nil
	}
	t.Cleanup(func() { ta.wg.Wait() })
	return ta
}

// validJob, kaydedilebilir bir iştir (SQL kimlik doğrulaması).
func validJob(name string) config.Job {
	j := config.NewJob(name)
	j.Connection.Server = `.\SQLEXPRESS`
	j.Connection.User = "yedekci"
	j.Backup.ServerPath = `D:\SqlBackups`
	j.Databases = []string{"Satis"}
	return *j
}

func (ta *testApp) save(t *testing.T, req SaveJobRequest) SaveJobResult {
	t.Helper()
	res, err := ta.SaveJob(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSaveAndGetJobPassword(t *testing.T) {
	ta := newTestApp(t)
	res := ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "Gizli;Şifre=1", PasswordChanged: true})
	if !res.Saved || len(res.Errors) != 0 {
		t.Fatalf("%+v", res)
	}
	raw, _ := os.ReadFile(config.JobPath("gece"))
	if strings.Contains(string(raw), "Gizli") {
		t.Fatal("şifre düz metin yazılmış")
	}

	d, err := ta.GetJob("gece")
	if err != nil {
		t.Fatal(err)
	}
	if !d.HasPassword || d.PasswordError != "" || d.Job.Connection.PasswordEnc != "" {
		t.Fatalf("şifre frontend'e gitmemeli: %+v", d)
	}

	// Şifre değiştirilmeden kaydet → kayıtlı şifre korunur.
	j := *d.Job
	j.Databases = []string{"Satis", "master"}
	if res := ta.save(t, SaveJobRequest{Job: j, OriginalName: "gece"}); !res.Saved {
		t.Fatalf("%+v", res)
	}
	saved, _ := config.LoadJob(config.JobPath("gece"))
	if pw, err := saved.Connection.Password(); err != nil || pw != "Gizli;Şifre=1" || len(saved.Databases) != 2 {
		t.Errorf("şifre=%q %v %v", pw, err, saved.Databases)
	}

	// Bağlantı testi kayıtlı şifreyi (değişmediyse) veya yenisini kullanır.
	req := ConnectionRequest{Connection: j.Connection, StoredJob: "gece"}
	info, err := ta.TestConnection(req)
	if err != nil || info.Server.ServerName != `PC01\SQLEXPRESS` || info.SupportsCompression || info.ProtocolLabel == "" {
		t.Fatalf("%+v %v", info, err)
	}
	req.Password, req.PasswordChanged = "yeni", true
	if dbs, err := ta.ListDatabases(req); err != nil || len(dbs) != 2 {
		t.Fatalf("%v %v", dbs, err)
	}
	if !slices.Equal(ta.passwords, []string{"Gizli;Şifre=1", "yeni"}) {
		t.Errorf("şifreler: %q", ta.passwords)
	}

	// Windows kimlik doğrulamasına geçince şifre silinir.
	j.Connection.Auth = config.AuthWindows
	ta.save(t, SaveJobRequest{Job: j, OriginalName: "gece"})
	if d, _ := ta.GetJob("gece"); d.HasPassword {
		t.Error("Windows kimliğinde şifre saklanmamalı")
	}
}

func TestPasswordNotDecryptable(t *testing.T) {
	ta := newTestApp(t)
	j := validJob("kopya")
	j.Connection.PasswordEnc = "AAAA" // başka makineden gelmiş gibi
	if err := config.SaveJob(config.JobPath("kopya"), &j); err != nil {
		t.Fatal(err)
	}
	d, err := ta.GetJob("kopya")
	if err != nil || !d.HasPassword || !strings.Contains(d.PasswordError, "şifre çözülemedi") {
		t.Errorf("%+v %v", d, err)
	}
}

func TestSaveJobValidation(t *testing.T) {
	ta := newTestApp(t)
	j := validJob("gece")
	j.Backup.ServerPath = ""
	j.Schedule.Time = "25:00"
	res := ta.save(t, SaveJobRequest{Job: j})
	if res.Saved || len(res.Errors) != 3 { // şifre yok + klasör + saat
		t.Fatalf("%+v", res)
	}
	if len(res.FieldErrors) != 3 || len(res.FieldErrors["connection.password"]) != 1 ||
		len(res.FieldErrors["backup.serverPath"]) != 1 || len(res.FieldErrors["schedule.time"]) != 1 {
		t.Errorf("alan hataları: %v", res.FieldErrors)
	}
	if _, err := os.Stat(config.JobPath("gece")); !errors.Is(err, os.ErrNotExist) {
		t.Error("geçersiz iş kaydedilmemeli")
	}

	ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	// Aynı adla (büyük/küçük harf farklı) yeni iş → çakışma.
	res = ta.save(t, SaveJobRequest{Job: validJob("GECE"), Password: "x", PasswordChanged: true})
	if res.Saved || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], `"GECE" adında bir iş zaten var`) {
		t.Errorf("%+v", res)
	}
	if len(res.FieldErrors["name"]) != 1 {
		t.Errorf("çakışma ada bağlanmalı: %v", res.FieldErrors)
	}
	// Geçersiz ad.
	if res := ta.save(t, SaveJobRequest{Job: validJob(`..\kötü`)}); res.Saved {
		t.Errorf("%+v", res)
	}
	// Olmayan işi düzenleme.
	if _, err := ta.SaveJob(SaveJobRequest{Job: validJob("x"), OriginalName: "yok"}); err == nil ||
		!strings.Contains(err.Error(), "iş bulunamadı: yok") {
		t.Errorf("%v", err)
	}
}

func TestValidateJob(t *testing.T) {
	ta := newTestApp(t)
	// Yeni şifre şifrelenmeden "var" sayılır; iş kaydedilmez.
	res, err := ta.ValidateJob(SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	if err != nil || res.Saved || len(res.Errors) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(config.JobPath("gece")); !errors.Is(err, os.ErrNotExist) {
		t.Error("ValidateJob kaydetmemeli")
	}
	j := validJob("gece")
	j.Backup.FileNameTemplate = "{db}_{bilinmeyen}"
	res, _ = ta.ValidateJob(SaveJobRequest{Job: j})
	if len(res.FieldErrors["connection.password"]) != 1 || len(res.FieldErrors["backup.fileNameTemplate"]) != 1 {
		t.Errorf("%v", res.FieldErrors)
	}
	// Kayıtlı işin şifresi korunur.
	ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	if res, _ := ta.ValidateJob(SaveJobRequest{Job: validJob("gece"), OriginalName: "gece"}); len(res.Errors) != 0 {
		t.Errorf("%+v", res)
	}
}

func TestCloseWarning(t *testing.T) {
	ta := newTestApp(t)
	var asked []string
	answer := false
	ta.confirm = func(title, msg string) bool {
		asked = append(asked, msg)
		return answer
	}
	beforeClose := BeforeClose(ta.App)
	if beforeClose(context.Background()) || len(asked) != 0 {
		t.Fatal("uyarı yokken sormadan kapanmalı")
	}
	ta.SetUnsavedChanges(true)
	ta.mu.Lock()
	ta.running["b"] = runningJob{name: "B_İş", cancel: func() {}}
	ta.running["a"] = runningJob{name: "A_İş", cancel: func() {}}
	ta.mu.Unlock()
	if !beforeClose(context.Background()) {
		t.Error("Hayır seçilince pencere kapanmamalı")
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "kaydedilmemiş") || !strings.Contains(asked[0], "A_İş, B_İş") {
		t.Errorf("%q", asked)
	}
	answer = true
	if beforeClose(context.Background()) {
		t.Error("Evet seçilince kapanmalı")
	}
	ta.mu.Lock()
	clear(ta.running)
	ta.mu.Unlock()
	ta.SetUnsavedChanges(false)
	if beforeClose(context.Background()) || len(asked) != 2 {
		t.Error("uyarı kalmayınca sormamalı")
	}
}

func TestRenameJob(t *testing.T) {
	ta := newTestApp(t)
	ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	if _, err := ta.CreateTask("gece"); err != nil {
		t.Fatal(err)
	}
	if _, err := ta.SaveBatch("gece", ""); err != nil {
		t.Fatal(err)
	}

	// Harf değişikliği.
	j := validJob("Gece")
	if res := ta.save(t, SaveJobRequest{Job: j, OriginalName: "gece"}); !res.Saved {
		t.Fatalf("%+v", res)
	}
	if res := ta.save(t, SaveJobRequest{Job: j, OriginalName: "Gece"}); len(res.Warnings) != 0 {
		t.Errorf("harf değişikliğinde uyarı olmamalı: %+v", res)
	}
	if list, _ := ta.ListJobs(); len(list.Jobs) != 1 || list.Jobs[0].Name != "Gece" {
		t.Fatalf("%+v", list)
	}
	if ti, _ := ta.GetTaskStatus("Gece"); ti.Outdated {
		t.Error("görev hâlâ işle eşleşmeli")
	}

	// Yeni ad: eski dosya silinir, görev ve .bat için uyarı verilir, şifre korunur.
	j.Name = "gunduz"
	res := ta.save(t, SaveJobRequest{Job: j, OriginalName: "Gece"})
	if !res.Saved || len(res.Warnings) != 2 || !strings.Contains(res.Warnings[0], `\SqlBackup\gece`) ||
		!strings.Contains(strings.ToLower(res.Warnings[1]), "gece.bat") {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(config.JobPath("Gece")); !errors.Is(err, os.ErrNotExist) {
		t.Error("eski iş dosyası silinmeli")
	}
	if d, _ := ta.GetJob("gunduz"); !d.HasPassword {
		t.Error("şifre taşınmalı")
	}
}

func TestDuplicateAndDeleteJob(t *testing.T) {
	ta := newTestApp(t)
	ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	if res, err := ta.DuplicateJob("gece", "gece_kopya"); err != nil || !res.Saved {
		t.Fatalf("%+v %v", res, err)
	}
	if res, _ := ta.DuplicateJob("gece", "GECE_KOPYA"); res.Saved || len(res.Errors) != 1 {
		t.Errorf("çakışma: %+v", res)
	}
	if res, _ := ta.DuplicateJob("gece", "a/b"); res.Saved {
		t.Errorf("geçersiz ad: %+v", res)
	}
	if d, _ := ta.GetJob("gece_kopya"); !d.HasPassword {
		t.Error("kopyada şifre olmalı")
	}

	ta.CreateTask("gece")
	ta.SaveBatch("gece", "")
	if err := ta.DeleteJob("gece"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{config.JobPath("gece"), scheduler.DefaultBatchPath("gece")} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("silinmedi: %s", p)
		}
	}
	if st, _ := ta.tasks.Query("gece"); st.Exists {
		t.Error("görev silinmeli")
	}
	// Görev silinemezse iş de silinmez.
	ta.CreateTask("gece_kopya")
	ta.tasks.err = errors.New("COM hatası")
	if err := ta.DeleteJob("gece_kopya"); err == nil {
		t.Fatal("hata beklenirdi")
	}
	if _, err := os.Stat(config.JobPath("gece_kopya")); err != nil {
		t.Error("iş silinmemeliydi")
	}
	if err := ta.DeleteJob("yok"); err == nil {
		t.Error("olmayan iş")
	}
}

func TestTaskOutdatedAndElevation(t *testing.T) {
	ta := newTestApp(t)
	j := validJob("gece")
	ta.save(t, SaveJobRequest{Job: j, Password: "x", PasswordChanged: true})
	if ti, err := ta.GetTaskStatus("gece"); err != nil || ti.Status.Exists || ti.Outdated {
		t.Fatalf("%+v %v", ti, err)
	}
	r, err := ta.CreateTask("gece")
	if err != nil || r.Elevated || !r.Task.Status.Exists || r.Task.Outdated {
		t.Fatalf("%+v %v", r, err)
	}
	if !strings.Contains(ta.tasks.xml[key("gece")], "<UserId>S-1-5-18</UserId>") {
		t.Errorf("SYSTEM görevi bekleniyordu")
	}

	// Saat değişti, görev güncellenmedi → uyarı.
	j.Schedule.Time = "04:00"
	res := ta.save(t, SaveJobRequest{Job: j, OriginalName: "gece"})
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "uyuşmuyor") {
		t.Errorf("%+v", res)
	}
	if list, _ := ta.ListJobs(); list.Jobs[0].Task == nil || !list.Jobs[0].Task.Outdated {
		t.Errorf("listede güncel değil görünmeli: %+v", list.Jobs[0].Task)
	}

	// Erişim reddi → CLI ile yükseltme.
	ta.tasks.err = scheduler.ErrAccessDenied
	r, err = ta.CreateTask("gece")
	if scheduler.IsElevated() {
		t.Skip("süreç yönetici yetkisiyle çalışıyor")
	}
	if err != nil || !r.Elevated || !slices.Equal(ta.elevated[0], []string{"task", "create", "--job", "gece"}) {
		t.Fatalf("%+v %v %q", r, err, ta.elevated)
	}
	// Yükseltilmiş süreç başarısızsa onun çıktısı hata olur.
	ta.App.elevate = func([]string) (int, string, error) { return 2, "HATA: bir sorun\n", nil }
	if _, err := ta.RunTask("gece"); err == nil || err.Error() != "HATA: bir sorun" {
		t.Errorf("%v", err)
	}
	ta.App.elevate = func([]string) (int, string, error) { return 0, "", scheduler.ErrElevationCancelled }
	if _, err := ta.DeleteTask("gece"); !errors.Is(err, scheduler.ErrElevationCancelled) {
		t.Errorf("%v", err)
	}

	ta.tasks.err = nil
	if _, err := ta.RunTask("yok"); err == nil {
		t.Error("olmayan iş")
	}
	ta.save(t, SaveJobRequest{Job: validJob("ikinci"), Password: "x", PasswordChanged: true})
	if _, err := ta.RunTask("ikinci"); err == nil || !strings.Contains(err.Error(), `görev bulunamadı: \SqlBackup\ikinci`) {
		t.Errorf("%v", err)
	}
}

func TestTaskWarnings(t *testing.T) {
	ta := newTestApp(t)
	j := validJob("gece")
	j.Connection.Auth = config.AuthWindows
	if w := ta.TaskWarnings(j); len(w) != 1 || !strings.Contains(w[0], `NT AUTHORITY\SYSTEM`) {
		t.Errorf("%q", w)
	}
	j.Schedule.RunAs = config.RunAsCurrent
	if w := ta.TaskWarnings(j); len(w) != 1 || !strings.Contains(w[0], `PC01\Fatih TÜRKOĞLU`) {
		t.Errorf("%q", w)
	}
}

func (ta *testApp) waitDone(t *testing.T) DoneEvent {
	t.Helper()
	ta.wg.Wait()
	ta.mu.Lock()
	defer ta.mu.Unlock()
	for i := len(ta.events) - 1; i >= 0; i-- {
		if d, ok := ta.events[i].data.(DoneEvent); ok {
			return d
		}
	}
	t.Fatal("bitti olayı yok")
	return DoneEvent{}
}

func TestRunJobNow(t *testing.T) {
	ta := newTestApp(t)
	ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	ta.srv.block = make(chan struct{})
	if err := ta.RunJobNow("gece"); err != nil {
		t.Fatal(err)
	}
	if !ta.IsJobRunning("gece") || !slices.Equal(ta.RunningJobs(), []string{"gece"}) {
		t.Error("çalışıyor görünmeli")
	}
	if list, _ := ta.ListJobs(); !list.Jobs[0].Running {
		t.Error("listede çalışıyor görünmeli")
	}
	if err := ta.RunJobNow("GECE"); err == nil || !strings.Contains(err.Error(), "zaten çalışıyor") {
		t.Errorf("ikinci çalışma: %v", err)
	}
	if err := ta.DeleteJob("gece"); err == nil {
		t.Error("çalışan iş silinmemeli")
	}
	close(ta.srv.block)
	d := ta.waitDone(t)
	if d.Job != "gece" || d.Result.ExitCode != backup.ExitOK || len(d.Result.Databases) != 1 {
		t.Fatalf("%+v", d.Result)
	}
	if ta.IsJobRunning("gece") || len(ta.RunningJobs()) != 0 {
		t.Error("bitmiş görünmeli")
	}
	var progress, started int
	for _, e := range ta.events {
		switch e.name {
		case EventProgress:
			progress++
			if p := e.data.(ProgressEvent); p.Job != "gece" {
				t.Errorf("%+v", p)
			}
		case EventStarted:
			started++
		}
	}
	if started != 1 || progress == 0 {
		t.Errorf("olaylar: başladı=%d ilerleme=%d", started, progress)
	}

	// Log ve çalışma kaydı (GUI kaynağıyla).
	runs, err := ta.ListRuns("", 0)
	if err != nil || len(runs) != 1 || runs[0].ExitCode != 0 || runs[0].Succeeded != 1 {
		t.Fatalf("%+v %v", runs, err)
	}
	if r, err := ta.ReadRun(runs[0].Path); err != nil || r.Job != "gece" {
		t.Errorf("%+v %v", r, err)
	}
	logs, _ := ta.ListLogs("gece")
	if len(logs) != 1 {
		t.Fatalf("%+v", logs)
	}
	lt, err := ta.ReadLog(logs[0].Path)
	if err != nil || !strings.Contains(lt.Text, "arayüzden çalıştırıldı") || lt.Truncated {
		t.Errorf("%+v %v", lt, err)
	}
	if list, _ := ta.ListJobs(); list.Jobs[0].LastRun == nil || list.Jobs[0].LastRun.ExitCode != 0 {
		t.Errorf("son çalışma: %+v", list.Jobs[0].LastRun)
	}
}

func TestCancelJob(t *testing.T) {
	ta := newTestApp(t)
	ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	ta.srv.block = make(chan struct{})
	if ta.CancelJob("gece") {
		t.Error("çalışmayan iş iptal edilemez")
	}
	if err := ta.RunJobNow("gece"); err != nil {
		t.Fatal(err)
	}
	if !ta.CancelJob("Gece") {
		t.Fatal("iptal edilmeli")
	}
	if d := ta.waitDone(t); d.Result.ExitCode != backup.ExitFailed {
		t.Errorf("%+v", d.Result)
	}
}

func TestShutdownCancelsRuns(t *testing.T) {
	ta := newTestApp(t)
	ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	ta.srv.block = make(chan struct{})
	if err := ta.RunJobNow("gece"); err != nil {
		t.Fatal(err)
	}
	Shutdown(ta.App)(context.Background())
	if runs, _ := ta.ListRuns("gece", 0); len(runs) != 1 {
		t.Errorf("kapanışta çalışma kaydı yazılmalı: %+v", runs)
	}
}

func TestReadPathsRestricted(t *testing.T) {
	ta := newTestApp(t)
	outside := filepath.Join(t.TempDir(), "gizli.log")
	os.WriteFile(outside, []byte("x"), 0o644)
	for _, p := range []string{outside, filepath.Join(config.LogsDir(), "..", "jobs", "gece.json"), config.LogsDir()} {
		if _, err := ta.ReadLog(p); err == nil || !strings.Contains(err.Error(), "log klasöründe değil") {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := ta.ReadRun(filepath.Join(config.LogsDir(), "gece_20261023.log")); err == nil {
		t.Error("runs dışındaki dosya okunmamalı")
	}
}

func TestReadLogTruncated(t *testing.T) {
	ta := newTestApp(t)
	p := filepath.Join(config.LogsDir(), "gece_20261023.log")
	var b strings.Builder
	for i := 0; b.Len() < maxLogBytes+1000; i++ {
		fmt.Fprintf(&b, "satır %d\n", i)
	}
	os.WriteFile(p, []byte(b.String()), 0o644)
	lt, err := ta.ReadLog(p)
	if err != nil || !lt.Truncated || len(lt.Text) > maxLogBytes || !strings.HasPrefix(lt.Text, "satır ") ||
		!strings.HasSuffix(lt.Text, "\n") {
		t.Errorf("truncated=%v len=%d başı=%q %v", lt.Truncated, len(lt.Text), lt.Text[:20], err)
	}
}

func TestListJobsWithBrokenFile(t *testing.T) {
	ta := newTestApp(t)
	ta.save(t, SaveJobRequest{Job: validJob("gece"), Password: "x", PasswordChanged: true})
	bad := validJob("eksik") // şifresiz: geçersiz ama listelenir
	config.SaveJob(config.JobPath("eksik"), &bad)
	os.WriteFile(filepath.Join(config.JobsDir(), "bozuk.json"), []byte("{"), 0o644)
	list, err := ta.ListJobs()
	if err != nil || len(list.Jobs) != 2 || len(list.Errors) != 1 || !strings.Contains(list.Errors[0], "bozuk.json") {
		t.Fatalf("%+v %v", list, err)
	}
	if list.Jobs[0].Name != "eksik" || len(list.Jobs[0].Errors) != 1 || len(list.Jobs[1].Errors) != 0 {
		t.Errorf("%+v", list.Jobs)
	}
}

func TestPreviewFileName(t *testing.T) {
	ta := newTestApp(t)
	j := validJob("gece")
	j.Backup.FileNameTemplate = "{server}_{db}_{yyyy-MM-dd}"
	if n, err := ta.PreviewFileName(j, "Satis", ""); err != nil || n != "._SQLEXPRESS_Satis_2026-10-23.bak" {
		t.Errorf("%q %v", n, err)
	}
	if n, _ := ta.PreviewFileName(j, "Satis", `PC01\SQLEXPRESS`); n != "PC01_SQLEXPRESS_Satis_2026-10-23.bak" {
		t.Errorf("%q", n)
	}
	j.Backup.FileNameTemplate = "{tarih}_{db}"
	if _, err := ta.PreviewFileName(j, "Satis", ""); err == nil {
		t.Error("bilinmeyen yer tutucu")
	}
	j.Backup.FileNameTemplate = "yedek"
	if _, err := ta.PreviewFileName(j, "Satis", ""); err == nil {
		t.Error("{db} yok")
	}
}

func TestSettings(t *testing.T) {
	ta := newTestApp(t)
	if err := ta.SaveSettings(config.Settings{LogKeepDays: -1}); err == nil {
		t.Error("negatif süre")
	}
	if err := ta.SaveSettings(config.Settings{Theme: "blue"}); err == nil {
		t.Error("bilinmeyen tema")
	}
	if err := ta.SaveSettings(config.Settings{LogKeepDays: 7, Theme: config.ThemeLight}); err != nil {
		t.Fatal(err)
	}
	// Dil verilmezse boş kalır: açılışta Windows dili kullanılmaya devam eder.
	if s, _ := ta.GetSettings(); s.LogKeepDays != 7 || s.Language != "" || s.EffectiveTheme() != config.ThemeLight {
		t.Errorf("%+v", s)
	}
	if err := ta.SaveSettings(config.Settings{DefaultServerPath: ` D:\Yedek `, DefaultAccessPath: `\\srv\Yedek`}); err != nil {
		t.Fatal(err)
	}
	if j := ta.NewJob(); j.Backup.ServerPath != `D:\Yedek` || j.Backup.AccessPath != `\\srv\Yedek` || j.Schedule.Time != "02:00" {
		t.Errorf("%+v", j.Backup)
	}
	if info := ta.GetAppInfo(); info.Version != "1.0.0-test" || info.DataDir != config.DataDir() || info.StartupError != "" {
		t.Errorf("%+v", info)
	}
}

func TestErrorList(t *testing.T) {
	err := errors.Join(errors.New("a"), errors.Join(errors.New("b"), errors.New("c\nd")))
	if got := errorList(err); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("%q", got)
	}
	if errorList(nil) != nil {
		t.Error("nil")
	}
}
