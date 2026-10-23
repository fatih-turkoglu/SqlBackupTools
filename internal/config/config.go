// Package config, iş (job) tanımlarını ve uygulama klasörlerini yönetir.
package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// CurrentVersion, job dosya formatının sürümüdür. İleride format değişirse
// eski dosyaları taşımak (migration) için kullanılır.
const CurrentVersion = 1

// Kimlik doğrulama tipleri.
const (
	AuthSQL     = "sql"
	AuthWindows = "windows"
)

// Retention modları.
const (
	RetentionNone  = "none"
	RetentionDays  = "days"
	RetentionCount = "count"
)

// Zamanlama sıklıkları.
const (
	FrequencyDaily  = "daily"
	FrequencyWeekly = "weekly"
	FrequencyHourly = "hourly"
)

// Zamanlanmış görevin çalışacağı hesap.
const (
	RunAsSystem  = "system"
	RunAsCurrent = "current"
)

// Job, tek bir yedekleme işinin tüm ayarlarını tutar. Her job ayrı bir JSON
// dosyası olarak saklanır.
type Job struct {
	Version    int        `json:"version"`
	Name       string     `json:"name"`
	Connection Connection `json:"connection"`

	// Databases, kullanıcının seçtiği veritabanlarıdır.
	Databases []string `json:"databases"`
	// ExcludedDatabases, seçim yapılırken listede olup seçilmeyen veritabanlarıdır.
	// IncludeNewDatabases açıkken "yeni" veritabanlarını ayırt etmek için kullanılır.
	ExcludedDatabases []string `json:"excludedDatabases"`
	// IncludeNewDatabases açıksa çalışma anında sunucudaki tüm veritabanları
	// (ExcludedDatabases hariç) yedeklenir; sonradan eklenenler de dahil olur.
	IncludeNewDatabases    bool `json:"includeNewDatabases"`
	ExcludeSystemDatabases bool `json:"excludeSystemDatabases"`

	Backup    Backup    `json:"backup"`
	Archive   Archive   `json:"archive"`
	Retention Retention `json:"retention"`
	Schedule  Schedule  `json:"schedule"`
}

// Connection, SQL Server bağlantı bilgileridir. Şifre yalnızca DPAPI ile
// şifrelenmiş olarak (PasswordEnc) saklanır.
type Connection struct {
	Server          string `json:"server"`
	Instance        string `json:"instance"`
	Port            int    `json:"port"` // 0 = varsayılan / instance üzerinden
	Auth            string `json:"auth"`
	User            string `json:"user"`
	PasswordEnc     string `json:"passwordEnc"`
	Encrypt         bool   `json:"encrypt"`
	TrustServerCert bool   `json:"trustServerCert"`
	TimeoutSec      int    `json:"timeoutSec"` // bağlantı zaman aşımı
}

// Backup, yedekleme seçenekleridir.
type Backup struct {
	// ServerPath, SQL Server'ın yedek dosyasını yazacağı klasördür
	// (SQL Server makinesinin bakış açısıyla).
	ServerPath string `json:"serverPath"`
	// AccessPath, bu programın aynı klasöre eriştiği yoldur (ör. UNC).
	// Boşsa ServerPath kullanılır (yerel sunucu).
	AccessPath        string `json:"accessPath"`
	FileNameTemplate  string `json:"fileNameTemplate"`
	Compression       bool   `json:"compression"`
	Checksum          bool   `json:"checksum"`
	CopyOnly          bool   `json:"copyOnly"`
	Verify            bool   `json:"verify"`
	CommandTimeoutMin int    `json:"commandTimeoutMin"` // 0 = sınırsız
}

// Archive, yedek sonrası zip seçenekleridir.
type Archive struct {
	ZipEnabled        bool `json:"zipEnabled"`
	DeleteBakAfterZip bool `json:"deleteBakAfterZip"`
	// ZipPerDatabase açıksa her veritabanı için ayrı zip, kapalıysa tek zip.
	ZipPerDatabase bool `json:"zipPerDatabase"`
}

// Retention, eski yedeklerin temizlenme kuralıdır.
type Retention struct {
	Mode  string `json:"mode"`
	Value int    `json:"value"`
}

// Schedule, Windows zamanlanmış görev ayarlarıdır.
type Schedule struct {
	Frequency     string   `json:"frequency"`
	Time          string   `json:"time"` // "HH:MM"
	Days          []string `json:"days"` // haftalık için: MON, TUE, ...
	IntervalHours int      `json:"intervalHours"`
	RunAs         string   `json:"runAs"`
}

// DefaultFileNameTemplate, yeni işlerde kullanılan dosya adı şablonudur.
const DefaultFileNameTemplate = "{db}_FULL_{yyyyMMdd}_{HHmmss}.bak"

// NewJob, varsayılan değerlerle doldurulmuş yeni bir iş döner.
func NewJob(name string) *Job {
	j := &Job{Name: name}
	j.ApplyDefaults()
	return j
}

