package app

import (
	"errors"
	"os"
	"strings"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/logging"
	"sqlbackup/internal/mssql"
	"sqlbackup/internal/scheduler"
)

// JobSummary, iş listesindeki (Dashboard) bir satırdır.
type JobSummary struct {
	Name                string          `json:"name"`
	Server              string          `json:"server"`
	Databases           []string        `json:"databases"`
	IncludeNewDatabases bool            `json:"includeNewDatabases"`
	Schedule            config.Schedule `json:"schedule"`
	// Running, işin şu anda (bu arayüzde veya başka bir süreçte) çalıştığını belirtir.
	Running bool `json:"running"`
	// LastRun, son çalışma kaydıdır; hiç çalışmadıysa nil.
	LastRun *RunSummary `json:"lastRun"`
	// Task, zamanlanmış görev durumudur; okunamadıysa nil.
	Task *TaskInfo `json:"task"`
	// Errors, işin ayarlarındaki doğrulama hatalarıdır (çalıştırılırsa başarısız olur).
	Errors []string `json:"errors"`
}

// JobList, iş listesidir. Errors, okunamayan iş dosyalarıdır.
type JobList struct {
	Jobs   []JobSummary `json:"jobs"`
	Errors []string     `json:"errors"`
}

// ListJobs, işleri ada göre sıralı, son çalışma ve görev durumuyla döner.
func (a *App) ListJobs() (JobList, error) {
	jobs, loadErr := config.ListJobs()
	list := JobList{Jobs: []JobSummary{}, Errors: errorList(loadErr)}
	for _, j := range jobs {
		s := JobSummary{
			Name:                j.Name,
			Server:              j.Connection.DisplayName(),
			Databases:           j.Databases,
			IncludeNewDatabases: j.IncludeNewDatabases,
			Schedule:            j.Schedule,
			Running:             a.IsJobRunning(j.Name),
			Errors:              errorList(j.Validate()),
		}
		if runs, err := logging.ListRuns(config.LogsDir(), j.Name); err == nil && len(runs) > 0 {
			r := readRunSummary(runs[0])
			s.LastRun = &r
		}
		if ti, err := a.taskInfo(j); err == nil {
			s.Task = &ti
		}
		list.Jobs = append(list.Jobs, s)
	}
	return list, nil
}

// JobDetail, düzenleme ekranı için iştir. Şifre (passwordEnc) frontend'e
// gönderilmez; yalnızca kayıtlı olup olmadığı ve çözülüp çözülemediği bildirilir.
type JobDetail struct {
	Job         *config.Job `json:"job"`
	HasPassword bool        `json:"hasPassword"`
	// PasswordError doluysa kayıtlı şifre çözülemiyor (ör. iş dosyası başka
	// makineden kopyalandı); arayüz şifreyi yeniden istemelidir.
	PasswordError string `json:"passwordError"`
}

// NewJob, varsayılan değerlerle yeni (kaydedilmemiş) bir iş döner. Yedek
// klasörleri ayarlardaki varsayılanlardan gelir (ayarlar okunamazsa boş).
func (a *App) NewJob() *config.Job {
	j := config.NewJob("")
	s, _ := config.LoadSettings()
	j.Backup.ServerPath = strings.TrimSpace(s.DefaultServerPath)
	j.Backup.AccessPath = strings.TrimSpace(s.DefaultAccessPath)
	return j
}

// loadJob, iş adını doğrular ve iş dosyasını okur.
func loadJob(name string) (*config.Job, error) {
	if err := config.ValidateName(name); err != nil {
		return nil, err
	}
	j, err := config.LoadJob(config.JobPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, i18n.Errorf("err.app.jobNotFound", "job", name)
	}
	return j, err
}

