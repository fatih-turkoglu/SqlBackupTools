package scheduler

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
)

// Görevler Görev Zamanlayıcı COM API'si (Schedule.Service) ile yönetilir.
// schtasks.exe kullanılmaz: /Query çıktısı yerelleştirilmiştir (başlıklar,
// tarih biçimi) ve /TR 261 karakterle sınırlıdır. COM'da durum, tarihler ve
// hata kodları (E_ACCESSDENIED) tipli gelir.

var (
	// ErrAccessDenied, işlemin yönetici yetkisi gerektirdiğini belirtir
	// (SYSTEM görevi oluşturma, yönetici tarafından oluşturulmuş görevi silme vb.).
	ErrAccessDenied = i18n.Errorf("err.scheduler.accessDenied")
	// ErrTaskNotFound, görevin (veya klasörünün) bulunmadığını belirtir.
	ErrTaskNotFound = i18n.Errorf("err.scheduler.notFound")
)

// HRESULT değerleri.
const (
	hrSFalse         = 0x00000001
	hrFileNotFound   = 0x80070002
	hrPathNotFound   = 0x80070003
	hrAccessDenied   = 0x80070005
	hrDispException  = 0x80020009
	hrRPCChangedMode = 0x80010106
)

// Görev Zamanlayıcı sabitleri.
const (
	taskCreateOrUpdate        = 6
	taskLogonInteractiveToken = 3
	taskLogonServiceAccount   = 5
	taskEnumHidden            = 1
)

// Görev sonuç kodları (LastTaskResult).
const (
	// ResultNotRun, görevin henüz hiç çalışmadığını belirtir (SCHED_S_TASK_HAS_NOT_RUN).
	ResultNotRun = 0x00041303
	// ResultRunning, görevin şu anda çalıştığını belirtir (SCHED_S_TASK_RUNNING).
	ResultRunning = 0x00041301
)

// Görev durumları (TaskStatus.State).
const (
	StateUnknown  = "unknown"
	StateDisabled = "disabled"
	StateQueued   = "queued"
	StateReady    = "ready"
	StateRunning  = "running"
)

// TaskStatus, kayıtlı görevin durumudur.
type TaskStatus struct {
	Exists  bool   `json:"exists"`
	Path    string `json:"path"`
	State   string `json:"state,omitempty"`
	Enabled bool   `json:"enabled"`
	// LastRun, görev hiç çalışmadıysa nil'dir.
	LastRun *time.Time `json:"lastRun,omitempty"`
	// LastResult, son çalışmanın çıkış kodudur (programın 0-3 kodları) veya
	// Görev Zamanlayıcı kodudur (ResultNotRun, ResultRunning, HRESULT).
	LastResult uint32 `json:"lastResult"`
	// NextRun, planlanmış çalışma yoksa (devre dışı) nil'dir.
	NextRun    *time.Time     `json:"nextRun,omitempty"`
	Definition TaskDefinition `json:"definition"`
}

// hresult, COM hatasının gerçek HRESULT'unu döner (IDispatch istisnasında
// EXCEPINFO içindeki kod).
func hresult(err error) uint32 {
	var oe *ole.OleError
	if !errors.As(err, &oe) {
		return 0
	}
	code := uint32(oe.Code())
	if code == hrDispException {
		if ei, ok := oe.SubError().(ole.EXCEPINFO); ok && ei.SCODE() != 0 {
			return ei.SCODE()
		}
	}
	return code
}

// comError, COM hatasını anlaşılır hataya çevirir: erişim reddi ve
// bulunamadı sentinel hatalarına, diğerleri anahtarlı hataya dönüşür.
func comError(key string, err error) error {
	switch hresult(err) {
	case hrAccessDenied:
		return ErrAccessDenied
	case hrFileNotFound, hrPathNotFound:
		return ErrTaskNotFound
	}
	return i18n.Errorf(key, "err", describe(err))
}

func describe(err error) string {
	var oe *ole.OleError
	if !errors.As(err, &oe) {
		return err.Error()
	}
	msg := strings.TrimSpace(oe.Description())
	if msg == "" {
		msg = strings.TrimSpace(oe.String())
	}
	return fmt.Sprintf("%s (0x%08X)", msg, hresult(err))
}

