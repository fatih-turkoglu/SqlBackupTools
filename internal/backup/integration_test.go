package backup

// Entegrasyon testi gerçek bir SQL Server gerektirir ve yalnızca
// SQLBACKUP_TEST_SERVER tanımlıysa çalışır (bkz. internal/mssql/integration_test.go).

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	mssqldb "github.com/microsoft/go-mssqldb"

	"sqlbackup/internal/archive"
	"sqlbackup/internal/config"
	"sqlbackup/internal/mssql"
)

func testConnection(t *testing.T) config.Connection {
	t.Helper()
	server := os.Getenv("SQLBACKUP_TEST_SERVER")
	if server == "" {
		t.Skip("SQLBACKUP_TEST_SERVER tanımlı değil; entegrasyon testi atlandı")
	}
	c := config.Connection{
		Server:          server,
		Auth:            config.AuthWindows,
		Protocol:        os.Getenv("SQLBACKUP_TEST_PROTOCOL"),
		TrustServerCert: true,
		TimeoutSec:      15,
	}
	if u := os.Getenv("SQLBACKUP_TEST_USER"); u != "" {
		enc, err := config.Protect(os.Getenv("SQLBACKUP_TEST_PASSWORD"))
		if err != nil {
			t.Fatal(err)
		}
		c.Auth, c.User, c.PasswordEnc = config.AuthSQL, u, enc
	}
	return c
}