// GetJob, işi düzenleme için döner.
func (a *App) GetJob(name string) (JobDetail, error) {
	j, err := loadJob(name)
	if err != nil {
		return JobDetail{}, err
	}
	d := JobDetail{Job: j, HasPassword: j.Connection.PasswordEnc != ""}
	if d.HasPassword {
		if _, err := config.Unprotect(j.Connection.PasswordEnc); err != nil {
			d.PasswordError = err.Error()
		}
	}
	j.Connection.PasswordEnc = ""
	return d, nil
}

// SaveJobRequest, kaydedilecek iştir. OriginalName, düzenlenen işin kayıtlı
// adıdır (yeni işte boş; farklıysa iş yeniden adlandırılır). Şifre yalnızca
// PasswordChanged ise değişir; değilse kayıtlı şifre korunur.
type SaveJobRequest struct {
	Job             config.Job `json:"job"`
	OriginalName    string     `json:"originalName"`
	Password        string     `json:"password"`
	PasswordChanged bool       `json:"passwordChanged"`
}

// SaveJobResult, kaydetme (veya yalnızca doğrulama) sonucudur. Errors doluysa
// iş kaydedilmemiştir.
type SaveJobResult struct {
	Saved  bool     `json:"saved"`
	Errors []string `json:"errors"`
	// FieldErrors, bir alana bağlanabilen hatalardır (alanın JSON yolu, ör.
	// "connection.server" → metinler); bunlar Errors'ta da yer alır.
	FieldErrors map[string][]string `json:"fieldErrors"`
	Warnings    []string            `json:"warnings"`
}

func newSaveJobResult() SaveJobResult {
	return SaveJobResult{Errors: []string{}, FieldErrors: map[string][]string{}, Warnings: []string{}}
}

// addError, hatayı ekler; field boş değilse alana da bağlar.
func (r *SaveJobResult) addError(field, msg string) {
	r.Errors = append(r.Errors, msg)
	if field != "" {
		r.FieldErrors[field] = append(r.FieldErrors[field], msg)
	}
}

// addErrors, errors.Join ile birleştirilmiş hataları açar; *config.FieldError
// olanları alanlarına bağlar.
func (r *SaveJobResult) addErrors(field string, err error) {
	if fe, ok := err.(*config.FieldError); ok {
		r.addErrors(fe.Field, fe.Err)
		return
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		if _, isText := err.(*i18n.Error); !isText {
			for _, e := range j.Unwrap() {
				r.addErrors(field, e)
			}
			return
		}
	}
	for _, msg := range errorList(err) {
		r.addError(field, msg)
	}
}

// ValidateJob, işi kaydetmeden SaveJob'un doğrulamasından geçirir; arayüz
// başarısız bir kaydetmeden sonra alan hatalarını canlı güncellemek için
// kullanır. Saved her zaman false'tur.
func (a *App) ValidateJob(req SaveJobRequest) (SaveJobResult, error) {
	res, _, _, err := a.validateSave(req, false)
	return res, err
}

