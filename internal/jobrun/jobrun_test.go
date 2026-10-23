package jobrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sqlbackup/internal/backup"
	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n/i18ntest"
	"sqlbackup/internal/logging"
)

func TestMain(m *testing.M) {
	i18ntest.Use("tr")
	os.Exit(m.Run())
}

var fixed = time.Date(2026, 10, 23, 2, 0, 0, 0, time.Local)

func TestInvalidNameCreatesNothing(t *testing.T) {
	t.Setenv(config.HomeEnv, filepath.Join(t.TempDir(), "veri"))
	j := config.NewJob(`..\kötü`)
	r := Execute(context.Background(), j, Options{Now: func() time.Time { return fixed }})
	if r.ExitCode != backup.ExitFailed || r.Error == "" || !r.Start.Equal(fixed) {
		t.Errorf("%+v", r)
	}
	if _, err := os.Stat(config.DataDir()); !errors.Is(err, os.ErrNotExist) {
		t.Error("geçersiz adla klasör/log oluşturulmamalı")
	}
}

func TestGUISourceAndRecord(t *testing.T) {
	t.Setenv(config.HomeEnv, filepath.Join(t.TempDir(), "veri"))
	j := config.NewJob("gece") // geçersiz ayarlar: çalışma kaydı yine yazılır
	var warned []error
	r := Execute(context.Background(), j, Options{Source: SourceGUI, Version: "9.9", Now: func() time.Time { return fixed },
		Warn: func(err error) { warned = append(warned, err) }})
	if r.ExitCode != backup.ExitFailed || len(warned) != 0 {
		t.Fatalf("%+v %v", r, warned)
	}
	data, err := os.ReadFile(filepath.Join(config.LogsDir(), "gece_20261023.log"))
	if err != nil || !strings.Contains(string(data), "arayüzden çalıştırıldı") || !strings.Contains(string(data), "version=9.9") {
		t.Errorf("%s %v", data, err)
	}
	if runs, _ := logging.ListRuns(config.LogsDir(), "gece"); len(runs) != 1 {
		t.Errorf("çalışma kaydı: %v", runs)
	}
}
