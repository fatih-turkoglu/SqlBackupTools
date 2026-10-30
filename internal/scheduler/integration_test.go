package scheduler

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ole "github.com/go-ole/go-ole"

	"sqlbackup/internal/config"
)

// Entegrasyon testleri gerçek Görev Zamanlayıcı'yı kullanır; yalnızca
// SQLBACKUP_TEST_TASKS=1 ile çalışır. Görevler geçici \SqlBackupTest_<nano>
// klasörüne geçerli kullanıcı adına (yönetici yetkisi gerekmeden) kaydedilir
// ve test sonunda klasörle birlikte silinir.
const taskTestEnv = "SQLBACKUP_TEST_TASKS"

func useTestFolder(t *testing.T) {
	t.Helper()
	if os.Getenv(taskTestEnv) == "" {
		t.Skipf("Görev Zamanlayıcı testleri için %s=1 tanımlayın", taskTestEnv)
	}
	old := TaskFolder
	TaskFolder = fmt.Sprintf(`\SqlBackupTest_%d`, time.Now().UnixNano())
	t.Cleanup(func() {
		removeTestFolder(t)
		if folderExists(t) {
			t.Errorf("test klasörü kaldı: %s", TaskFolder)
		}
		TaskFolder = old
	})
}

// removeTestFolder, test klasöründeki görevleri ve klasörü siler.
func removeTestFolder(t *testing.T) {
	withService(func(svc *ole.IDispatch) error {
		f, err := getFolder(svc, TaskFolder)
		if err != nil {
			return nil
		}
		defer f.Release()
		for _, name := range []string{"gece", "sistem"} {
			callDispatch(f, "DeleteTask", name, 0)
		}
		deleteFolderIfEmpty(svc, f)
		return nil
	})
}

func folderExists(t *testing.T) bool {
	exists := false
	withService(func(svc *ole.IDispatch) error {
		if f, err := getFolder(svc, TaskFolder); err == nil {
			exists = true
			f.Release()
		}
		return nil
	})
	return exists
}

func currentUser(t *testing.T) string {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return u.Username
}

func TestTaskLifecycle(t *testing.T) {
	useTestFolder(t)
	// Görevde %AD% kalıbı genişletileceği için tek % kullanılır (bkz. TaskXML).
	exe, jobFile := helperExe(t, t.TempDir(), "Program Ğüşİöç 100%")
	j := config.NewJob("gece")
	j.Schedule.Time, j.Schedule.RunAs = "23:59", config.RunAsCurrent
	x, err := TaskXML(j, TaskOptions{Command: Command{Exe: exe, JobFile: jobFile}, User: currentUser(t), Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateTask("gece", x, config.RunAsCurrent); err != nil {
		t.Fatal(err)
	}
	// Var olan görev güncellenebilmeli.
	if err := CreateTask("gece", x, config.RunAsCurrent); err != nil {
		t.Fatalf("güncelleme: %v", err)
	}

	st, err := QueryTask("gece")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Exists || !st.Enabled || st.State != StateReady || st.LastRun != nil || st.LastResult != ResultNotRun {
		t.Errorf("durum: %+v", st)
	}
	if st.NextRun == nil || st.NextRun.Hour() != 23 || st.NextRun.Minute() != 59 || st.NextRun.Before(time.Now()) {
		t.Errorf("sonraki çalışma: %v", st.NextRun)
	}
	// Görev Zamanlayıcı hesabı SID olarak saklar; adı geri çözülmeli.
	if st.Definition.RunAs != config.RunAsCurrent || !strings.EqualFold(st.Definition.Command, exe) ||
		!strings.EqualFold(st.Definition.User, currentUser(t)) {
		t.Errorf("tanım: %+v", st.Definition)
	}
	// Görev Zamanlayıcı'nın geri verdiği XML'den zamanlama okunabilmeli.
	if !st.Definition.Matches(j, Command{Exe: exe, JobFile: jobFile}) {
		t.Errorf("görev işle eşleşmeli: %+v", st.Definition)
	}
	if st.Path != TaskFolder+`\gece` {
		t.Errorf("yol: %s", st.Path)
	}

	// Görev Zamanlayıcı üzerinden çalıştır: argümanlar exe'ye ulaşmalı,
	// çıkış kodu LastTaskResult olarak okunmalı.
	if err := RunTask("gece"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err = QueryTask("gece")
		if err != nil {
			t.Fatal(err)
		}
		if st.LastRun != nil && st.State != StateRunning && st.LastResult != ResultRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("görev bitmedi: %+v", st)
		}
		time.Sleep(250 * time.Millisecond)
	}
	if st.LastResult != helperExitCode {
		t.Errorf("son sonuç: 0x%X", st.LastResult)
	}
	got, err := os.ReadFile(jobFile + ".args")
	if err != nil {
		t.Fatalf("yardımcı exe çalışmadı: %v", err)
	}
	if want := "run\n--job\n" + jobFile; string(got) != want {
		t.Errorf("argümanlar: %q, beklenen %q", got, want)
	}

	if err := DeleteTask("gece"); err != nil {
		t.Fatal(err)
	}
	if st, err := QueryTask("gece"); err != nil || st.Exists {
		t.Errorf("silinen görev: %+v %v", st, err)
	}
	if folderExists(t) {
		t.Error("boş klasör silinmeli")
	}
	if err := DeleteTask("gece"); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("olmayan görevi silme: %v", err)
	}
	if err := RunTask("gece"); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("olmayan görevi çalıştırma: %v", err)
	}
}

func TestSystemTaskNeedsAdmin(t *testing.T) {
	useTestFolder(t)
	if IsElevated() {
		t.Skip("süreç yönetici yetkisiyle çalışıyor")
	}
	j := config.NewJob("sistem")
	// Exe güvenlik denetiminden geçen bir exe ile yönetici yetkisi istenir.
	cmd := testCommand()
	cmd.Exe = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	x, err := TaskXML(j, TaskOptions{Command: cmd, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateTask("sistem", x, config.RunAsSystem); !errors.Is(err, ErrAccessDenied) {
		t.Errorf("SYSTEM görevi: %v", err)
	}

	// Kullanıcının değiştirebildiği exe yetki sorulmadan reddedilir.
	if cmd.Exe, err = os.Executable(); err != nil {
		t.Fatal(err)
	}
	if x, err = TaskXML(j, TaskOptions{Command: cmd, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := CreateTask("sistem", x, config.RunAsSystem); err == nil || errors.Is(err, ErrAccessDenied) {
		t.Errorf("güvensiz exe ile SYSTEM görevi: %v", err)
	}
}
