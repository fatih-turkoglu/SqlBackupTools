package app

import (
	"os"
	"slices"
	"testing"

	"sqlbackup/internal/config"
	"sqlbackup/internal/mssql"
)

// TestRealConnection, köprünün gerçek sürücüyle bağlantı testini ve
// veritabanı listesini sınar. SQLBACKUP_TEST_SERVER tanımlıysa çalışır
// (bkz. internal/mssql/integration_test.go); dosya oluşturmaz.
func TestRealConnection(t *testing.T) {
	server := os.Getenv("SQLBACKUP_TEST_SERVER")
	if server == "" {
		t.Skip("SQLBACKUP_TEST_SERVER tanımlı değil; entegrasyon testi atlandı")
	}
	a := New("test")
	c := config.Connection{Server: server, Auth: config.AuthWindows,
		Protocol: os.Getenv("SQLBACKUP_TEST_PROTOCOL")}
	req := ConnectionRequest{Connection: c}
	if u := os.Getenv("SQLBACKUP_TEST_USER"); u != "" {
		req.Connection.Auth, req.Connection.User = config.AuthSQL, u
		req.Password, req.PasswordChanged = os.Getenv("SQLBACKUP_TEST_PASSWORD"), true
	}
	info, err := a.TestConnection(req)
	if err != nil {
		t.Fatal(err)
	}
	if info.Server.ServerName == "" || info.ProtocolLabel == "" {
		t.Errorf("%+v", info)
	}
	dbs, err := a.ListDatabases(req)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(dbs, func(d mssql.Database) bool { return d.Name == "master" && d.IsSystem }) {
		t.Errorf("master yok: %+v", dbs)
	}
}
