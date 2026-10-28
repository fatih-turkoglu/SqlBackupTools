package mssql

import (
	"net/url"
	"strings"
	"testing"

	"sqlbackup/internal/config"
)

func TestSplitServer(t *testing.T) {
	cases := []struct{ server, inst, wantHost, wantInst string }{
		{`.\SQLEXPRESS`, "", ".", "SQLEXPRESS"},
		{`(local)`, "", ".", ""},
		{`localhost\X`, "", ".", "X"},
		{`127.0.0.1`, "", ".", ""},
		{`sqlsrv01\PROD`, "", "sqlsrv01", "PROD"},
		{`sqlsrv01\PROD`, "OTHER", "sqlsrv01", "OTHER"},
		{` 10.0.0.5 `, "", "10.0.0.5", ""},
	}
	for _, c := range cases {
		h, i := splitServer(c.server, c.inst)
		if h != c.wantHost || i != c.wantInst {
			t.Errorf("%q: beklenen %s/%s, gelen %s/%s", c.server, c.wantHost, c.wantInst, h, i)
		}
	}
}

func TestConnStringSQLAuthEscapesPassword(t *testing.T) {
	c := config.Connection{Server: `srv\INST`, Auth: config.AuthSQL, User: "sa", TimeoutSec: 10, Protocol: config.ProtocolNP}
	pw := `p@ss;word="x"/?#`
	dsn, err := ConnString(c, pw)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := u.User.Password(); got != pw {
		t.Fatalf("şifre bozuldu: %q", got)
	}
	if u.Path != "/INST" || u.Hostname() != "srv" {
		t.Fatalf("host/instance hatalı: %s %s", u.Host, u.Path)
	}
	q := u.Query()
	if q.Get("pipe") != `MSSQL$INST\sql\query` {
		t.Fatalf("pipe hatalı: %q", q.Get("pipe"))
	}
	// Şifreleme kapalıyken (yalnızca login paketi) sertifika doğrulanmaz.
	if q.Get("connection timeout") != "10" || q.Get("encrypt") != "false" || q.Get("TrustServerCertificate") != "true" {
		t.Fatalf("parametreler hatalı: %v", q)
	}
}

func TestConnStringWindowsAuthHasNoUser(t *testing.T) {
	c := config.Connection{Server: "srv", Auth: config.AuthWindows, User: "ignored", Port: 1433,
		Protocol: config.ProtocolTCP, Encrypt: true, TrustServerCert: true}
	dsn, err := ConnString(c, "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	if u.User != nil {
		t.Fatal("Windows auth'ta kullanıcı olmamalı")
	}
	q := u.Query()
	if u.Port() != "1433" || q.Get("protocol") != "tcp" || q.Has("pipe") {
		t.Fatalf("beklenmeyen dsn: %s", dsn)
	}
	if q.Get("encrypt") != "true" || q.Get("TrustServerCertificate") != "true" {
		t.Fatalf("şifreleme parametreleri hatalı: %s", dsn)
	}
}

func TestConnStringEncryptValidatesCert(t *testing.T) {
	dsn, err := ConnString(config.Connection{Server: "srv", Auth: config.AuthWindows, Encrypt: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	if q := u.Query(); q.Get("encrypt") != "true" || q.Has("TrustServerCertificate") {
		t.Fatalf("şifreleme açıkken sertifika doğrulanmalı: %s", dsn)
	}
}

func TestCandidateProtocols(t *testing.T) {
	cases := []struct {
		c    config.Connection
		want string
	}{
		{config.Connection{Server: `.\SQLEXPRESS`}, "lpc,np,tcp"},
		{config.Connection{Server: `localhost`}, "lpc,np,tcp"},
		{config.Connection{Server: `sqlsrv01\PROD`}, "tcp,np"},
		{config.Connection{Server: `.`, Port: 1433}, "tcp"},
		{config.Connection{Server: `sqlsrv01`, Protocol: config.ProtocolNP}, "np"},
	}
	for _, c := range cases {
		if got := strings.Join(candidateProtocols(c.c), ","); got != c.want {
			t.Errorf("%+v: beklenen %s, gelen %s", c.c, c.want, got)
		}
	}
}

func TestDefaultPipe(t *testing.T) {
	if p := defaultPipe(""); p != `sql\query` {
		t.Fatal(p)
	}
	if p := defaultPipe("mssqlserver"); p != `sql\query` {
		t.Fatal(p)
	}
	if p := defaultPipe("sqlexpress"); p != `MSSQL$SQLEXPRESS\sql\query` {
		t.Fatal(p)
	}
}

func TestBuildBackupQuery(t *testing.T) {
	q, args := buildBackupQuery("ERP];DROP", `D:\b\x.bak`, BackupOptions{Compression: true, Checksum: true, CopyOnly: true, Description: "gece"})
	for _, want := range []string{"BACKUP DATABASE @db TO DISK = @file", "INIT", "COMPRESSION", "CHECKSUM", "COPY_ONLY", "STATS = 5", "DESCRIPTION = @desc"} {
		if !strings.Contains(q, want) {
			t.Errorf("sorguda %q yok: %s", want, q)
		}
	}
	if strings.Contains(q, "ERP") {
		t.Fatal("veritabanı adı sorguya gömülmemeli")
	}
	if len(args) != 3 {
		t.Fatalf("3 parametre bekleniyordu: %d", len(args))
	}

	q, _ = buildBackupQuery("ERP", "x", BackupOptions{})
	if strings.Contains(q, "COMPRESSION") || strings.Contains(q, "CHECKSUM") || strings.Contains(q, "COPY_ONLY") {
		t.Fatalf("seçenekler kapalıyken eklenmemeli: %s", q)
	}
}

func TestSupportsCompression(t *testing.T) {
	if (ServerInfo{Edition: "Express Edition (64-bit)"}).SupportsCompression() {
		t.Fatal("Express desteklememeli")
	}
	if !(ServerInfo{Edition: "Standard Edition (64-bit)"}).SupportsCompression() {
		t.Fatal("Standard desteklemeli")
	}
}

func TestOSErrorHint(t *testing.T) {
	inst := ServerInfo{ServerName: `SRV\SQLEXPRESS`}
	cases := []struct {
		msg, want string
	}{
		{`[3201] Cannot open backup device 'D:\x.bak'. Operating system error 5(Erişim engellendi.). | [3013] ...`, `NT Service\MSSQL$SQLEXPRESS`},
		{`[3201] Cannot open backup device 'Q:\x.bak'. Operating system error 3(The system cannot find the path specified.).`, "bulunamadı"},
		{`[3202] Write on "D:\x.bak" failed: 112(There is not enough space on the disk.)`, "boş alan"},
		{"[3201] ... Operating system error 1450(...)", ""},
		{"[4060] Cannot open database", ""},
	}
	for _, c := range cases {
		got := inst.osErrorHint(c.msg)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%q: beklenen %q içeren ipucu, gelen %q", c.msg, c.want, got)
		}
	}
	if a := (ServerInfo{ServerName: "SRV"}).serviceAccount(); a != `NT Service\MSSQLSERVER` {
		t.Errorf("varsayılan örnek hesabı: %s", a)
	}
}
