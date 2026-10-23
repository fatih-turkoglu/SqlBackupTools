package scheduler

import (
	"strings"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
)

// Warnings, işin zamanlanmış görev olarak çalışmasıyla ilgili uyarılardır
// (görev oluşturulurken ve arayüzde gösterilir). user, "current" hesabıdır.
func Warnings(j *config.Job, user string) []string {
	var w []string
	switch j.Schedule.RunAs {
	case config.RunAsSystem:
		if j.Connection.Auth == config.AuthWindows {
			w = append(w, i18n.T("warn.scheduler.systemWindowsAuth"))
		}
		// SYSTEM ağ paylaşımına makine hesabıyla erişir; yalnızca zip/saklama
		// programın klasöre erişmesini gerektirir.
		p := j.Backup.EffectiveAccessPath()
		if strings.HasPrefix(p, `\\`) && (j.Archive.ZipEnabled || j.Retention.Mode != config.RetentionNone) {
			w = append(w, i18n.T("warn.scheduler.systemNetworkPath", "path", p))
		}
	case config.RunAsCurrent:
		w = append(w, i18n.T("warn.scheduler.currentUser", "user", user))
	}
	return w
}