// withService, COM'u bu iş parçacığında başlatır, Schedule.Service'e
// bağlanır ve fn'i çalıştırır. COM nesneleri iş parçacığına bağlı olduğu
// için tüm çağrılar tek kilitli iş parçacığında yapılır.
func withService(fn func(svc *ole.IDispatch) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		switch hresult(err) {
		case hrSFalse: // zaten başlatılmış; yine de dengelenmeli
			defer ole.CoUninitialize()
		case hrRPCChangedMode: // başka kipte başlatılmış; kullanılabilir
		default:
			return i18n.Errorf("err.scheduler.connect", "err", describe(err))
		}
	} else {
		defer ole.CoUninitialize()
	}

	unk, err := oleutil.CreateObject("Schedule.Service")
	if err != nil {
		return i18n.Errorf("err.scheduler.connect", "err", describe(err))
	}
	defer unk.Release()
	svc, err := unk.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return i18n.Errorf("err.scheduler.connect", "err", describe(err))
	}
	defer svc.Release()
	if _, err := oleutil.CallMethod(svc, "Connect"); err != nil {
		return i18n.Errorf("err.scheduler.connect", "err", describe(err))
	}
	return fn(svc)
}

// callDispatch, IDispatch dönen bir yöntemi çağırır. Dönen nesne çağıran
// tarafından Release edilmelidir.
func callDispatch(d *ole.IDispatch, name string, args ...any) (*ole.IDispatch, error) {
	v, err := oleutil.CallMethod(d, name, args...)
	if err != nil {
		return nil, err
	}
	return v.ToIDispatch(), nil
}

func getFolder(svc *ole.IDispatch, path string) (*ole.IDispatch, error) {
	return callDispatch(svc, "GetFolder", path)
}

