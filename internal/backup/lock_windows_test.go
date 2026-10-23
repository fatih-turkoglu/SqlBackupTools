package backup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"sqlbackup/internal/config"
)

// testJobName, gerçek işlerle çakışmayacak benzersiz bir iş adı döner.
func testJobName() string {
	return fmt.Sprintf("SqlBackupTest_%d", time.Now().UnixNano())
}

func TestJobLock(t *testing.T) {
	name := testJobName()
	if IsRunning(name) {
		t.Fatal("kilit alınmadan çalışıyor görünmemeli")
	}
	l, err := acquireJobLock(name)
	if err != nil {
		t.Fatal(err)
	}
	if !IsRunning(name) || !IsRunning(strings.ToUpper(name)) {
		t.Error("kilit alındıktan sonra çalışıyor görünmeli (büyük/küçük harf duyarsız)")
	}
	if _, err := acquireJobLock(strings.ToUpper(name)); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("ikinci kilit ErrAlreadyRunning vermeli: %v", err)
	}
	l.release()
	l.release() // ikinci çağrı zararsız olmalı
	if IsRunning(name) {
		t.Error("kilit bırakıldıktan sonra çalışıyor görünmemeli")
	}
	l2, err := acquireJobLock(name)
	if err != nil {
		t.Fatalf("bırakılan kilit yeniden alınabilmeli: %v", err)
	}
	l2.release()
}

// TestLockHelperProcess, süreçler arası testte kilidi tutan alt süreçtir.
func TestLockHelperProcess(t *testing.T) {
	name := os.Getenv("SQLBACKUP_LOCK_HELPER")
	if name == "" {
		t.Skip("yalnızca alt süreç olarak çalışır")
	}
	l, err := acquireJobLock(name)
	if err != nil {
		fmt.Println("hata:", err)
		os.Exit(1)
	}
	fmt.Println("kilitli")
	// stdin kapanana kadar bekle; süreç kilidi bırakmadan sonlandırılır.
	bufio.NewReader(os.Stdin).ReadString('\n')
	_ = l
	os.Exit(0)
}

func TestJobLockCrossProcess(t *testing.T) {
	name := testJobName()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(), "SQLBACKUP_LOCK_HELPER="+name)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(stdout).ReadString('\n')
	if strings.TrimSpace(line) != "kilitli" {
		cmd.Process.Kill()
		t.Fatalf("alt süreç kilidi alamadı: %q", line)
	}

	if !IsRunning(name) {
		t.Error("başka süreçteki kilit görünmeli")
	}
	j := testJob()
	j.Name = name
	r := runWith(t, j, newFake(), nil)
	if r.ExitCode != ExitBusy || !r.AlreadyRunning {
		t.Errorf("exit=%d alreadyRunning=%v", r.ExitCode, r.AlreadyRunning)
	}

	// Süreç kilidi bırakmadan öldürülür; işletim sistemi kilidi kaldırmalı.
	cmd.Process.Kill()
	cmd.Wait()
	stdin.Close()
	if IsRunning(name) {
		t.Error("sonlanan sürecin kilidi kalmamalı")
	}
}

func TestRunAlreadyRunning(t *testing.T) {
	j := testJob()
	j.Name = testJobName()
	l, err := acquireJobLock(j.Name)
	if err != nil {
		t.Fatal(err)
	}
	f := newFake()
	connected := false
	fixed := time.Date(2026, 10, 23, 2, 0, 0, 0, time.Local)
	r := Run(context.Background(), j, Options{
		Connect: func(context.Context, config.Connection, string) (Server, error) { connected = true; return f, nil },
		Now:     func() time.Time { return fixed },
	})
	t.Log("\n" + r.Summary())
	if r.ExitCode != ExitBusy || !r.AlreadyRunning || connected || len(f.backups) != 0 ||
		!strings.Contains(r.Summary(), "UYARI") {
		t.Fatalf("exit=%d alreadyRunning=%v connected=%v", r.ExitCode, r.AlreadyRunning, connected)
	}
	l.release()

	// Kilit serbestken çalışma normal ilerlemeli ve sonunda kilidi bırakmalı.
	if r := runWith(t, j, newFake(), nil); r.ExitCode != ExitOK {
		t.Fatalf("exit=%d", r.ExitCode)
	}
	if IsRunning(j.Name) {
		t.Error("çalışma bittikten sonra kilit kalmamalı")
	}
}
