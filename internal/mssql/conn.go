// Package mssql, SQL Server bağlantısı, veritabanı listeleme ve yedekleme işlemlerini içerir.
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	mssqldb "github.com/microsoft/go-mssqldb"
	// Named Pipes (np:) ve Shared Memory (lpc:) protokollerini kaydeder.
	_ "github.com/microsoft/go-mssqldb/namedpipe"
	_ "github.com/microsoft/go-mssqldb/sharedmemory"

	"sqlbackup/internal/config"
)

// AppName, SQL Server oturumlarında görünen uygulama adıdır.
const AppName = "SqlBackup"

// Client, tek bir SQL Server'a açılmış bağlantı havuzudur.
type Client struct {
	db   *sql.DB
	info ServerInfo
}

// ServerInfo, bağlanılan sunucunun temel bilgileridir.
type ServerInfo struct {
	ServerName     string `json:"serverName"`
	MachineName    string `json:"machineName"`
	ProductVersion string `json:"productVersion"`
	ProductLevel   string `json:"productLevel"`
	Edition        string `json:"edition"`
	EngineEdition  int    `json:"engineEdition"`
	IsSysAdmin     bool   `json:"isSysAdmin"`
	LoginName      string `json:"loginName"`
	DefaultBackup  string `json:"defaultBackupDir"`
	// Protocol, bağlantının kurulduğu protokoldür (tcp, np, lpc).
	Protocol string `json:"protocol"`
}

// SupportsCompression, sunucu sürümünün yedek sıkıştırmayı destekleyip
// desteklemediğini döner. Express ve Web sürümleri desteklemez.
func (i ServerInfo) SupportsCompression() bool {
	e := strings.ToLower(i.Edition)
	return !strings.Contains(e, "express") && !strings.Contains(e, "web")
}

