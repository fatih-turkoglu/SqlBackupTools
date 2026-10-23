// Package logging, dosya tabanlı log ve çalışma geçmişi kayıtlarını yönetir.
//
// Klasör düzeni (logsDir genelde config.LogsDir()):
//
//	logs\<iş>_<yyyyMMdd>.log                  günlük metin log (çalışma başlangıç gününe göre)
//	logs\runs\<iş>_<yyyyMMdd_HHmmss>.json     her çalışmanın sonuç kaydı
//
// Paket backup ve config paketlerinden bağımsızdır; klasör parametre olarak
// verilir, sonuç kaydı herhangi bir JSON'a çevrilebilir değer olabilir.
package logging

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultKeepDays, log ve çalışma kayıtlarının varsayılan saklama süresidir.
const DefaultKeepDays = 30

const (
	logDateLayout = "20060102"
	runTimeLayout = "20060102_150405"
)

var (
	logFileRe = regexp.MustCompile(`^(.+)_(\d{8})\.log$`)
	runFileRe = regexp.MustCompile(`^(.+)_(\d{8}_\d{6})(?:_(\d+))?\.json$`)
)

// RunsDir, çalışma kayıtlarının klasörüdür.
func RunsDir(logsDir string) string { return filepath.Join(logsDir, "runs") }

// JobLog, bir işin açık günlük log dosyasıdır.
type JobLog struct {
	Path string
	f    *os.File
}

// OpenJobLog, işin start gününe ait log dosyasını ekleme kipinde açar
// (yoksa oluşturur). Gece yarısını geçen bir çalışma başladığı günün
// dosyasına yazmaya devam eder.
func OpenJobLog(logsDir, job string, start time.Time) (*JobLog, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("log klasörü oluşturulamadı: %w", err)
	}
	p := filepath.Join(logsDir, job+"_"+start.Format(logDateLayout)+".log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("log dosyası açılamadı: %w", err)
	}
	return &JobLog{Path: p, f: f}, nil
}

// Handler, log dosyasına metin biçiminde yazan bir slog işleyicisi döner.
// Her kayıt tek bir Write ile yazılır; aynı dosyaya başka bir süreç de
// (ör. "zaten çalışıyor" uyarısı) ekleme yapabilir.
func (l *JobLog) Handler(level slog.Leveler) slog.Handler {
	return slog.NewTextHandler(l.f, &slog.HandlerOptions{Level: level})
}

// Close, log dosyasını kapatır.
func (l *JobLog) Close() error { return l.f.Close() }

// LogInfo, bir log dosyasının bilgisidir.
type LogInfo struct {
	Path string    `json:"path"`
	Job  string    `json:"job"`
	Date time.Time `json:"date"`
	Size int64     `json:"size"`
}

// ListLogs, log dosyalarını en yeniden eskiye sıralı döner. job boşsa tüm
// işler listelenir. Adı biçime uymayan dosyalar atlanır.
func ListLogs(logsDir, job string) ([]LogInfo, error) {
	entries, err := readDir(logsDir)
	if err != nil {
		return nil, err
	}
	var out []LogInfo
	for _, e := range entries {
		m := logFileRe.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() || (job != "" && !strings.EqualFold(m[1], job)) {
			continue
		}
		d, err := time.ParseInLocation(logDateLayout, m[2], time.Local)
		if err != nil {
			continue
		}
		li := LogInfo{Path: filepath.Join(logsDir, e.Name()), Job: m[1], Date: d}
		if info, err := e.Info(); err == nil {
			li.Size = info.Size()
		}
		out = append(out, li)
	}
	sort.SliceStable(out, func(i, k int) bool {
		if !out[i].Date.Equal(out[k].Date) {
			return out[i].Date.After(out[k].Date)
		}
		return out[i].Job < out[k].Job
	})
	return out, nil
}

