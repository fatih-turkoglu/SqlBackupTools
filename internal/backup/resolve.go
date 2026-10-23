package backup

import (
	"strings"

	"sqlbackup/internal/config"
	"sqlbackup/internal/mssql"

	"sqlbackup/internal/i18n"
)

// Target, çözümleme sonucunda yedeklenmesi planlanan veritabanıdır.
type Target struct {
	Name string
	// Reason doluysa veritabanı yedeklenmez; Failed ise hata, değilse atlama
	// olarak raporlanır.
	Reason string
	Failed bool
}

// ResolveDatabases, işin ayarlarına ve sunucudaki listeye göre yedeklenecek
// veritabanlarını belirler. Sıralama korunur, tekrarlar elenir.
//
//   - IncludeNewDatabases açıksa sunucudaki tüm veritabanları (ExcludedDatabases
//     hariç) alınır; yedeklenemeyenler (OFFLINE vb.) atlanır, tempdb sessizce
//     çıkarılır.
//   - Kapalıysa yalnızca seçili veritabanları alınır; sunucuda bulunmayan veya
//     yedeklenemeyen bir seçim hata sayılır, çünkü kullanıcı onu açıkça istemiştir.
//   - ExcludeSystemDatabases her iki modda da sistem veritabanlarını atlar.
func ResolveDatabases(j *config.Job, server []mssql.Database) []Target {
	byName := make(map[string]mssql.Database, len(server))
	for _, d := range server {
		byName[strings.ToLower(d.Name)] = d
	}
	seen := map[string]bool{}
	var out []Target

	add := func(name string, explicit bool) {
		key := strings.ToLower(name)
		if seen[key] {
			return
		}
		seen[key] = true
		d, ok := byName[key]
		if !ok {
			out = append(out, Target{Name: name, Reason: i18n.T("reason.notFound"), Failed: true})
			return
		}
		if d.IsSystem && j.ExcludeSystemDatabases {
			if explicit {
				out = append(out, Target{Name: d.Name, Reason: i18n.T("reason.systemExcluded")})
			}
			return
		}
		if !d.CanBackup {
			if strings.EqualFold(d.Name, "tempdb") && !explicit {
				return
			}
			out = append(out, Target{Name: d.Name, Reason: notBackupableReason(d), Failed: explicit})
			return
		}
		out = append(out, Target{Name: d.Name})
	}

	if j.IncludeNewDatabases {
		for _, name := range j.ExcludedDatabases {
			seen[strings.ToLower(name)] = true
		}
		for _, d := range server {
			add(d.Name, false)
		}
		return out
	}
	for _, name := range j.Databases {
		if strings.TrimSpace(name) != "" {
			add(name, true)
		}
	}
	return out
}

func notBackupableReason(d mssql.Database) string {
	if strings.EqualFold(d.Name, "tempdb") {
		return i18n.T("reason.tempdb")
	}
	return i18n.T("reason.state", "state", d.State)
}
