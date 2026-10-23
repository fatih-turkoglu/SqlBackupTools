package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sqlbackup/internal/config"
)

func TestRetentionPatternMatch(t *testing.T) {
	p, err := newRetentionPattern(config.DefaultFileNameTemplate, `PC01\SQLEXPRESS`, "gece", []string{"Satis", "Satis_Arsiv", "a.b"})
	if err != nil {
		t.Fatal(err)
	}
	at := func(y, mo, d, h, mi, s int) time.Time {
		return time.Date(y, time.Month(mo), d, h, mi, s, 0, time.Local)
	}
	tests := []struct {
		name   string
		series string
		time   time.Time
		ok     bool
	}{
		{"Satis_FULL_20261023_020000.bak", "satis", at(2026, 10, 23, 2, 0, 0), true},
		{"satis_full_20261023_020000.ZIP", "satis", at(2026, 10, 23, 2, 0, 0), true},
		{"Satis_FULL_20261023_020000_2.bak", "satis", at(2026, 10, 23, 2, 0, 0), true},
		{"Satis_Arsiv_FULL_20261023_020000.bak", "satis_arsiv", at(2026, 10, 23, 2, 0, 0), true},
		{"a.b_FULL_20261023_020000.bak", "a.b", at(2026, 10, 23, 2, 0, 0), true},
		{"aXb_FULL_20261023_020000.bak", "", time.Time{}, false}, // '.' regex jokeri olmamalı
		{"Diger_FULL_20261023_020000.bak", "", time.Time{}, false},
		{"Satis_FULL_20261323_020000.bak", "", time.Time{}, false}, // geçersiz ay
		{"Satis_FULL_20261023_020000.bak.tmp", "", time.Time{}, false},
		{"Satis_FULL_20261023_020000.zip.tmp", "", time.Time{}, false},
		{"Satis_FULL_2026102_020000.bak", "", time.Time{}, false},
		{"Satis_FULL_20261023_020000_x.bak", "", time.Time{}, false},
	}
	for _, tt := range tests {
		s, tm, ok := p.match(tt.name)
		if ok != tt.ok || s != tt.series || !tm.Equal(tt.time) {
			t.Errorf("%s: (%q, %v, %v), beklenen (%q, %v, %v)", tt.name, s, tm, ok, tt.series, tt.time, tt.ok)
		}
	}
}

func TestRetentionPatternPlaceholders(t *testing.T) {
	p, err := newRetentionPattern("{server}-{job}-{db}-{dd.MM.yy}-{db}", `PC01\SQLEXPRESS`, "gece", []string{"Satis"})
	if err != nil {
		t.Fatal(err)
	}
	if s, tm, ok := p.match("PC01_SQLEXPRESS-gece-Satis-23.10.26-Satis.bak"); !ok || s != "satis" ||
		!tm.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.Local)) {
		t.Errorf("eşleşmedi: %q %v %v", s, tm, ok)
	}
	for _, name := range []string{
		"PC02_SQLEXPRESS-gece-Satis-23.10.26-Satis.bak", // farklı sunucu
		"PC01_SQLEXPRESS-ogle-Satis-23.10.26-Satis.bak", // farklı iş
	} {
		if _, _, ok := p.match(name); ok {
			t.Errorf("%s eşleşmemeliydi", name)
		}
	}
}

func TestRetentionPatternNoDate(t *testing.T) {
	for _, tmpl := range []string{"{db}.bak", "{db}_{HHmmss}", "{db}_{yyyyMM}"} {
		if _, err := newRetentionPattern(tmpl, "s", "j", []string{"db"}); err != errNoDate {
			t.Errorf("%s: errNoDate bekleniyordu, %v", tmpl, err)
		}
	}
}