// SaveRun, çalışma sonucunu runs klasörüne JSON olarak atomik biçimde
// (geçici dosya + rename) yazar ve dosya yolunu döner. Aynı saniyede
// başlamış başka bir kayıt varsa adın sonuna _2, _3 eklenir.
func SaveRun(logsDir, job string, start time.Time, v any) (string, error) {
	dir := RunsDir(logsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("çalışma kaydı klasörü oluşturulamadı: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".run-*.tmp")
	if err != nil {
		return "", fmt.Errorf("çalışma kaydı yazılamadı: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // taşıma başarılıysa dosya zaten yok
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return "", fmt.Errorf("çalışma kaydı yazılamadı: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("çalışma kaydı yazılamadı: %w", err)
	}

	base := job + "_" + start.Format(runTimeLayout)
	for i := 1; ; i++ {
		name := base + ".json"
		if i > 1 {
			name = base + "_" + strconv.Itoa(i) + ".json"
		}
		p := filepath.Join(dir, name)
		// Ad O_EXCL ile ayrılır (eşzamanlı yazan başka süreç dahil var olan
		// bir kaydın üzerine yazılmaz), sonra geçici dosya üzerine taşınır.
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("çalışma kaydı yazılamadı: %w", err)
		}
		f.Close()
		if err := os.Rename(tmpName, p); err != nil {
			os.Remove(p)
			return "", fmt.Errorf("çalışma kaydı yazılamadı: %w", err)
		}
		return p, nil
	}
}

// RunInfo, bir çalışma kaydı dosyasının bilgisidir.
type RunInfo struct {
	Path  string    `json:"path"`
	Job   string    `json:"job"`
	Start time.Time `json:"start"`
}

// ListRuns, çalışma kayıtlarını en yeniden eskiye sıralı döner. job boşsa
// tüm işler listelenir. Adı biçime uymayan dosyalar atlanır.
func ListRuns(logsDir, job string) ([]RunInfo, error) {
	entries, err := readDir(RunsDir(logsDir))
	if err != nil {
		return nil, err
	}
	type item struct {
		RunInfo
		seq int
	}
	var items []item
	for _, e := range entries {
		m := runFileRe.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() || (job != "" && !strings.EqualFold(m[1], job)) {
			continue
		}
		t, err := time.ParseInLocation(runTimeLayout, m[2], time.Local)
		if err != nil {
			continue
		}
		seq, _ := strconv.Atoi(m[3])
		items = append(items, item{RunInfo{Path: filepath.Join(RunsDir(logsDir), e.Name()), Job: m[1], Start: t}, seq})
	}
	sort.SliceStable(items, func(i, k int) bool {
		a, b := items[i], items[k]
		if !a.Start.Equal(b.Start) {
			return a.Start.After(b.Start)
		}
		if a.Job != b.Job {
			return a.Job < b.Job
		}
		return a.seq > b.seq
	})
	out := make([]RunInfo, len(items))
	for i, it := range items {
		out[i] = it.RunInfo
	}
	return out, nil
}

// ReadRun, bir çalışma kaydını v içine okur.
func ReadRun(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("çalışma kaydı okunamadı: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("çalışma kaydı çözümlenemedi (%s): %w", path, err)
	}
	return nil
}

// Cleanup, keepDays günden eski log dosyalarını ve çalışma kayıtlarını
// siler; silinen dosya sayısını döner. Tarih dosya adından okunur; adı
// biçime uymayan dosyalara dokunulmaz. Yarım kalmış geçici kayıt dosyaları
// (.run-*.tmp) bir günden eskiyse silinir. keepDays <= 0 ise hiçbir şey
// yapılmaz. Bir dosya silinemese de diğerlerine devam edilir.
func Cleanup(logsDir string, keepDays int, now time.Time) (int, error) {
	if keepDays <= 0 {
		return 0, nil
	}
	cutoff := now.AddDate(0, 0, -keepDays)
	// Log dosyaları gün bazındadır: günün tamamı sınırdan eskiyse silinir.
	y, m, d := cutoff.Date()
	cutoffDay := time.Date(y, m, d, 0, 0, 0, 0, now.Location())

	var deleted int
	var errs []error
	remove := func(p string) {
		if err := os.Remove(p); err != nil {
			errs = append(errs, err)
			return
		}
		deleted++
	}

	logs, err := ListLogs(logsDir, "")
	if err != nil {
		return 0, err
	}
	for _, l := range logs {
		if l.Date.Before(cutoffDay) {
			remove(l.Path)
		}
	}
	runs, err := ListRuns(logsDir, "")
	if err != nil {
		return deleted, err
	}
	for _, r := range runs {
		if r.Start.Before(cutoff) {
			remove(r.Path)
		}
	}
	if entries, err := readDir(RunsDir(logsDir)); err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".run-") || !strings.HasSuffix(e.Name(), ".tmp") {
				continue
			}
			if info, err := e.Info(); err == nil && info.ModTime().Before(now.Add(-24*time.Hour)) {
				remove(filepath.Join(RunsDir(logsDir), e.Name()))
			}
		}
	}
	if len(errs) > 0 {
		return deleted, fmt.Errorf("bazı eski log dosyaları silinemedi: %w", errors.Join(errs...))
	}
	return deleted, nil
}

// readDir, klasör yoksa boş liste döner.
func readDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}
