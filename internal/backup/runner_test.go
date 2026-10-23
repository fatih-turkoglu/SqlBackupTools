package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"sqlbackup/internal/config"
	"sqlbackup/internal/mssql"
)

type fakeServer struct {
	info       mssql.ServerInfo
	dbs        []mssql.Database
	failBackup map[string]bool // veritabanı adı
	failVerify map[string]bool // dosya yolu
	onBackup   func(db string)

	backups []string // "db=file"
	opts    []mssql.BackupOptions
	verify  []string
	closed  bool
}

func (f *fakeServer) Info() mssql.ServerInfo { return f.info }
func (f *fakeServer) ListDatabases(context.Context) ([]mssql.Database, error) {
	return f.dbs, nil
}
func (f *fakeServer) BackupDatabase(ctx context.Context, db, file string, o mssql.BackupOptions, p mssql.ProgressFunc) error {
	if f.onBackup != nil {
		f.onBackup(db)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.backups = append(f.backups, db+"="+file)
	f.opts = append(f.opts, o)
	if f.failBackup[db] {
		return errors.New("[3201] yazılamadı")
	}
	if p != nil {
		p(50)
		p(100)
	}
	return nil
}
func (f *fakeServer) VerifyBackup(_ context.Context, file string, _ bool, _ mssql.ProgressFunc) error {
	f.verify = append(f.verify, file)
	if f.failVerify[file] {
		return errors.New("doğrulama hatası")
	}
	return nil
}
func (f *fakeServer) LastBackupSet(context.Context, string, string) (*mssql.BackupSetInfo, error) {
	return &mssql.BackupSetInfo{SizeBytes: 3 << 20, CompressedBytes: 1 << 20}, nil
}
func (f *fakeServer) Close() error { f.closed = true; return nil }

func newFake() *fakeServer {
	return &fakeServer{
		info: mssql.ServerInfo{ServerName: `PC01\SQLEXPRESS`, Edition: "Express Edition (64-bit)", Protocol: "lpc"},
		dbs:  serverDBs(),
	}
}

func testJob() *config.Job {
	j := config.NewJob("gece")
	j.Connection.Server = `.\SQLEXPRESS`
	j.Connection.Auth = config.AuthWindows
	j.Backup.ServerPath = `D:\SqlBackups\`
	j.Backup.Compression = true
	j.Backup.Verify = true
	j.Databases = []string{"Satis", "Yeni"}
	return j
}

func runWith(t *testing.T, j *config.Job, f *fakeServer, prog func(Progress)) *Result {
	t.Helper()
	fixed := time.Date(2026, 10, 23, 2, 0, 0, 0, time.Local)
	r := Run(context.Background(), j, Options{
		Connect:  func(context.Context, config.Connection, string) (Server, error) { return f, nil },
		Now:      func() time.Time { return fixed },
		Progress: prog,
	})
	t.Log("\n" + r.Summary())
	return r
}

func TestRunSuccess(t *testing.T) {
	f := newFake()
	var progs []Progress
	r := runWith(t, testJob(), f, func(p Progress) { progs = append(progs, p) })

	if r.ExitCode != ExitOK || r.Error != "" {
		t.Fatalf("başarı bekleniyordu: exit=%d err=%s", r.ExitCode, r.Error)
	}
	want := []string{
		`Satis=D:\SqlBackups\Satis_FULL_20261023_020000.bak`,
		`Yeni=D:\SqlBackups\Yeni_FULL_20261023_020000.bak`,
	}
	if strings.Join(f.backups, ",") != strings.Join(want, ",") {
		t.Errorf("yedekler: %v", f.backups)
	}
	if len(f.verify) != 2 || !f.closed {
		t.Errorf("doğrulama/kapama: verify=%v closed=%v", f.verify, f.closed)
	}
	// Express: sıkıştırma kapatılıp uyarı verilmeli.
	if f.opts[0].Compression || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "sıkıştırma") {
		t.Errorf("sıkıştırma düşürülmeli: opts=%+v warnings=%v", f.opts[0], r.Warnings)
	}
	d := r.Databases[0]
	if d.Status != StatusSuccess || !d.Verified || d.FileBytes != 1<<20 || d.SizeBytes != 3<<20 || d.Compressed {
		t.Errorf("DB sonucu hatalı: %+v", d)
	}
	if len(progs) != 4 || progs[2] != (Progress{Database: "Yeni", Index: 2, Total: 2, Phase: PhaseBackup, Percent: 50}) {
		t.Errorf("ilerleme: %+v", progs)
	}
}

func TestRunCompressionSupported(t *testing.T) {
	f := newFake()
	f.info.Edition = "Standard Edition (64-bit)"
	r := runWith(t, testJob(), f, nil)
	if !f.opts[0].Compression || len(r.Warnings) != 0 || !r.Databases[0].Compressed {
		t.Errorf("sıkıştırma açık kalmalı: %+v %v", f.opts[0], r.Warnings)
	}
}

func TestRunPartial(t *testing.T) {
	f := newFake()
	f.failBackup = map[string]bool{"Satis": true}
	j := testJob()
	j.Databases = []string{"Satis", "Yeni", "Yok", "Stok"}
	r := runWith(t, j, f, nil)

	if r.ExitCode != ExitPartial {
		t.Fatalf("kısmi hata bekleniyordu: %d", r.ExitCode)
	}
	ok, failed, skipped := r.Counts()
	if ok != 1 || failed != 3 || skipped != 0 {
		t.Errorf("sayılar: ok=%d failed=%d skipped=%d", ok, failed, skipped)
	}
	// Hata veren DB'den sonra diğerleri devam etmeli.
	if len(f.backups) != 2 || !strings.Contains(r.Databases[0].Error, "3201") {
		t.Errorf("devam etmeli: %v / %+v", f.backups, r.Databases[0])
	}
	if len(f.verify) != 1 {
		t.Errorf("yalnızca başarılı yedek doğrulanmalı: %v", f.verify)
	}
}

func TestRunVerifyFailure(t *testing.T) {
	f := newFake()
	f.failVerify = map[string]bool{`D:\SqlBackups\Yeni_FULL_20261023_020000.bak`: true}
	r := runWith(t, testJob(), f, nil)
	if r.ExitCode != ExitPartial || r.Databases[1].Status != StatusFailed || r.Databases[1].Verified {
		t.Fatalf("doğrulama hatası DB'yi başarısız yapmalı: %+v", r.Databases[1])
	}
}

func TestRunAllFailed(t *testing.T) {
	f := newFake()
	f.failBackup = map[string]bool{"Satis": true, "Yeni": true}
	if r := runWith(t, testJob(), f, nil); r.ExitCode != ExitFailed {
		t.Fatalf("tamamen başarısız bekleniyordu: %d", r.ExitCode)
	}
}

func TestRunOnlySkipped(t *testing.T) {
	// Yeni DB'ler dahil modunda yalnızca OFFLINE bir DB kaldıysa hiçbir şey
	// yedeklenmemiştir; bu başarı sayılmamalı.
	f := newFake()
	j := testJob()
	j.IncludeNewDatabases = true
	j.ExcludeSystemDatabases = true
	j.ExcludedDatabases = []string{"Satis", "Yeni"}
	r := runWith(t, j, f, nil)
	if r.ExitCode != ExitFailed || len(f.backups) != 0 {
		t.Fatalf("exit=%d backups=%v", r.ExitCode, f.backups)
	}
	if _, _, skipped := r.Counts(); skipped != 1 {
		t.Errorf("Stok atlanmalı: %+v", r.Databases)
	}
}

func TestRunConfigError(t *testing.T) {
	called := false
	j := testJob()
	j.Backup.ServerPath = ""
	j.Backup.FileNameTemplate = "{db}_{tarih}.bak"
	r := Run(context.Background(), j, Options{
		Connect: func(context.Context, config.Connection, string) (Server, error) { called = true; return nil, nil },
	})
	if r.ExitCode != ExitFailed || called || !strings.Contains(r.Error, "sunucu yolu") {
		t.Fatalf("config hatası bekleniyordu: exit=%d err=%s", r.ExitCode, r.Error)
	}

	j = testJob()
	j.Backup.FileNameTemplate = "{db}_{tarih}.bak"
	if r := Run(context.Background(), j, Options{}); !strings.Contains(r.Error, "{tarih}") {
		t.Fatalf("şablon hatası bekleniyordu: %s", r.Error)
	}
}

func TestRunConnectError(t *testing.T) {
	r := Run(context.Background(), testJob(), Options{
		Connect: func(context.Context, config.Connection, string) (Server, error) {
			return nil, errors.New("sunucuya bağlanılamadı")
		},
	})
	if r.ExitCode != ExitFailed || r.Server != `.\SQLEXPRESS` || !strings.Contains(r.Summary(), "HATA: sunucuya bağlanılamadı") {
		t.Fatalf("bağlantı hatası: %+v", r)
	}
}

func TestRunCancel(t *testing.T) {
	f := newFake()
	ctx, cancel := context.WithCancel(context.Background())
	f.onBackup = func(string) { cancel() }
	r := Run(ctx, testJob(), Options{
		Connect: func(context.Context, config.Connection, string) (Server, error) { return f, nil },
	})
	if r.ExitCode != ExitFailed || r.Databases[1].Error != "işlem iptal edildi" || len(f.backups) != 0 {
		t.Fatalf("iptal: %+v backups=%v", r.Databases, f.backups)
	}
}

func TestUniqueFileName(t *testing.T) {
	rn := newRunner(testJob(), Options{})
	got := []string{rn.uniqueFileName("a_b.bak"), rn.uniqueFileName("A_B.bak"), rn.uniqueFileName("a_b.BAK")}
	if strings.Join(got, ",") != "a_b.bak,A_B_2.bak,a_b_3.BAK" {
		t.Errorf("benzersiz ad: %v", got)
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KB", 5 << 30: "5.0 GB"} {
		if got := formatBytes(n); got != want {
			t.Errorf("%d: %s, want %s", n, got, want)
		}
	}
}
