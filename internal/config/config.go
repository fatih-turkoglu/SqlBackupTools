// Package config, iş (job) tanımlarını ve uygulama klasörlerini yönetir.
package config

import (
	"errors"
	"regexp"
	"strings"

	"sqlbackup/internal/i18n"
)

// CurrentVersion, job dosya formatının sürümüdür. İleride format değişirse
// eski dosyaları taşımak (migration) için kullanılır.
const CurrentVersion = 1

// Kimlik doğrulama tipleri.
const (
	AuthSQL     = "sql"
	AuthWindows = "windows"
)

// Bağlantı protokolleri. Boş değer (ProtocolAuto) sürücünün sırayla
// denemesi anlamına gelir.
const (
	ProtocolAuto = ""
	ProtocolTCP  = "tcp"
	ProtocolNP   = "np"  // Named Pipes
	ProtocolLPC  = "lpc" // Shared Memory (yalnızca yerel)
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
	Port            int    `json:"port"`     // 0 = varsayılan / instance üzerinden
	Protocol        string `json:"protocol"` // "", tcp, np, lpc
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

// DisplayName, sunucunun ayarlardaki adıdır (HOST\INSTANCE). Çalışmada
// bağlandıktan sonra sunucunun bildirdiği ad kullanılır.
func (c Connection) DisplayName() string {
	s := strings.TrimSpace(c.Server)
	if c.Instance != "" && !strings.Contains(s, `\`) {
		s += `\` + c.Instance
	}
	return s
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
		return i18n.Errorf("err.config.nameInvalid")
	}
	if strings.Trim(name, ".") == "" {
		return i18n.Errorf("err.config.nameDots")
	}
	return nil
}

// Validate, işin çalıştırılabilir olup olmadığını kontrol eder. Tüm hataları
// birlikte döner (errors.Join).
func (j *Job) Validate() error {
	var errs []error
	add := func(key string, a ...any) { errs = append(errs, i18n.Errorf(key, a...)) }

	if err := ValidateName(j.Name); err != nil {
		errs = append(errs, err)
	}

	// Bağlantı
	c := j.Connection
	if strings.TrimSpace(c.Server) == "" {
		add("err.config.serverEmpty")
	}
	if c.Port < 0 || c.Port > 65535 {
		add("err.config.portRange")
	}
	switch c.Protocol {
	case ProtocolAuto, ProtocolTCP:
	case ProtocolNP, ProtocolLPC:
		if c.Port > 0 {
			add("err.config.portTCPOnly")
		}
	default:
		add("err.config.protocolInvalid", "value", c.Protocol)
	}
	switch c.Auth {
	case AuthSQL:
		if strings.TrimSpace(c.User) == "" {
			add("err.config.userEmpty")
		}
		if c.PasswordEnc == "" {
			add("err.config.passwordEmpty")
		}
	case AuthWindows:
	default:
		add("err.config.authInvalid", "value", c.Auth)
	}
	if c.TimeoutSec < 0 {
		add("err.config.timeoutNegative")
	}

	// Veritabanları
	if len(j.Databases) == 0 && !j.IncludeNewDatabases {
		add("err.config.noDatabase")
	}

	// Yedek
	b := j.Backup
	if strings.TrimSpace(b.ServerPath) == "" {
		add("err.config.serverPathEmpty")
	}
	if err := ValidateTemplate(b.FileNameTemplate); err != nil {
		errs = append(errs, err)
	}
	if b.CommandTimeoutMin < 0 {
		add("err.config.commandTimeoutNegative")
	}

	// Arşiv
	if j.Archive.DeleteBakAfterZip && !j.Archive.ZipEnabled {
		add("err.config.deleteBakNeedsZip")
	}

	// Retention
	switch j.Retention.Mode {
	case RetentionNone:
	case RetentionDays, RetentionCount:
		if j.Retention.Value <= 0 {
			add("err.config.retentionValue")
		}
	default:
		add("err.config.retentionModeInvalid", "value", j.Retention.Mode)
	}

	// Zamanlama
	s := j.Schedule
	if !timeRe.MatchString(s.Time) {
		add("err.config.scheduleTime")
	}
	switch s.Frequency {
	case FrequencyDaily:
	case FrequencyWeekly:
		if len(s.Days) == 0 {
			add("err.config.scheduleNoDays")
		}
		for _, d := range s.Days {
			if !validDays[d] {
				add("err.config.scheduleDayInvalid", "value", d)
			}
		}
	case FrequencyHourly:
		if s.IntervalHours < 1 || s.IntervalHours > 23 {
			add("err.config.scheduleInterval")
		}
	default:
		add("err.config.scheduleFrequencyInvalid", "value", s.Frequency)
	}
	switch s.RunAs {
	case RunAsSystem, RunAsCurrent:
	default:
		add("err.config.runAsInvalid", "value", s.RunAs)
	}

	return errors.Join(errs...)
}

// ValidateTemplate, dosya adı şablonunun temel kurallarını ({db} içermesi,
// geçersiz karakter olmaması) kontrol eder; yer tutucuları backup paketi doğrular.
func ValidateTemplate(t string) error {
	if strings.TrimSpace(t) == "" {
		return i18n.Errorf("err.config.templateEmpty")
	}
	if !strings.Contains(t, "{db}") {
		return i18n.Errorf("err.config.templateNoDb")
	}
	if strings.ContainsAny(t, `\/:*?"<>|`) {
		return i18n.Errorf("err.config.templateInvalidChars")
	}
	return nil
}
