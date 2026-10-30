package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sqlbackup/internal/i18n"
)

func TestCheckExeSecurity(t *testing.T) {
	// Windows klasöründeki exe'yi yalnızca TrustedInstaller/yöneticiler değiştirebilir.
	sys := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if err := CheckExeSecurity(sys); err != nil {
		t.Errorf("%s: %v", sys, err)
	}

	// Kullanıcının klasöründeki exe (test binary'si, build\bin, İndirilenler...) reddedilir.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	err = CheckExeSecurity(self)
	var ie *i18n.Error
	if !errors.As(err, &ie) || !strings.Contains(err.Error(), "SYSTEM görevi") {
		t.Errorf("kullanıcı klasöründeki exe kabul edildi: %v", err)
	}

	if err := CheckExeSecurity(filepath.Join(t.TempDir(), "yok.exe")); err == nil {
		t.Error("olmayan exe kabul edildi")
	}
}
