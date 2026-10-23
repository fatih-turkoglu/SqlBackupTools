package backup

// Entegrasyon testi gerçek bir SQL Server gerektirir ve yalnızca
// SQLBACKUP_TEST_SERVER tanımlıysa çalışır (bkz. internal/mssql/integration_test.go).

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	mssqldb "github.com/microsoft/go-mssqldb"

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

func TestIntegrationRun(t *testing.T) {
	conn := testConnection(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Bilgi ve test DB'si için ayrı, doğrudan bir bağlantı açılır.
	cl, err := mssql.Open(ctx, conn, os.Getenv("SQLBACKUP_TEST_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	info := cl.Info()
	cl.Close()
	if info.DefaultBackup == "" {
		t.Skip("varsayılan yedek klasörü okunamadı")
	}
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

	name := fmt.Sprintf("SqlBackupTest_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE DATABASE ["+name+"]"); err != nil {
		t.Fatal(err)
	}

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

	// Temizlik: yedek dosyası sunucu tarafında, tam yol ile silinir.
	t.Cleanup(func() {
		bg := context.Background()
		for _, d := range r.Databases {
			if d.Database == name && d.File != "" {
				if _, err := db.ExecContext(bg, "EXEC master.dbo.xp_delete_file 0, @p1", d.File); err != nil {
					t.Logf("yedek dosyası silinemedi: %v", err)
				}
			}
		}
		db.ExecContext(bg, "ALTER DATABASE ["+name+"] SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE ["+name+"]")
		db.ExecContext(bg, "EXEC msdb.dbo.sp_delete_database_backuphistory @database_name = @p1", name)
	})

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