// validateSave, kaydedilecek işi hazırlar (ad, varsayılanlar, şifre) ve
// doğrular; old, düzenlenen işin kayıtlı hâlidir (yeni işte nil). encrypt
// false ise yeni şifre şifrelenmez, yalnızca var sayılır (doğrulama için).
func (a *App) validateSave(req SaveJobRequest, encrypt bool) (res SaveJobResult, j config.Job, old *config.Job, err error) {
	res = newSaveJobResult()
	j = req.Job
	j.Name = strings.TrimSpace(j.Name)
	j.ApplyDefaults()

	if orig := strings.TrimSpace(req.OriginalName); orig != "" {
		if old, err = loadJob(orig); err != nil {
			return res, j, nil, err
		}
	}

	// Şifre: frontend passwordEnc'i hiç görmez; burada belirlenir.
	j.Connection.PasswordEnc = ""
	if j.Connection.Auth == config.AuthSQL {
		switch {
		case req.PasswordChanged && req.Password != "" && !encrypt:
			j.Connection.PasswordEnc = "-"
		case req.PasswordChanged && req.Password != "":
			if err = j.Connection.SetPassword(req.Password); err != nil {
				return res, j, old, err
			}
		case !req.PasswordChanged && old != nil:
			j.Connection.PasswordEnc = old.Connection.PasswordEnc
		}
	}

	res.addErrors("", j.Validate())
	if err := backup.ValidateTemplate(j.Backup.FileNameTemplate); err != nil {
		res.addError("backup.fileNameTemplate", err.Error())
	}
	// Dosya sistemi büyük/küçük harf duyarsız: yalnızca harf değişikliği çakışma değildir.
	if old == nil || !strings.EqualFold(j.Name, old.Name) {
		if config.ValidateName(j.Name) == nil && fileExists(config.JobPath(j.Name)) {
			res.addError("name", i18n.T("err.app.jobExists", "job", j.Name))
		}
	}
	if old != nil && j.Name != old.Name && a.IsJobRunning(old.Name) {
		res.addError("name", i18n.T("err.app.renameRunning", "job", old.Name))
	}
	return res, j, old, nil
}

