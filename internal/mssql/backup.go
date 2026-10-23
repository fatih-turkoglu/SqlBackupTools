package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

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
