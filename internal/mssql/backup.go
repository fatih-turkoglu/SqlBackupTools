package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang-sql/sqlexp"
	mssqldb "github.com/microsoft/go-mssqldb"
)

// ProgressFunc, yedek/doğrulama sırasında yüzde bilgisini bildirir.
type ProgressFunc func(percent int)

// BackupOptions, BACKUP DATABASE komutunun seçenekleridir.
type BackupOptions struct {
	Compression bool
	Checksum    bool
	CopyOnly    bool
	// Description, yedek setine yazılan açıklamadır (opsiyonel).
	Description string
}

// SQL Server bilgi mesajı numaraları.
const (
	msgPercentProcessed = 3211 // "%d percent processed."
)

var firstNumberRe = regexp.MustCompile(`\d+`)

// BackupDatabase, veritabanının FULL yedeğini sunucu tarafındaki file
// yoluna alır. Veritabanı adı ve dosya yolu parametre olarak gönderilir;
// bu sayede SQL injection riski yoktur.
func (c *Client) BackupDatabase(ctx context.Context, database, file string, opts BackupOptions, progress ProgressFunc) error {
	query, args := buildBackupQuery(database, file, opts)
	if err := c.execWithMessages(ctx, query, progress, args...); err != nil {
		return fmt.Errorf("%s yedeklenemedi: %w", database, err)
	}
	return nil
}

func buildBackupQuery(database, file string, opts BackupOptions) (string, []any) {
	with := []string{"INIT", "FORMAT", "STATS = 5"}
	if opts.Compression {
		with = append(with, "COMPRESSION")
	}
	if opts.Checksum {
		with = append(with, "CHECKSUM")
	}
	if opts.CopyOnly {
		with = append(with, "COPY_ONLY")
	}
	args := []any{sql.Named("db", database), sql.Named("file", file)}
	if opts.Description != "" {
		with = append(with, "DESCRIPTION = @desc")
		args = append(args, sql.Named("desc", truncate(opts.Description, 255)))
	}
	q := "BACKUP DATABASE @db TO DISK = @file WITH " + strings.Join(with, ", ")
	return q, args
}

// VerifyBackup, yedek dosyasını RESTORE VERIFYONLY ile doğrular. Yedek
// CHECKSUM ile alındıysa checksum da kontrol edilir.
func (c *Client) VerifyBackup(ctx context.Context, file string, checksum bool, progress ProgressFunc) error {
	q := "RESTORE VERIFYONLY FROM DISK = @file WITH STATS = 10"
	if checksum {
		q += ", CHECKSUM"
	}
	if err := c.execWithMessages(ctx, q, progress, sql.Named("file", file)); err != nil {
		return fmt.Errorf("yedek doğrulanamadı (%s): %w", file, err)
	}
	return nil
}

// BackupSetInfo, msdb'deki bir yedek setinin bilgisidir.
type BackupSetInfo struct {
	// SizeBytes, yedeklenen veri boyutudur (sıkıştırma öncesi).
	SizeBytes int64
	// CompressedBytes, sıkıştırılmış boyuttur; sıkıştırma yoksa SizeBytes'a
	// eşittir. Diskteki dosya boyutuna yaklaşık olarak karşılık gelir.
	CompressedBytes int64
	// Finish, sunucunun yerel saatidir; datetime saat dilimi taşımadığı için
	// sürücü bunu UTC olarak işaretler.
	Finish time.Time
}

// LastBackupSet, veritabanının verilen dosyaya alınmış son FULL yedeğinin
// bilgisini msdb'den okur. Dosya sunucu servis hesabı tarafından yazıldığı
// için program dosyaya erişemeyebilir; boyut bu yüzden msdb'den alınır.
// Kayıt yoksa (nil, nil) döner.
func (c *Client) LastBackupSet(ctx context.Context, database, file string) (*BackupSetInfo, error) {
	const q = `
SELECT TOP (1)
  CAST(bs.backup_size AS bigint),
  CAST(ISNULL(bs.compressed_backup_size, bs.backup_size) AS bigint),
  bs.backup_finish_date
FROM msdb.dbo.backupset bs
JOIN msdb.dbo.backupmediafamily mf ON mf.media_set_id = bs.media_set_id
WHERE bs.database_name = @db AND mf.physical_device_name = @file AND bs.type = 'D'
ORDER BY bs.backup_finish_date DESC`
	var i BackupSetInfo
	err := c.db.QueryRowContext(ctx, q, sql.Named("db", database), sql.Named("file", file)).
		Scan(&i.SizeBytes, &i.CompressedBytes, &i.Finish)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("yedek bilgisi okunamadı (%s): %w", database, err)
	}
	return &i, nil
}

// execWithMessages, sorguyu çalıştırır ve sunucunun gönderdiği bilgi
// mesajlarını (STATS yüzdeleri) ve hataları toplar. BACKUP/RESTORE hataları
// genellikle birden çok mesaj olarak gelir (ör. 3201 + 3013); hepsi
// birleştirilerek döner.
func (c *Client) execWithMessages(ctx context.Context, query string, progress ProgressFunc, args ...any) error {
	retmsg := &sqlexp.ReturnMessage{}
	rows, err := c.db.QueryContext(ctx, query, append(args, retmsg)...)
	if err != nil {
		return err
	}
	defer rows.Close()

	var sqlErrs []string
	for active := true; active; {
		switch m := retmsg.Message(ctx).(type) {
		case sqlexp.MsgNotice:
			if e, ok := m.Message.(mssqldb.Error); ok && e.Number == msgPercentProcessed && progress != nil {
				if n, err := strconv.Atoi(firstNumberRe.FindString(e.Message)); err == nil {
					progress(n)
				}
			}
		case sqlexp.MsgError:
			sqlErrs = append(sqlErrs, errorText(m.Error))
		case sqlexp.MsgNext:
			for rows.Next() {
			}
		case sqlexp.MsgNextResultSet:
			active = rows.NextResultSet()
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("işlem iptal edildi veya zaman aşımına uğradı: %w", err)
	}
	if len(sqlErrs) > 0 {
		return errors.New(strings.Join(sqlErrs, " | "))
	}
	return rows.Err()
}

func errorText(err error) string {
	var e mssqldb.Error
	if errors.As(err, &e) {
		return fmt.Sprintf("[%d] %s", e.Number, e.Message)
	}
	return err.Error()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