// SaveJob, işi doğrular ve kaydeder. Geçersiz iş kaydedilmez (Errors).
// Yeniden adlandırmada eski dosya silinir; eski adın görevi ve .bat'ı
// taşınmaz, uyarı verilir.
func (a *App) SaveJob(req SaveJobRequest) (SaveJobResult, error) {
	res, j, old, err := a.validateSave(req, true)
	if err != nil || len(res.Errors) > 0 {
		return res, err
	}

	renamed := old != nil && j.Name != old.Name
	if renamed && strings.EqualFold(j.Name, old.Name) {
		if err := os.Rename(config.JobPath(old.Name), config.JobPath(j.Name)); err != nil {
			return res, err
		}
	}
	if err := config.SaveJob(config.JobPath(j.Name), &j); err != nil {
		return res, err
	}
	res.Saved = true

	// Yalnızca harf değişikliğinde görev ve .bat çalışmaya devam eder (Windows
	// yolları ve görev adları harf duyarsız); gerçek ad değişikliği onları bozar.
	if renamed && !strings.EqualFold(j.Name, old.Name) {
		if err := config.DeleteJob(old.Name); err != nil {
			res.Warnings = append(res.Warnings, err.Error())
		}
		if st, err := a.tasks.Query(old.Name); err == nil && st.Exists {
			res.Warnings = append(res.Warnings, i18n.T("warn.app.renamedTask", "path", st.Path))
		}
		if p := scheduler.DefaultBatchPath(old.Name); fileExists(p) {
			res.Warnings = append(res.Warnings, i18n.T("warn.app.renamedBatch", "path", p))
		}
	} else if ti, err := a.taskInfo(&j); err == nil && ti.Outdated {
		res.Warnings = append(res.Warnings, i18n.T("warn.app.taskOutdated", "path", ti.Status.Path))
	}
	return res, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// DuplicateJob, işi (şifresi ve zamanlamasıyla) yeni adla kopyalar. Görev
// oluşturulmaz.
func (a *App) DuplicateJob(name, newName string) (SaveJobResult, error) {
	res := newSaveJobResult()
	j, err := loadJob(name)
	if err != nil {
		return res, err
	}
	j.Name = strings.TrimSpace(newName)
	if err := config.ValidateName(j.Name); err != nil {
		res.addError("name", err.Error())
		return res, nil
	}
	if fileExists(config.JobPath(j.Name)) {
		res.addError("name", i18n.T("err.app.jobExists", "job", j.Name))
		return res, nil
	}
	if err := config.SaveJob(config.JobPath(j.Name), j); err != nil {
		return res, err
	}
	res.Saved = true
	return res, nil
}

// DeleteJob, işi siler: önce zamanlanmış görevi (gerekirse yönetici
// yetkisiyle), sonra varsayılan .bat'ı ve iş dosyasını. Loglar korunur.
// Görev silinemezse hiçbir şey silinmez.
func (a *App) DeleteJob(name string) error {
	if _, err := loadJob(name); err != nil {
		return err
	}
	if a.IsJobRunning(name) {
		return i18n.Errorf("err.app.deleteRunning", "job", name)
	}
	if st, err := a.tasks.Query(name); err != nil {
		return err
	} else if st.Exists {
		if _, err := a.withElevation(func() error { return a.tasks.Delete(name) }, "delete", name); err != nil {
			return err
		}
	}
	if err := os.Remove(scheduler.DefaultBatchPath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return config.DeleteJob(name)
}

// ConnectionRequest, bağlantı testi ve veritabanı listesi isteğidir.
// PasswordChanged değilse ve StoredJob doluysa o işin kayıtlı şifresi kullanılır.
type ConnectionRequest struct {
	Connection      config.Connection `json:"connection"`
	Password        string            `json:"password"`
	PasswordChanged bool              `json:"passwordChanged"`
	StoredJob       string            `json:"storedJob"`
}

// ConnectionInfo, bağlantı testinin sonucudur.
type ConnectionInfo struct {
	Server              mssql.ServerInfo `json:"server"`
	SupportsCompression bool             `json:"supportsCompression"`
	ProtocolLabel       string           `json:"protocolLabel"`
}

func (a *App) password(req ConnectionRequest) (string, error) {
	if req.Connection.Auth != config.AuthSQL {
		return "", nil
	}
	if req.PasswordChanged || strings.TrimSpace(req.StoredJob) == "" {
		return req.Password, nil
	}
	j, err := loadJob(strings.TrimSpace(req.StoredJob))
	if err != nil {
		return "", err
	}
	if j.Connection.PasswordEnc == "" {
		return "", nil
	}
	p, err := config.Unprotect(j.Connection.PasswordEnc)
	if err != nil {
		return "", i18n.Errorf("err.backup.password", "err", err)
	}
	return p, nil
}

func (a *App) open(req ConnectionRequest) (backup.Server, error) {
	pw, err := a.password(req)
	if err != nil {
		return nil, err
	}
	c := req.Connection
	if c.TimeoutSec == 0 {
		c.TimeoutSec = 30
	}
	if a.connect != nil {
		return a.connect(a.context(), c, pw)
	}
	cl, err := mssql.Open(a.context(), c, pw)
	if err != nil {
		return nil, err
	}
	return cl, nil
}

// TestConnection, bağlantıyı sınar ve sunucu bilgilerini döner.
func (a *App) TestConnection(req ConnectionRequest) (ConnectionInfo, error) {
	srv, err := a.open(req)
	if err != nil {
		return ConnectionInfo{}, err
	}
	defer srv.Close()
	info := srv.Info()
	return ConnectionInfo{Server: info, SupportsCompression: info.SupportsCompression(),
		ProtocolLabel: mssql.ProtocolLabel(info.Protocol)}, nil
}

// ListDatabases, sunucudaki veritabanlarını listeler.
func (a *App) ListDatabases(req ConnectionRequest) ([]mssql.Database, error) {
	srv, err := a.open(req)
	if err != nil {
		return nil, err
	}
	defer srv.Close()
	return srv.ListDatabases(a.context())
}

// CheckFolderAccess, programın klasöre okuma/yazma/silme erişimini dener
// (zip ve saklama için erişim yolu).
func (a *App) CheckFolderAccess(path string) error {
	return backup.CheckAccess(path)
}

// PreviewFileName, şablondan db için örnek dosya adı üretir (şu anki zamanla).
// server boşsa bağlantı ayarlarındaki ad kullanılır.
func (a *App) PreviewFileName(job config.Job, db, server string) (string, error) {
	return backup.PreviewFileName(&job, db, server, a.now())
}
