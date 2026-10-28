package mssql

// Entegrasyon testleri gerçek bir SQL Server gerektirir ve yalnızca
// SQLBACKUP_TEST_SERVER tanımlıysa çalışır. Örnek (PowerShell):
//
//	$env:SQLBACKUP_TEST_SERVER = '.\SQLEXPRESS'
//	go test ./internal/mssql/ -v
//
// SQLBACKUP_TEST_USER / SQLBACKUP_TEST_PASSWORD verilmezse Windows kimlik
// doğrulaması kullanılır. SQLBACKUP_TEST_PROTOCOL ile protokol zorlanabilir.
// Testler yedek dosyasını sunucunun varsayılan yedek klasörüne yazar; bu
// klasörün test makinesinden erişilebilir olması (yerel sunucu) beklenir.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sqlbackup/internal/config"
)

func testClient(t *testing.T) *Client {
	t.Helper()
	server := os.Getenv("SQLBACKUP_TEST_SERVER")
	if server == "" {
		t.Skip("SQLBACKUP_TEST_SERVER tanımlı değil; entegrasyon testi atlandı")
	}
	// Varsayılan ayarlar (şifreleme kapalı, sertifikaya güven kapalı): yeni
	// bir işin bağlanabildiği sınanır.
	c := config.Connection{
		Server:     server,
		Auth:       config.AuthWindows,
		Protocol:   os.Getenv("SQLBACKUP_TEST_PROTOCOL"),
		TimeoutSec: 15,
	}
	if u := os.Getenv("SQLBACKUP_TEST_USER"); u != "" {
		c.Auth, c.User = config.AuthSQL, u
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	cl, err := Open(ctx, c, os.Getenv("SQLBACKUP_TEST_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bağlantı süresi: %v", time.Since(start).Round(time.Millisecond))
	t.Cleanup(func() { cl.Close() })
	return cl
}

// tempDatabase, test için küçük bir veritabanı oluşturur ve test sonunda siler.
func tempDatabase(t *testing.T, cl *Client) string {
	t.Helper()
	name := fmt.Sprintf("SqlBackupTest_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := cl.db.ExecContext(ctx, "CREATE DATABASE ["+name+"]"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cl.db.ExecContext(ctx, "ALTER DATABASE ["+name+"] SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE ["+name+"]")
		cl.db.ExecContext(ctx, "EXEC msdb.dbo.sp_delete_database_backuphistory @database_name = @p1", name)
	})
	_, err := cl.db.ExecContext(ctx, "USE ["+name+"]; CREATE TABLE t (id int IDENTITY, v nvarchar(100)); "+
		"INSERT t(v) SELECT TOP 2000 N'satır' FROM sys.all_objects;")
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestIntegrationServerInfo(t *testing.T) {
	cl := testClient(t)
	i := cl.Info()
	t.Logf("%+v", i)
	if i.ProductVersion == "" || i.Edition == "" {
		t.Fatalf("sunucu bilgisi eksik: %+v", i)
	}
}

func TestIntegrationListDatabases(t *testing.T) {
	cl := testClient(t)
	name := tempDatabase(t, cl)
	dbs, err := cl.ListDatabases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found, tempdb bool
	for _, d := range dbs {
		t.Logf("%-40s %-8s %-6s %8.1f MB sys=%v backup=%v last=%v", d.Name, d.State, d.RecoveryModel, d.SizeMB, d.IsSystem, d.CanBackup, d.LastFullBackup)
		if d.Name == name {
			found = true
			if d.IsSystem || !d.CanBackup || d.SizeMB <= 0 {
				t.Errorf("test DB bilgisi hatalı: %+v", d)
			}
		}
		if d.Name == "tempdb" {
			tempdb = true
			if d.CanBackup || !d.IsSystem {
				t.Errorf("tempdb yedeklenemez olarak işaretlenmeli: %+v", d)
			}
		}
	}
	if !found || !tempdb {
		t.Fatalf("beklenen veritabanları listede yok (test=%v tempdb=%v)", found, tempdb)
	}
}

func TestIntegrationBackupAndVerify(t *testing.T) {
	cl := testClient(t)
	dir := cl.Info().DefaultBackup
	if dir == "" {
		t.Skip("varsayılan yedek klasörü okunamadı")
	}
	name := tempDatabase(t, cl)
	file := filepath.Join(dir, name+".bak")
	// Yedek klasörüne genelde yalnızca SQL Server servis hesabı yazabilir;
	// bu yüzden dosya sunucu tarafında silinir.
	t.Cleanup(func() {
		if _, err := cl.db.Exec("EXEC master.dbo.xp_delete_file 0, @p1", file); err != nil {
			t.Logf("yedek dosyası silinemedi: %v", err)
		}
	})

	var pcts []int
	opts := BackupOptions{Checksum: true, CopyOnly: true, Description: "SqlBackup entegrasyon testi",
		Compression: cl.Info().SupportsCompression()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := cl.BackupDatabase(ctx, name, file, opts, func(p int) { pcts = append(pcts, p) }); err != nil {
		t.Fatal(err)
	}
	t.Logf("ilerleme: %v", pcts)
	if len(pcts) == 0 || pcts[len(pcts)-1] != 100 {
		t.Errorf("ilerleme 100 ile bitmeli: %v", pcts)
	}
	set, err := cl.LastBackupSet(ctx, name, file)
	if err != nil || set == nil || set.SizeBytes <= 0 || set.CompressedBytes <= 0 {
		t.Fatalf("yedek seti bilgisi okunamadı: %+v %v", set, err)
	}
	t.Logf("yedek seti: %+v", *set)
	if set, err := cl.LastBackupSet(ctx, name, file+".yok"); set != nil || err != nil {
		t.Errorf("olmayan dosya için (nil, nil) bekleniyordu: %+v %v", set, err)
	}
	var vp []int
	if err := cl.VerifyBackup(ctx, file, true, func(p int) { vp = append(vp, p) }); err != nil {
		t.Fatal(err)
	}
	t.Logf("doğrulama ilerleme: %v", vp)
}

func TestIntegrationBackupErrors(t *testing.T) {
	cl := testClient(t)
	ctx := context.Background()

	err := cl.BackupDatabase(ctx, "master", `Z:\yok\olmayan\klasor\x.bak`, BackupOptions{}, nil)
	if err == nil || !strings.Contains(err.Error(), "[3201]") {
		t.Fatalf("3201 hatası bekleniyordu: %v", err)
	}
	t.Log(err)

	err = cl.BackupDatabase(ctx, "BoyleBirDBYok_xyz", filepath.Join(cl.Info().DefaultBackup, "x.bak"), BackupOptions{}, nil)
	if err == nil {
		t.Fatal("olmayan DB için hata bekleniyordu")
	}
	t.Log(err)

	err = cl.VerifyBackup(ctx, `Z:\yok.bak`, false, nil)
	if err == nil {
		t.Fatal("olmayan dosya doğrulaması hata vermeli")
	}
}

func TestIntegrationCancel(t *testing.T) {
	cl := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := cl.db.QueryContext(ctx, "WAITFOR DELAY '00:00:10'")
	if err == nil {
		// QueryContext hemen dönmüş olabilir; mesaj döngüsü üzerinden de kontrol edelim.
		err = cl.execWithMessages(ctx, "WAITFOR DELAY '00:00:10'", nil)
	}
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("iptal çalışmadı: err=%v süre=%v", err, time.Since(start))
	}
}
