package mssql

import (
	"context"
	"database/sql"
	"time"

	"sqlbackup/internal/i18n"
)

// Database, sunucudaki bir veritabanının listeleme bilgisidir.
type Database struct {
	Name           string     `json:"name"`
	State          string     `json:"state"`
	RecoveryModel  string     `json:"recoveryModel"`
	SizeMB         float64    `json:"sizeMB"`
	IsSystem       bool       `json:"isSystem"`
	IsReadOnly     bool       `json:"isReadOnly"`
	CanBackup      bool       `json:"canBackup"`
	LastFullBackup *time.Time `json:"lastFullBackup"`
}

// ListDatabases, sunucudaki veritabanlarını listeler. Snapshot'lar dahil
// edilmez; tempdb yedeklenemeyeceği için CanBackup=false döner.
func (c *Client) ListDatabases(ctx context.Context) ([]Database, error) {
	const q = `
SELECT
  d.name,
  d.state_desc,
  d.recovery_model_desc,
  CAST(ISNULL(f.size_pages, 0) * 8 / 1024.0 AS float),
  CAST(CASE WHEN d.database_id <= 4 THEN 1 ELSE 0 END AS bit),
  d.is_read_only,
  b.last_full
FROM sys.databases d
OUTER APPLY (
  SELECT SUM(CAST(mf.size AS bigint)) AS size_pages
  FROM sys.master_files mf WHERE mf.database_id = d.database_id
) f
OUTER APPLY (
  SELECT MAX(bs.backup_finish_date) AS last_full
  FROM msdb.dbo.backupset bs
  WHERE bs.database_name = d.name AND bs.type = 'D'
) b
WHERE d.source_database_id IS NULL
ORDER BY CASE WHEN d.database_id <= 4 THEN 0 ELSE 1 END, d.name`

	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, i18n.Errorf("err.mssql.listDatabases", "err", err)
	}
	defer rows.Close()

	var list []Database
	for rows.Next() {
		var d Database
		var last sql.NullTime
		if err := rows.Scan(&d.Name, &d.State, &d.RecoveryModel, &d.SizeMB,
			&d.IsSystem, &d.IsReadOnly, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			t := last.Time
			d.LastFullBackup = &t
		}
		d.CanBackup = d.Name != "tempdb" && d.State == "ONLINE"
		list = append(list, d)
	}
	return list, rows.Err()
}
