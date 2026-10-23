package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// EnsureDirs, veri klasörlerini oluşturur.
func EnsureDirs() error {
	for _, d := range []string{JobsDir(), LogsDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("klasör oluşturulamadı %s: %w", d, err)
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
		return nil, fmt.Errorf("iş dosyası okunamadı: %w", err)
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("iş dosyası çözümlenemedi (%s): %w", path, err)
	}
	if j.Version > CurrentVersion {
		return nil, fmt.Errorf("iş dosyası daha yeni bir sürüme ait (v%d), programı güncelleyin", j.Version)
	}
	j.ApplyDefaults()
	return &j, nil
}

// SaveJob, işi verilen yola atomik olarak (geçici dosya + rename) yazar.
func SaveJob(path string, j *Job) error {
	j.ApplyDefaults()
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".job-*.tmp")
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