// integrationEnv, sunucu bilgisi ve DDL için doğrudan bir bağlantı hazırlar.
func integrationEnv(t *testing.T, ctx context.Context) (config.Connection, mssql.ServerInfo, *sql.DB) {
	t.Helper()
	conn := testConnection(t)
	cl, err := mssql.Open(ctx, conn, os.Getenv("SQLBACKUP_TEST_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	info := cl.Info()
	cl.Close()
	dc := conn
	dc.Protocol = info.Protocol
	dsn, err := mssql.ConnString(dc, os.Getenv("SQLBACKUP_TEST_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	connector, err := mssqldb.NewConnector(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { db.Close() })
	return conn, info, db
}

// tempDatabase, test DB'si oluşturur; test sonunda DB'yi ve yedek geçmişini siler.
func tempDatabase(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	name := fmt.Sprintf("SqlBackupTest_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE DATABASE ["+name+"]"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		db.ExecContext(bg, "ALTER DATABASE ["+name+"] SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE ["+name+"]")
		db.ExecContext(bg, "EXEC msdb.dbo.sp_delete_database_backuphistory @database_name = @p1", name)
	})
	return name
}

// deleteServerFiles, yedek dosyalarını sunucu tarafında tam yol ile siler.
func deleteServerFiles(t *testing.T, db *sql.DB, r *Result) {
	for _, d := range r.Databases {
		if d.Status == StatusSuccess && d.File != "" {
			if _, err := db.ExecContext(context.Background(), "EXEC master.dbo.xp_delete_file 0, @p1", d.File); err != nil {
				t.Logf("yedek dosyası silinemedi: %v", err)
			}
		}
	}
}

func TestIntegrationRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, info, db := integrationEnv(t, ctx)
	if info.DefaultBackup == "" {
		t.Skip("varsayılan yedek klasörü okunamadı")
	}
	name := tempDatabase(t, ctx, db)

	j := config.NewJob("entegrasyon")
	j.Connection = conn
	j.Databases = []string{name, "BoyleBirDBYok_xyz"}
	j.Backup.ServerPath = info.DefaultBackup
	j.Backup.Compression = true
	j.Backup.Checksum = true
	j.Backup.CopyOnly = true
	j.Backup.Verify = true

	var last Progress
	r := Run(ctx, j, Options{Progress: func(p Progress) { last = p }})
	t.Log("\n" + r.Summary())
	t.Cleanup(func() { deleteServerFiles(t, db, r) })

	if r.ExitCode != ExitPartial {
		t.Fatalf("kısmi sonuç (1) bekleniyordu: %d", r.ExitCode)
	}
	d := r.Databases[0]
	if d.Status != StatusSuccess || !d.Verified || d.FileBytes <= 0 || d.Compressed != info.SupportsCompression() {
		t.Errorf("test DB sonucu hatalı: %+v", d)
	}
	if r.Databases[1].Status != StatusFailed {
		t.Errorf("olmayan DB hatalı olmalı: %+v", r.Databases[1])
	}
	if !info.SupportsCompression() && len(r.Warnings) == 0 {
		t.Error("sıkıştırma uyarısı bekleniyordu")
	}
	if last.Phase != PhaseVerify || last.Percent != 100 {
		t.Errorf("son ilerleme doğrulama %%100 olmalı: %+v", last)
	}
}

// TestIntegrationZipNoAccess: varsayılan yedek klasörüne (Program Files
// altında) genelde yalnızca SQL servis hesabı yazabilir. Bu durumda yedek
// alınmalı, zip anlaşılır bir hatayla atlanmalıdır.
func TestIntegrationZipNoAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, info, db := integrationEnv(t, ctx)
	if info.DefaultBackup == "" {
		t.Skip("varsayılan yedek klasörü okunamadı")
	}
	if archive.CheckAccess(info.DefaultBackup) == nil {
		t.Skip("varsayılan yedek klasörüne erişim var; senaryo uygulanamaz")
	}
	name := tempDatabase(t, ctx, db)

	j := config.NewJob("entegrasyon")
	j.Connection = conn
	j.Databases = []string{name}
	j.Backup.ServerPath = info.DefaultBackup
	j.Archive.ZipEnabled = true
	j.Archive.DeleteBakAfterZip = true
	r := Run(ctx, j, Options{})
	t.Log("\n" + r.Summary())
	t.Cleanup(func() { deleteServerFiles(t, db, r) })

	if r.ExitCode != ExitPartial || r.Databases[0].Status != StatusSuccess ||
		len(r.Archives) != 1 || !strings.Contains(r.Archives[0].Error, "erişilemiyor") {
		t.Fatalf("yedek alınmalı, zip hata vermeli: %+v", r)
	}
}

// TestIntegrationZip: geçici bir klasöre SQL Server servis hesabı için
// değiştirme izni verilir; yedek + zip + .bak silme uçtan uca denenir.
// Yalnızca yerel sunucuda çalışır.
func TestIntegrationZip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, info, db := integrationEnv(t, ctx)
	if !strings.EqualFold(info.MachineName, hostname()) {
		t.Skip("uzak sunucu; geçici klasör paylaşılamaz")
	}
	dir := t.TempDir()
	account := `NT Service\MSSQLSERVER`
	if _, inst, ok := strings.Cut(info.ServerName, `\`); ok {
		account = `NT Service\MSSQL$` + inst
	}
	if out, err := exec.Command("icacls", dir, "/grant", account+":(OI)(CI)M").CombinedOutput(); err != nil {
		t.Skipf("klasöre servis hesabı izni verilemedi (%s): %v %s", account, err, out)
	}
	name := tempDatabase(t, ctx, db)

	j := config.NewJob("entegrasyon")
	j.Connection = conn
	j.Databases = []string{name}
	j.Backup.ServerPath = dir
	j.Backup.Verify = true
	j.Archive.ZipEnabled = true
	j.Archive.ZipPerDatabase = true
	j.Archive.DeleteBakAfterZip = true
	r := Run(ctx, j, Options{})
	t.Log("\n" + r.Summary())

	if r.ExitCode != ExitOK || len(r.Archives) != 1 || !r.Archives[0].BakDeleted {
		t.Fatalf("zip başarılı olmalı: %+v", r)
	}
	zr, err := zip.OpenReader(r.Archives[0].File)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	// msdb'deki boyut yaklaşıktır; diskteki gerçek boyutla (SourceBytes) karşılaştırılır.
	if len(zr.File) != 1 || zr.File[0].UncompressedSize64 != uint64(r.Archives[0].SourceBytes) {
		t.Errorf("zip içeriği hatalı: %d girdi, %d bayt", len(zr.File), zr.File[0].UncompressedSize64)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".zip") {
		t.Errorf("klasörde yalnızca zip kalmalı: %v", entries)
	}
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// TestIntegrationErrorScenarios: yaygın hata senaryolarında çalıştırıcının
// doğru exit code ve anlaşılır hata verdiğini sınar.
func TestIntegrationErrorScenarios(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, info, db := integrationEnv(t, ctx)
	local := strings.EqualFold(info.MachineName, hostname())

	run := func(t *testing.T, j *config.Job) *Result {
		t.Helper()
		r := Run(ctx, j, Options{})
		t.Log("\n" + r.Summary())
		t.Cleanup(func() { deleteServerFiles(t, db, r) })
		return r
	}

	t.Run("YanlisSifre", func(t *testing.T) {
		j := config.NewJob("entegrasyon")
		j.Connection = conn
		j.Connection.Auth, j.Connection.User = config.AuthSQL, "SqlBackupTest_yok"
		enc, err := config.Protect("yanlis-sifre")
		if err != nil {
			t.Fatal(err)
		}
		j.Connection.PasswordEnc = enc
		j.Databases = []string{"master"}
		j.Backup.ServerPath = info.DefaultBackup
		r := run(t, j)
		if r.ExitCode != ExitFailed || !strings.Contains(r.Error, "18456") {
			t.Fatalf("giriş hatası (exit 2) bekleniyordu: %d %q", r.ExitCode, r.Error)
		}
	})

	t.Run("OfflineDB", func(t *testing.T) {
		name := tempDatabase(t, ctx, db)
		if _, err := db.ExecContext(ctx, "ALTER DATABASE ["+name+"] SET OFFLINE WITH ROLLBACK IMMEDIATE"); err != nil {
			t.Fatal(err)
		}
		// OFFLINE DB silinirse dosyaları diskte kalır; silmeden önce çevrimiçi yap
		// (Cleanup LIFO: tempDatabase'in silmesinden önce çalışır).
		t.Cleanup(func() { db.ExecContext(context.Background(), "ALTER DATABASE ["+name+"] SET ONLINE") })

		j := config.NewJob("entegrasyon")
		j.Connection = conn
		j.Databases = []string{name}
		j.Backup.ServerPath = info.DefaultBackup
		r := run(t, j)
		if r.ExitCode != ExitFailed || len(r.Databases) != 1 || r.Databases[0].Status != StatusFailed ||
			!strings.Contains(r.Databases[0].Error, "OFFLINE") {
			t.Fatalf("OFFLINE DB hata vermeli: %+v", r)
		}
	})

	t.Run("OlmayanSurucu", func(t *testing.T) {
		name := tempDatabase(t, ctx, db)
		j := config.NewJob("entegrasyon")
		j.Connection = conn
		j.Databases = []string{name}
		j.Backup.ServerPath = `Q:\SqlBackupTest_yok\klasor`
		r := run(t, j)
		if r.ExitCode != ExitFailed || r.Databases[0].Status != StatusFailed ||
			!strings.Contains(r.Databases[0].Error, "3201") {
			t.Fatalf("3201 hatası bekleniyordu: %+v", r.Databases)
		}
	})

	// Klasör var ama SQL Server servis hesabının yazma izni yok (en sık
	// karşılaşılan kurulum hatası). Yalnızca yerel sunucuda.
	t.Run("ServisHesabiIzinYok", func(t *testing.T) {
		if !local {
			t.Skip("uzak sunucu")
		}
		dir := t.TempDir() // yalnızca test kullanıcısı + SYSTEM + Administrators
		name := tempDatabase(t, ctx, db)
		j := config.NewJob("entegrasyon")
		j.Connection = conn
		j.Databases = []string{name}
		j.Backup.ServerPath = dir
		r := run(t, j)
		if r.ExitCode != ExitFailed || r.Databases[0].Status != StatusFailed ||
			!strings.Contains(r.Databases[0].Error, "3201") {
			t.Fatalf("erişim reddi (3201) bekleniyordu: %+v", r.Databases)
		}
	})
}
