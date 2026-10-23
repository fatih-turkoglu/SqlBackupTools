package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sqlbackup/internal/i18n"
)

// HomeEnv, veri klasörünü değiştirmek için kullanılan ortam değişkenidir
// (testler ve taşınabilir kurulum için).
const HomeEnv = "SQLBACKUP_HOME"

// DataDir, uygulama veri klasörünü döner. Varsayılan: %ProgramData%\SqlBackup.
// ProgramData seçildi çünkü zamanlanmış görev SYSTEM hesabıyla çalışabilir ve
// kullanıcı profili altındaki dosyalara erişemeyebilir.
func DataDir() string {
	if d := os.Getenv(HomeEnv); d != "" {
		return d
	}
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "SqlBackup")
}

// JobsDir, job JSON ve .bat dosyalarının klasörüdür.
func JobsDir() string { return filepath.Join(DataDir(), "jobs") }

// LogsDir, log dosyalarının klasörüdür.
func LogsDir() string { return filepath.Join(DataDir(), "logs") }

// EnsureDirs, veri klasörlerini oluşturur. Log klasörü ilk oluşturulduğunda
// tüm kullanıcılara değiştirme izni verilir (bkz. grantUsersModify); böylece
// zamanlanmış görev (SYSTEM) ile arayüzü kullanan kullanıcı aynı log
// dosyalarına yazabilir. İzin yalnızca oluşturmada verilir: sonradan
// yalnızca klasörün sahibi değiştirebilir ve değişiklik tüm dosyalara yayılır.
// Log klasörü çalışma sırasında logging paketince de oluşturulabildiği için
// iş çalıştıranlar log açmadan önce bunu çağırmalıdır.
func EnsureDirs() error {
	for _, d := range []string{JobsDir(), LogsDir()} {
		_, statErr := os.Stat(d)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return i18n.Errorf("err.config.mkdir", "path", d, "err", err)
		}
		if d == LogsDir() && errors.Is(statErr, os.ErrNotExist) {
			if err := grantUsersModify(d); err != nil {
				return i18n.Errorf("err.config.dirPermission", "path", d, "err", err)
			}
		}
	}
	return nil
}

// JobPath, verilen iş adının JSON dosya yolunu döner.
func JobPath(name string) string {
	return filepath.Join(JobsDir(), name+".json")
}

// LoadJob, bir job dosyasını okur ve varsayılanları uygular. Doğrulama
// yapmaz; çağıran gerektiğinde Validate çağırmalıdır.
func LoadJob(path string) (*Job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, i18n.Errorf("err.config.jobRead", "err", err)
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, i18n.Errorf("err.config.jobParse", "path", path, "err", err)
	}
	if j.Version > CurrentVersion {
		return nil, i18n.Errorf("err.config.jobVersion", "version", j.Version)
	}
	j.ApplyDefaults()
	return &j, nil
}

// SaveJob, işi verilen yola atomik olarak (geçici dosya + rename) yazar.
func SaveJob(path string, j *Job) error {
	j.ApplyDefaults()
	return writeJSONAtomic(path, j)
}

// writeJSONAtomic, değeri girintili JSON olarak geçici dosya + rename ile yazar.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename başarılıysa dosya zaten yok

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ListJobs, jobs klasöründeki tüm işleri ada göre sıralı döner. Okunamayan
// dosyalar atlanır ve ikinci dönüş değerinde raporlanır.
func ListJobs() ([]*Job, error) {
	entries, err := os.ReadDir(JobsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var jobs []*Job
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		j, err := LoadJob(filepath.Join(JobsDir(), e.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(a, b int) bool {
		return strings.ToLower(jobs[a].Name) < strings.ToLower(jobs[b].Name)
	})
	return jobs, errors.Join(errs...)
}

// DeleteJob, işin JSON dosyasını siler.
func DeleteJob(name string) error {
	err := os.Remove(JobPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