func TestExpiredFiles(t *testing.T) {
	now := time.Date(2026, 10, 23, 2, 0, 0, 0, time.Local)
	day := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	files := []retentionFile{
		{Path: "a0.bak", Series: "a", Time: day(0)},
		{Path: "a0.zip", Series: "a", Time: day(0)},
		{Path: "a1.bak", Series: "a", Time: day(1)},
		{Path: "a1.zip", Series: "a", Time: day(1)},
		{Path: "a5.bak", Series: "a", Time: day(5)},
		{Path: "a8.bak", Series: "a", Time: day(8)},
		{Path: "b9.bak", Series: "b", Time: day(9)},
		{Path: "b9_2.bak", Series: "b", Time: day(9)},
		{Path: "b30.bak", Series: "b", Time: day(30)},
	}
	names := func(fs []retentionFile) string {
		var s []string
		for _, f := range fs {
			s = append(s, f.Path)
		}
		return strings.Join(s, ",")
	}
	tests := []struct {
		mode    string
		value   int
		protect map[string]bool
		want    string
	}{
		{config.RetentionDays, 7, nil, "a8.bak,b30.bak,b9.bak,b9_2.bak"},
		{config.RetentionDays, 9, nil, "b30.bak"}, // tam 9 gün önceki silinmez
		{config.RetentionDays, 10, nil, "b30.bak"},
		{config.RetentionDays, 7, map[string]bool{"b30.bak": true}, "a8.bak,b9.bak,b9_2.bak"},
		{config.RetentionCount, 2, nil, "a5.bak,a8.bak"},
		{config.RetentionCount, 1, nil, "a1.bak,a1.zip,a5.bak,a8.bak,b30.bak"},
		{config.RetentionCount, 10, nil, ""},
		{config.RetentionNone, 1, nil, ""},
	}
	for _, tt := range tests {
		if got := names(expiredFiles(files, tt.mode, tt.value, now, tt.protect)); got != tt.want {
			t.Errorf("%s %d: %s, beklenen %s", tt.mode, tt.value, got, tt.want)
		}
	}
}