// ApplyDefaults, boş bırakılmış alanlara varsayılan değerleri yazar.
func (j *Job) ApplyDefaults() {
	if j.Version == 0 {
		j.Version = CurrentVersion
	}
	if j.Connection.Auth == "" {
		j.Connection.Auth = AuthSQL
	}
	if j.Connection.TimeoutSec == 0 {
		j.Connection.TimeoutSec = 30
	}
	if j.Backup.FileNameTemplate == "" {
		j.Backup.FileNameTemplate = DefaultFileNameTemplate
	}
	if j.Retention.Mode == "" {
		j.Retention.Mode = RetentionNone
	}
	if j.Schedule.Frequency == "" {
		j.Schedule.Frequency = FrequencyDaily
	}
	if j.Schedule.Time == "" {
		j.Schedule.Time = "02:00"
	}
	if j.Schedule.RunAs == "" {
		j.Schedule.RunAs = RunAsSystem
	}
	if j.Databases == nil {
		j.Databases = []string{}
	}
	if j.ExcludedDatabases == nil {
		j.ExcludedDatabases = []string{}
	}
	if j.Schedule.Days == nil {
		j.Schedule.Days = []string{}
	}
}

// EffectiveAccessPath, programın yedek klasörüne erişeceği yolu döner.
func (b Backup) EffectiveAccessPath() string {
	if strings.TrimSpace(b.AccessPath) != "" {
		return b.AccessPath
	}
	return b.ServerPath
}

var (
	jobNameRe = regexp.MustCompile(`^[\p{L}\p{N}_.-]{1,64}$`)
	timeRe    = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	validDays = map[string]bool{"MON": true, "TUE": true, "WED": true, "THU": true, "FRI": true, "SAT": true, "SUN": true}
)

// ValidateName, iş adının dosya ve görev adı olarak kullanılabilir olduğunu kontrol eder.
func ValidateName(name string) error {
	if !jobNameRe.MatchString(name) {
		return errors.New("iş adı 1-64 karakter olmalı; yalnızca harf, rakam, '_', '-' ve '.' içerebilir")
	}
	if strings.Trim(name, ".") == "" {
		return errors.New("iş adı yalnızca noktadan oluşamaz")
	}
	return nil
}

// Validate, işin çalıştırılabilir olup olmadığını kontrol eder. Tüm hataları
// birlikte döner (errors.Join).
func (j *Job) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if err := ValidateName(j.Name); err != nil {
		errs = append(errs, err)
	}

	// Bağlantı
	c := j.Connection
	if strings.TrimSpace(c.Server) == "" {
		add("sunucu adı boş olamaz")
	}
	if c.Port < 0 || c.Port > 65535 {
		add("port 0-65535 aralığında olmalı")
	}
	switch c.Auth {
	case AuthSQL:
		if strings.TrimSpace(c.User) == "" {
			add("SQL kimlik doğrulamasında kullanıcı adı boş olamaz")
		}
		if c.PasswordEnc == "" {
			add("SQL kimlik doğrulamasında şifre girilmeli")
		}
	case AuthWindows:
	default:
		add("geçersiz kimlik doğrulama tipi: %q", c.Auth)
	}
	if c.TimeoutSec < 0 {
		add("bağlantı zaman aşımı negatif olamaz")
	}

	// Veritabanları
	if len(j.Databases) == 0 && !j.IncludeNewDatabases {
		add("en az bir veritabanı seçilmeli")
	}

	// Yedek
	b := j.Backup
	if strings.TrimSpace(b.ServerPath) == "" {
		add("yedek klasörü (sunucu yolu) boş olamaz")
	}
	if err := validateTemplate(b.FileNameTemplate); err != nil {
		errs = append(errs, err)
	}
	if b.CommandTimeoutMin < 0 {
		add("komut zaman aşımı negatif olamaz")
	}

	// Arşiv
	if j.Archive.DeleteBakAfterZip && !j.Archive.ZipEnabled {
		add("zip sonrası .bak silme için zip seçeneği açık olmalı")
	}

	// Retention
	switch j.Retention.Mode {
	case RetentionNone:
	case RetentionDays, RetentionCount:
		if j.Retention.Value <= 0 {
			add("saklama değeri 0'dan büyük olmalı")
		}
	default:
		add("geçersiz saklama modu: %q", j.Retention.Mode)
	}

	// Zamanlama
	s := j.Schedule
	if !timeRe.MatchString(s.Time) {
		add("zamanlama saati SS:DD formatında olmalı (ör. 02:00)")
	}
	switch s.Frequency {
	case FrequencyDaily:
	case FrequencyWeekly:
		if len(s.Days) == 0 {
			add("haftalık zamanlamada en az bir gün seçilmeli")
		}
		for _, d := range s.Days {
			if !validDays[d] {
				add("geçersiz gün: %q", d)
			}
		}
	case FrequencyHourly:
		if s.IntervalHours < 1 || s.IntervalHours > 23 {
			add("saatlik zamanlamada aralık 1-23 saat olmalı")
		}
	default:
		add("geçersiz zamanlama sıklığı: %q", s.Frequency)
	}
	switch s.RunAs {
	case RunAsSystem, RunAsCurrent:
	default:
		add("geçersiz çalışma hesabı: %q", s.RunAs)
	}

	return errors.Join(errs...)
}

func validateTemplate(t string) error {
	if strings.TrimSpace(t) == "" {
		return errors.New("dosya adı şablonu boş olamaz")
	}
	if !strings.Contains(t, "{db}") {
		return errors.New("dosya adı şablonu {db} içermeli")
	}
	if strings.ContainsAny(t, `\/:*?"<>|`) {
		return errors.New(`dosya adı şablonu \ / : * ? " < > | karakterlerini içeremez`)
	}
	return nil
}