// ensureFolder, görev klasörünü döner; yoksa oluşturur.
func ensureFolder(svc *ole.IDispatch) (*ole.IDispatch, error) {
	f, err := getFolder(svc, TaskFolder)
	if err == nil {
		return f, nil
	}
	if c := hresult(err); c != hrFileNotFound && c != hrPathNotFound {
		return nil, err
	}
	root, err := getFolder(svc, `\`)
	if err != nil {
		return nil, err
	}
	defer root.Release()
	return callDispatch(root, "CreateFolder", strings.TrimPrefix(TaskFolder, `\`), nil)
}

// CreateTask, görevi (TaskXML çıktısı) kaydeder; varsa günceller. runAs,
// XML'deki hesapla aynı olmalıdır (config.RunAsSystem / RunAsCurrent).
// SYSTEM görevinde exe'nin yönetici olmayanlarca değiştirilemediği denetlenir
// (bkz. CheckExeSecurity); CLI, arayüz ve yükseltilmiş süreç buradan geçer.
func CreateTask(name, taskXML, runAs string) error {
	logon := taskLogonInteractiveToken
	if runAs == config.RunAsSystem {
		logon = taskLogonServiceAccount
		def, err := ParseTaskXML(taskXML)
		if err != nil {
			return err
		}
		if def.Command == "" {
			return i18n.Errorf("err.scheduler.badPath", "path", def.Command)
		}
		if err := CheckExeSecurity(def.Command); err != nil {
			return err
		}
	}
	return withService(func(svc *ole.IDispatch) error {
		folder, err := ensureFolder(svc)
		if err != nil {
			return comError("err.scheduler.create", err)
		}
		defer folder.Release()
		// Hesap bilgisi (userId, password) verilmez; XML'deki Principal kullanılır.
		t, err := callDispatch(folder, "RegisterTask", name, taskXML, taskCreateOrUpdate, nil, nil, logon, nil)
		if err != nil {
			return comError("err.scheduler.create", err)
		}
		t.Release()
		return nil
	})
}

// DeleteTask, görevi siler. Klasörde başka görev kalmadıysa klasör de silinir.
func DeleteTask(name string) error {
	return withService(func(svc *ole.IDispatch) error {
		folder, err := getFolder(svc, TaskFolder)
		if err != nil {
			return comError("err.scheduler.delete", err)
		}
		defer folder.Release()
		if _, err := oleutil.CallMethod(folder, "DeleteTask", name, 0); err != nil {
			return comError("err.scheduler.delete", err)
		}
		deleteFolderIfEmpty(svc, folder)
		return nil
	})
}

// deleteFolderIfEmpty, görev klasörü boşsa siler; hatalar yok sayılır.
func deleteFolderIfEmpty(svc, folder *ole.IDispatch) {
	count := func(method string, arg any) int {
		c, err := callDispatch(folder, method, arg)
		if err != nil {
			return -1
		}
		defer c.Release()
		v, err := oleutil.GetProperty(c, "Count")
		if err != nil {
			return -1
		}
		return int(v.Val)
	}
	if count("GetTasks", taskEnumHidden) != 0 || count("GetFolders", 0) != 0 {
		return
	}
	root, err := getFolder(svc, `\`)
	if err != nil {
		return
	}
	defer root.Release()
	oleutil.CallMethod(root, "DeleteFolder", strings.TrimPrefix(TaskFolder, `\`), 0)
}

// RunTask, görevi hemen başlatır (Görev Zamanlayıcı üzerinden, görevin hesabıyla).
func RunTask(name string) error {
	return withService(func(svc *ole.IDispatch) error {
		t, err := getTask(svc, name)
		if err != nil {
			return comError("err.scheduler.run", err)
		}
		defer t.Release()
		rt, err := callDispatch(t, "Run", nil)
		if err != nil {
			return comError("err.scheduler.run", err)
		}
		rt.Release()
		return nil
	})
}

func getTask(svc *ole.IDispatch, name string) (*ole.IDispatch, error) {
	folder, err := getFolder(svc, TaskFolder)
	if err != nil {
		return nil, err
	}
	defer folder.Release()
	return callDispatch(folder, "GetTask", name)
}

// QueryTask, görevin durumunu döner. Görev yoksa Exists=false ve nil hata döner.
func QueryTask(name string) (TaskStatus, error) {
	st := TaskStatus{Path: TaskPath(name)}
	err := withService(func(svc *ole.IDispatch) error {
		t, err := getTask(svc, name)
		if err != nil {
			return comError("err.scheduler.query", err)
		}
		defer t.Release()
		st.Exists = true

		prop := func(n string) (*ole.VARIANT, error) {
			v, err := oleutil.GetProperty(t, n)
			if err != nil {
				return nil, comError("err.scheduler.query", err)
			}
			return v, nil
		}
		v, err := prop("State")
		if err != nil {
			return err
		}
		st.State = stateName(v.Val)
		if v, err = prop("Enabled"); err != nil {
			return err
		}
		st.Enabled, _ = v.Value().(bool)
		if v, err = prop("LastTaskResult"); err != nil {
			return err
		}
		st.LastResult = uint32(v.Val)
		// Tarihler yalnızca okunabildiğinde doldurulur; bazı hesaplar için
		// (ör. başka kullanıcının görevi) hata verebilir.
		if v, err := prop("LastRunTime"); err == nil && st.LastResult != ResultNotRun {
			st.LastRun = variantTime(v)
		}
		if v, err := prop("NextRunTime"); err == nil {
			st.NextRun = variantTime(v)
		}
		if v, err := prop("Xml"); err == nil {
			if def, err := ParseTaskXML(v.ToString()); err == nil {
				def.User = accountName(def.User)
				st.Definition = def
			}
			v.Clear()
		}
		return nil
	})
	if errors.Is(err, ErrTaskNotFound) {
		return TaskStatus{Path: TaskPath(name)}, nil
	}
	return st, err
}

// accountName, Görev Zamanlayıcı'nın kaydederken SID'e çevirdiği hesabı
// DOMAIN\ad biçimine döndürür; çözülemezse olduğu gibi bırakır.
func accountName(s string) string {
	if !strings.HasPrefix(s, "S-1-") {
		return s
	}
	sid, err := windows.StringToSid(s)
	if err != nil {
		return s
	}
	account, domain, _, err := sid.LookupAccount("")
	if err != nil {
		return s
	}
	if domain == "" {
		return account
	}
	return domain + `\` + account
}

func stateName(v int64) string {
	switch v {
	case 1:
		return StateDisabled
	case 2:
		return StateQueued
	case 3:
		return StateReady
	case 4:
		return StateRunning
	}
	return StateUnknown
}

// variantTime, VT_DATE değerini yerel saate çevirir. go-ole yerel saati UTC
// olarak etiketler; Görev Zamanlayıcı "hiç" için 1899/1999 tarihleri döner.
func variantTime(v *ole.VARIANT) *time.Time {
	t, ok := v.Value().(time.Time)
	if !ok || t.Year() < 2000 {
		return nil
	}
	l := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
	return &l
}
