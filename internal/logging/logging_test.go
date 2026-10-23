package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 23, 2, 0, 0, 0, time.Local)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestJobLogAppend(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs") // klasör yoksa oluşturulmalı
	for i, msg := range []string{"ilk çalışma", "ikinci çalışma"} {
		l, err := OpenJobLog(dir, "gece", t0.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		slog.New(l.Handler(slog.LevelInfo)).Info(msg, "db", "Satış")
		slog.New(l.Handler(slog.LevelInfo)).Debug("görünmemeli")
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		if l.Path != filepath.Join(dir, "gece_20261023.log") {
			t.Errorf("log yolu: %s", l.Path)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "gece_20261023.log"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Count(s, "\n") != 2 || !strings.Contains(s, `msg="ilk çalışma" db=Satış`) ||
		!strings.Contains(s, "ikinci çalışma") || strings.Contains(s, "görünmemeli") {
		t.Errorf("log içeriği:\n%s", s)
	}
}

func TestListLogs(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"gece_20261022.log", "gece_20261023.log", "ogle_vakti_20261023.log", "rastgele.log", "gece_2026102.log"} {
		touch(t, filepath.Join(dir, n))
	}
	logs, err := ListLogs(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range logs {
		got = append(got, l.Job+"@"+l.Date.Format("0102"))
	}
	if strings.Join(got, ",") != "gece@1023,ogle_vakti@1023,gece@1022" {
		t.Errorf("liste: %v", got)
	}
	if logs, _ := ListLogs(dir, "GECE"); len(logs) != 2 || logs[0].Size != 1 {
		t.Errorf("iş filtresi: %+v", logs)
	}
	if logs, err := ListLogs(filepath.Join(dir, "yok"), ""); err != nil || len(logs) != 0 {
		t.Errorf("olmayan klasör: %v %v", logs, err)
	}
}

type sample struct {
	Job      string `json:"job"`
	ExitCode int    `json:"exitCode"`
}

func TestSaveAndListRuns(t *testing.T) {
	dir := t.TempDir()
	p1, err := SaveRun(dir, "gece", t0, sample{"gece", 0})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := SaveRun(dir, "gece", t0, sample{"gece", 3}) // aynı saniye
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRun(dir, "ogle", t0.Add(-time.Hour), sample{"ogle", 1}); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p1) != "gece_20261023_020000.json" || filepath.Base(p2) != "gece_20261023_020000_2.json" {
		t.Errorf("kayıt adları: %s %s", p1, p2)
	}
	touch(t, filepath.Join(RunsDir(dir), "bozuk.json"))

	runs, err := ListRuns(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range runs {
		got = append(got, filepath.Base(r.Path))
	}
	want := "gece_20261023_020000_2.json,gece_20261023_020000.json,ogle_20261023_010000.json"
	if strings.Join(got, ",") != want {
		t.Errorf("liste: %v", got)
	}
	if runs[2].Job != "ogle" || !runs[2].Start.Equal(t0.Add(-time.Hour)) {
		t.Errorf("kayıt bilgisi: %+v", runs[2])
	}

	var s sample
	if err := ReadRun(p2, &s); err != nil || s.ExitCode != 3 {
		t.Errorf("okunan: %+v %v", s, err)
	}
	if runs, _ := ListRuns(dir, "ogle"); len(runs) != 1 {
		t.Errorf("iş filtresi: %v", runs)
	}
	// Geçici dosya kalmamalı.
	entries, _ := os.ReadDir(RunsDir(dir))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("geçici dosya kaldı: %s", e.Name())
		}
	}
}

func TestCleanup(t *testing.T) {
	dir := t.TempDir()
	runs := RunsDir(dir)
	files := map[string]bool{ // dosya → silinmeli mi
		"gece_20260922.log": true,
		"gece_20260923.log": false, // sınır günü (23.09) tutulur
		"gece_20261023.log": false,
		"notlar.txt":        false,
		"gece.log":          false,
		filepath.Join("runs", "gece_20260923_015959.json"): true,
		filepath.Join("runs", "gece_20260923_020000.json"): false, // tam 30 gün
		filepath.Join("runs", "gece_20261022_020000.json"): false,
		filepath.Join("runs", "baska.json"):                false,
	}
	for n := range files {
		touch(t, filepath.Join(dir, n))
	}
	oldTmp := filepath.Join(runs, ".run-123.tmp")
	newTmp := filepath.Join(runs, ".run-456.tmp")
	touch(t, oldTmp)
	touch(t, newTmp)
	// Geçici dosyalar için dosya zamanı kullanılır (bir günden eskiyse silinir).
	os.Chtimes(oldTmp, t0.Add(-48*time.Hour), t0.Add(-48*time.Hour))
	os.Chtimes(newTmp, t0.Add(-time.Hour), t0.Add(-time.Hour))

	n, err := Cleanup(dir, 30, t0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("silinen sayısı: %d", n)
	}
	for f, del := range files {
		_, err := os.Stat(filepath.Join(dir, f))
		if exists := err == nil; exists == del {
			t.Errorf("%s: var=%v, silinmeli=%v", f, exists, del)
		}
	}
	if _, err := os.Stat(oldTmp); err == nil {
		t.Error("eski geçici dosya kaldı")
	}
	if _, err := os.Stat(newTmp); err != nil {
		t.Error("yeni geçici dosya silinmemeliydi")
	}
	if n, err := Cleanup(dir, 0, t0); n != 0 || err != nil {
		t.Errorf("keepDays=0 hiçbir şey yapmamalı: %d %v", n, err)
	}
}
