package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
	"sqlbackup/internal/logging"
)

// maxLogBytes, ReadLog'un döndüğü en fazla bayttır (dosyanın sonu).
const maxLogBytes = 2 << 20

// RunSummary, çalışma geçmişi tablosunun bir satırıdır.
type RunSummary struct {
	Path           string    `json:"path"`
	Job            string    `json:"job"`
	Start          time.Time `json:"start"`
	ElapsedMs      int64     `json:"elapsedMs"`
	ExitCode       int       `json:"exitCode"`
	Succeeded      int       `json:"succeeded"`
	Failed         int       `json:"failed"`
	Skipped        int       `json:"skipped"`
	Error          string    `json:"error"`
	AlreadyRunning bool      `json:"alreadyRunning"`
	// ReadError doluysa kayıt dosyası okunamadı; diğer alanlar (Path, Job, Start hariç) boştur.
	ReadError string `json:"readError"`
}

func readRunSummary(ri logging.RunInfo) RunSummary {
	s := RunSummary{Path: ri.Path, Job: ri.Job, Start: ri.Start}
	var r backup.Result
	if err := logging.ReadRun(ri.Path, &r); err != nil {
		s.ReadError = err.Error()
		return s
	}
	s.Succeeded, s.Failed, s.Skipped = r.Counts()
	s.ElapsedMs, s.ExitCode, s.Error, s.AlreadyRunning = r.ElapsedMs, r.ExitCode, r.Error, r.AlreadyRunning
	if r.Job != "" {
		s.Job = r.Job
	}
	return s
}

// ListRuns, çalışma kayıtlarını en yeniden eskiye döner; job boşsa tüm
// işler. limit > 0 ise en fazla o kadar kayıt okunur.
func (a *App) ListRuns(job string, limit int) ([]RunSummary, error) {
	runs, err := logging.ListRuns(config.LogsDir(), strings.TrimSpace(job))
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	out := make([]RunSummary, len(runs))
	for i, r := range runs {
		out[i] = readRunSummary(r)
	}
	return out, nil
}

// insideDir, path'in dir altında olup olmadığını döner. Frontend'den gelen
// yollar yalnızca log klasöründeki dosyaları okuyabilir.
func insideDir(path, dir string) bool {
	p, err1 := filepath.Abs(path)
	d, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(d, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// ReadRun, bir çalışma kaydını (ListRuns yolu) tam olarak döner.
func (a *App) ReadRun(path string) (*backup.Result, error) {
	if !insideDir(path, logging.RunsDir(config.LogsDir())) {
		return nil, i18n.Errorf("err.app.pathOutside", "path", path)
	}
	var r backup.Result
	if err := logging.ReadRun(path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListLogs, log dosyalarını en yeniden eskiye döner; job boşsa tüm işler.
func (a *App) ListLogs(job string) ([]logging.LogInfo, error) {
	logs, err := logging.ListLogs(config.LogsDir(), strings.TrimSpace(job))
	if logs == nil {
		logs = []logging.LogInfo{}
	}
	return logs, err
}

// LogText, bir log dosyasının içeriğidir.
type LogText struct {
	Text string `json:"text"`
	Size int64  `json:"size"`
	// Truncated, dosya büyük olduğu için yalnızca sonunun döndüğünü belirtir.
	Truncated bool `json:"truncated"`
}

// ReadLog, log dosyasının (ListLogs yolu) içeriğini döner; büyükse son 2 MB.
func (a *App) ReadLog(path string) (LogText, error) {
	if !insideDir(path, config.LogsDir()) {
		return LogText{}, i18n.Errorf("err.app.pathOutside", "path", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return LogText{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return LogText{}, err
	}
	lt := LogText{Size: info.Size()}
	if lt.Size > maxLogBytes {
		lt.Truncated = true
		if _, err := f.Seek(-maxLogBytes, io.SeekEnd); err != nil {
			return LogText{}, err
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return LogText{}, err
	}
	if lt.Truncated {
		// Yarım kalan ilk satır atılır.
		if i := strings.IndexByte(string(data), '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	lt.Text = string(data)
	return lt, nil
}

// SelectFolder, klasör seçme diyaloğunu açar; iptalde boş döner.
func (a *App) SelectFolder(title, defaultDir string) (string, error) {
	return runtime.OpenDirectoryDialog(a.context(), runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDir,
	})
}

// SelectBatchFile, .bat kaydetme diyaloğunu açar; iptalde boş döner.
func (a *App) SelectBatchFile(name string) (string, error) {
	return runtime.SaveFileDialog(a.context(), runtime.SaveDialogOptions{
		DefaultDirectory: config.JobsDir(),
		DefaultFilename:  name + ".bat",
		Filters:          []runtime.FileFilter{{DisplayName: i18n.T("common.batchFiles") + " (*.bat)", Pattern: "*.bat"}},
	})
}