// Open, bağlantıyı açar, sunucuya ping atar ve sunucu bilgisini okur.
// Protokol "otomatik" ise uygun protokoller sırayla denenir ve ilk başarılı
// olan kullanılır.
func Open(ctx context.Context, c config.Connection, password string) (*Client, error) {
	var errs []string
	for _, p := range candidateProtocols(c) {
		cc := c
		cc.Protocol = p
		cl, err := openWith(ctx, cc, password)
		if err == nil {
			cl.info.Protocol = p
			return cl, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", protocolLabel(p), err))
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("sunucuya bağlanılamadı: %s", strings.Join(errs, " | "))
}

func openWith(ctx context.Context, c config.Connection, password string) (*Client, error) {
	dsn, err := ConnString(c, password)
	if err != nil {
		return nil, err
	}
	connector, err := mssqldb.NewConnector(dsn)
	if err != nil {
		return nil, fmt.Errorf("bağlantı bilgisi geçersiz: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)

	cl := &Client{db: db}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if cl.info, err = cl.readServerInfo(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return cl, nil
}

// candidateProtocols, denenecek protokolleri sırasıyla döner. Yerel
// sunucuda Shared Memory en hızlı ve ağ ayarlarından bağımsız olduğu için
// önce denenir. Port verilmişse yalnızca TCP kullanılabilir.
func candidateProtocols(c config.Connection) []string {
	if c.Protocol != config.ProtocolAuto {
		return []string{c.Protocol}
	}
	if c.Port > 0 {
		return []string{config.ProtocolTCP}
	}
	host, _ := splitServer(c.Server, c.Instance)
	if host == "." {
		return []string{config.ProtocolLPC, config.ProtocolNP, config.ProtocolTCP}
	}
	return []string{config.ProtocolTCP, config.ProtocolNP}
}

func protocolLabel(p string) string {
	switch p {
	case config.ProtocolTCP:
		return "TCP/IP"
	case config.ProtocolNP:
		return "Named Pipes"
	case config.ProtocolLPC:
		return "Shared Memory"
	}
	return p
}

// Close, bağlantı havuzunu kapatır.
func (c *Client) Close() error { return c.db.Close() }

// Info, bağlantı açılırken okunan sunucu bilgisini döner.
func (c *Client) Info() ServerInfo { return c.info }

func (c *Client) readServerInfo(ctx context.Context) (ServerInfo, error) {
	const q = `
SELECT
  CAST(@@SERVERNAME AS nvarchar(256)),
  CAST(SERVERPROPERTY('MachineName') AS nvarchar(256)),
  CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)),
  CAST(SERVERPROPERTY('ProductLevel') AS nvarchar(128)),
  CAST(SERVERPROPERTY('Edition') AS nvarchar(256)),
  CAST(SERVERPROPERTY('EngineEdition') AS int),
  CAST(IS_SRVROLEMEMBER('sysadmin') AS int),
  SUSER_SNAME()`
	var i ServerInfo
	var serverName sql.NullString
	var sysadmin sql.NullInt32
	err := c.db.QueryRowContext(ctx, q).Scan(&serverName, &i.MachineName, &i.ProductVersion,
		&i.ProductLevel, &i.Edition, &i.EngineEdition, &sysadmin, &i.LoginName)
	if err != nil {
		return i, fmt.Errorf("sunucu bilgisi okunamadı: %w", err)
	}
	i.ServerName = serverName.String
	i.IsSysAdmin = sysadmin.Int32 == 1

	// Varsayılan yedek klasörü; yetki yoksa boş kalır.
	var dir sql.NullString
	_ = c.db.QueryRowContext(ctx,
		`SELECT CAST(SERVERPROPERTY('InstanceDefaultBackupPath') AS nvarchar(512))`).Scan(&dir)
	i.DefaultBackup = dir.String
	return i, nil
}

// ConnString, config bilgilerinden go-mssqldb URL bağlantı dizesi üretir.
// URL biçimi seçildi; böylece şifre ve kullanıcı adındaki özel karakterler
// (; = " vb.) güvenle kodlanır.
func ConnString(c config.Connection, password string) (string, error) {
	host, instance := splitServer(c.Server, c.Instance)
	if host == "" {
		return "", fmt.Errorf("sunucu adı boş")
	}

	u := &url.URL{Scheme: "sqlserver", Host: host}
	if c.Port > 0 {
		u.Host = host + ":" + strconv.Itoa(c.Port)
	}
	if instance != "" {
		u.Path = "/" + instance
	}

	switch c.Auth {
	case config.AuthWindows:
		// Kullanıcı verilmezse sürücü Windows tümleşik kimlik doğrulaması (SSPI) kullanır.
	default:
		u.User = url.UserPassword(c.User, password)
	}

	q := url.Values{}
	q.Set("database", "master")
	q.Set("app name", AppName)
	if c.TimeoutSec > 0 {
		q.Set("connection timeout", strconv.Itoa(c.TimeoutSec))
		q.Set("dial timeout", strconv.Itoa(c.TimeoutSec))
	}
	if c.Encrypt {
		q.Set("encrypt", "true")
	} else {
		q.Set("encrypt", "false")
	}
	if c.TrustServerCert {
		q.Set("TrustServerCertificate", "true")
	}
	if c.Protocol != config.ProtocolAuto {
		q.Set("protocol", c.Protocol)
	}
	// Named Pipes için pipe adını açıkça veriyoruz; böylece SQL Browser
	// servisi kapalı olsa da instance'a bağlanılabilir.
	if c.Protocol == config.ProtocolNP {
		q.Set("pipe", defaultPipe(instance))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// splitServer, "host\instance" biçimini ayırır ve yerel makineyi gösteren
// adları (localhost, (local), 127.0.0.1, makine adı) "." olarak normalize
// eder. "." seçildi çünkü Named Pipes yerelde \\.\pipe\... yolunu ister;
// \\localhost\pipe\... ağ yığını üzerinden gider ve engellenebilir.
func splitServer(server, instance string) (string, string) {
	server = strings.TrimSpace(server)
	instance = strings.TrimSpace(instance)
	if h, inst, ok := strings.Cut(server, `\`); ok {
		server = h
		if instance == "" {
			instance = inst
		}
	}
	if isLocalHost(server) {
		server = "."
	}
	return server, instance
}

func isLocalHost(h string) bool {
	switch strings.ToLower(h) {
	case ".", "(local)", "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	name, err := os.Hostname()
	return err == nil && strings.EqualFold(h, name)
}

func defaultPipe(instance string) string {
	if instance == "" || strings.EqualFold(instance, "MSSQLSERVER") {
		return `sql\query`
	}
	return `MSSQL$` + strings.ToUpper(instance) + `\sql\query`
}
