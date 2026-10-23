package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func validJob() *Job {
	j := NewJob("gece_full")
	j.Connection.Server = "sqlsrv01"
	j.Connection.User = "sa"
	j.Connection.PasswordEnc = "x"
	j.Databases = []string{"ERP"}
	j.Backup.ServerPath = `D:\Backups`
	return j
}

func TestNewJobDefaultsAreValidOnceFilled(t *testing.T) {
	if err := validJob().Validate(); err != nil {
		t.Fatalf("geçerli iş hata verdi: %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Job)
		want   string
	}{
		{"boş ad", func(j *Job) { j.Name = "" }, "iş adı"},
		{"geçersiz ad", func(j *Job) { j.Name = "a/b" }, "iş adı"},
		{"sadece nokta", func(j *Job) { j.Name = ".." }, "noktadan"},
		{"sunucu yok", func(j *Job) { j.Connection.Server = " " }, "sunucu adı"},
		{"port", func(j *Job) { j.Connection.Port = 70000 }, "port"},
		{"sql kullanıcı", func(j *Job) { j.Connection.User = "" }, "kullanıcı adı"},
		{"sql şifre", func(j *Job) { j.Connection.PasswordEnc = "" }, "şifre"},
		{"auth", func(j *Job) { j.Connection.Auth = "x" }, "kimlik doğrulama"},
		{"db yok", func(j *Job) { j.Databases = nil }, "veritabanı"},
		{"yol yok", func(j *Job) { j.Backup.ServerPath = "" }, "yedek klasörü"},
		{"şablon db", func(j *Job) { j.Backup.FileNameTemplate = "a.bak" }, "{db}"},
		{"şablon karakter", func(j *Job) { j.Backup.FileNameTemplate = `x\{db}.bak` }, "içeremez"},
		{"zip sil", func(j *Job) { j.Archive.DeleteBakAfterZip = true }, "zip"},
		{"retention", func(j *Job) { j.Retention.Mode = RetentionDays }, "saklama değeri"},
		{"saat", func(j *Job) { j.Schedule.Time = "25:00" }, "saati"},
		{"haftalık gün", func(j *Job) { j.Schedule.Frequency = FrequencyWeekly }, "gün"},
		{"geçersiz gün", func(j *Job) {
			j.Schedule.Frequency = FrequencyWeekly
			j.Schedule.Days = []string{"PZT"}
		}, "geçersiz gün"},
		{"saatlik", func(j *Job) { j.Schedule.Frequency = FrequencyHourly }, "1-23"},
		{"runas", func(j *Job) { j.Schedule.RunAs = "x" }, "çalışma hesabı"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := validJob()
			tc.mutate(j)
			err := j.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("beklenen %q içeren hata, gelen: %v", tc.want, err)
			}
		})
	}
}

func TestWindowsAuthNeedsNoPassword(t *testing.T) {
	j := validJob()
	j.Connection.Auth = AuthWindows
	j.Connection.User = ""
	j.Connection.PasswordEnc = ""
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIncludeNewDatabasesAllowsEmptySelection(t *testing.T) {
	j := validJob()
	j.Databases = nil
	j.IncludeNewDatabases = true
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEffectiveAccessPath(t *testing.T) {
	b := Backup{ServerPath: `D:\Backups`}
	if got := b.EffectiveAccessPath(); got != `D:\Backups` {
		t.Fatal(got)
	}
	b.AccessPath = `\\sqlsrv01\Backups$`
	if got := b.EffectiveAccessPath(); got != `\\sqlsrv01\Backups$` {
		t.Fatal(got)
	}
}

func TestSaveLoadListDelete(t *testing.T) {
	t.Setenv(HomeEnv, t.TempDir())
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	j := validJob()
	j.Name = "Müşteri_DB"
	j.Schedule.Days = []string{"MON", "FRI"}
	if err := SaveJob(JobPath(j.Name), j); err != nil {
		t.Fatal(err)
	}
	other := validJob()
	other.Name = "a_job"
	if err := SaveJob(JobPath(other.Name), other); err != nil {
		t.Fatal(err)
	}
	// Bozuk dosya listeyi engellememeli.
	if err := os.WriteFile(filepath.Join(JobsDir(), "bozuk.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadJob(JobPath(j.Name))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, j) {
		t.Fatalf("okunan iş farklı:\n%+v\n%+v", got, j)
	}

	jobs, err := ListJobs()
	if err == nil {
		t.Fatal("bozuk dosya için hata bekleniyordu")
	}
	if len(jobs) != 2 || jobs[0].Name != "a_job" || jobs[1].Name != "Müşteri_DB" {
		t.Fatalf("liste hatalı: %d", len(jobs))
	}

	if err := DeleteJob(j.Name); err != nil {
		t.Fatal(err)
	}
	if err := DeleteJob(j.Name); err != nil {
		t.Fatal("olmayan işi silmek hata vermemeli")
	}
	if _, err := os.Stat(JobPath(j.Name)); !os.IsNotExist(err) {
		t.Fatal("dosya silinmedi")
	}
}

func TestLoadRejectsNewerVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(p, []byte(`{"version": 99}`), 0o644)
	if _, err := LoadJob(p); err == nil {
		t.Fatal("hata bekleniyordu")
	}
}