// retentionDir, çalışma öncesi klasörde bulunan eski dosyaları oluşturur.
func retentionDir(t *testing.T, names ...string) (*config.Job, string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("eski"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	j := testJob()
	j.Backup.ServerPath = dir
	return j, dir
}

var oldFiles = []string{
	"Satis_FULL_20261010_020000.bak",
	"Satis_FULL_20261020_020000.zip",
	"Yeni_FULL_20261001_020000.bak",
	"Yeni_FULL_20261001_020000_2.bak",
	"Diger_FULL_20260101_020000.bak", // işe ait olmayan DB
	"notlar.txt",
}

func TestRunRetentionDays(t *testing.T) {
	j, dir := retentionDir(t, oldFiles...)
	j.Retention = config.Retention{Mode: config.RetentionDays, Value: 7}
	f := newFake()
	f.writeFiles = true
	r := runWith(t, j, f, nil)
	if r.ExitCode != ExitOK || r.Retention == nil || r.Retention.Error != "" || len(r.Retention.Deleted) != 3 {
		t.Fatalf("exit=%d retention=%+v", r.ExitCode, r.Retention)
	}
	want := "Diger_FULL_20260101_020000.bak,Satis_FULL_20261020_020000.zip,Satis_FULL_20261023_020000.bak," +
		"Yeni_FULL_20261023_020000.bak,notlar.txt"
	if got := dirNames(t, dir); got != want {
		t.Errorf("klasör içeriği: %s", got)
	}
	if d := r.Retention.Deleted[0]; d.File != filepath.Join(dir, "Satis_FULL_20261010_020000.bak") || d.Bytes != 4 ||
		!d.Time.Equal(time.Date(2026, 10, 10, 2, 0, 0, 0, time.Local)) {
		t.Errorf("silinen dosya kaydı: %+v", d)
	}
	if !strings.Contains(r.Summary(), "[SİL]") {
		t.Errorf("özet silinenleri göstermeli")
	}
}

func TestRunRetentionCount(t *testing.T) {
	j, dir := retentionDir(t, oldFiles...)
	j.Retention = config.Retention{Mode: config.RetentionCount, Value: 2}
	f := newFake()
	f.writeFiles = true
	r := runWith(t, j, f, nil)
	if r.ExitCode != ExitOK || len(r.Retention.Deleted) != 1 {
		t.Fatalf("exit=%d retention=%+v", r.ExitCode, r.Retention)
	}
	want := "Diger_FULL_20260101_020000.bak,Satis_FULL_20261020_020000.zip,Satis_FULL_20261023_020000.bak," +
		"Yeni_FULL_20261001_020000.bak,Yeni_FULL_20261001_020000_2.bak,Yeni_FULL_20261023_020000.bak,notlar.txt"
	if got := dirNames(t, dir); got != want {
		t.Errorf("klasör içeriği: %s", got)
	}
}

func TestRunRetentionKeepsFailedDatabase(t *testing.T) {
	// Yedeği alınamayan veritabanının eski yedeklerine dokunulmaz.
	j, dir := retentionDir(t, oldFiles...)
	j.Retention = config.Retention{Mode: config.RetentionDays, Value: 7}
	f := newFake()
	f.writeFiles = true
	f.failBackup = map[string]bool{"Yeni": true}
	r := runWith(t, j, f, nil)
	if r.ExitCode != ExitPartial || len(r.Retention.Deleted) != 1 {
		t.Fatalf("exit=%d retention=%+v", r.ExitCode, r.Retention)
	}
	if got := dirNames(t, dir); !strings.Contains(got, "Yeni_FULL_20261001_020000.bak") ||
		strings.Contains(got, "Satis_FULL_20261010") {
		t.Errorf("klasör içeriği: %s", got)
	}
}

func TestRunRetentionAllFailed(t *testing.T) {
	j, dir := retentionDir(t, oldFiles...)
	j.Retention = config.Retention{Mode: config.RetentionDays, Value: 1}
	f := newFake()
	f.failBackup = map[string]bool{"Satis": true, "Yeni": true}
	r := runWith(t, j, f, nil)
	if r.ExitCode != ExitFailed || r.Retention != nil {
		t.Fatalf("exit=%d retention=%+v", r.ExitCode, r.Retention)
	}
	if got := dirNames(t, dir); len(strings.Split(got, ",")) != len(oldFiles) {
		t.Errorf("hiçbir dosya silinmemeliydi: %s", got)
	}
}

func TestRunRetentionSingleZip(t *testing.T) {
	j, dir := retentionDir(t, "gece_FULL_20261001_020000.zip", "gece_FULL_20261022_020000.zip")
	j.Archive.ZipEnabled = true
	j.Archive.DeleteBakAfterZip = true
	j.Retention = config.Retention{Mode: config.RetentionCount, Value: 2}
	f := newFake()
	f.writeFiles = true
	r := runWith(t, j, f, nil)
	if r.ExitCode != ExitOK || len(r.Retention.Deleted) != 1 {
		t.Fatalf("exit=%d retention=%+v", r.ExitCode, r.Retention)
	}
	if got := dirNames(t, dir); got != "gece_FULL_20261022_020000.zip,gece_FULL_20261023_020000.zip" {
		t.Errorf("klasör içeriği: %s", got)
	}
}

func TestRunRetentionNoDate(t *testing.T) {
	j, _ := retentionDir(t)
	j.Backup.FileNameTemplate = "{db}.bak"
	j.Retention = config.Retention{Mode: config.RetentionCount, Value: 2}
	r := runWith(t, j, newFake(), nil)
	if r.ExitCode != ExitOK || len(r.Warnings) == 0 || !strings.Contains(r.Warnings[len(r.Warnings)-1], "tarih") {
		t.Fatalf("exit=%d warnings=%v", r.ExitCode, r.Warnings)
	}
}

func TestRunRetentionNoAccess(t *testing.T) {
	j, dir := retentionDir(t)
	j.Backup.AccessPath = filepath.Join(dir, "yok")
	j.Retention = config.Retention{Mode: config.RetentionDays, Value: 7}
	f := newFake()
	r := runWith(t, j, f, nil)
	if r.ExitCode != ExitPartial || len(f.backups) != 2 || !strings.Contains(r.Retention.Error, "erişilemiyor") {
		t.Fatalf("exit=%d backups=%v retention=%+v", r.ExitCode, f.backups, r.Retention)
	}
}
