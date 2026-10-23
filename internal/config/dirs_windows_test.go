package config

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// usersAce, yoldaki DACL'da BUILTIN\Users için izin veren ACE'yi döner.
func usersAce(t *testing.T, path string) *windows.ACCESS_ALLOWED_ACE {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	users, _ := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if ace.Header.AceType == windows.ACCESS_ALLOWED_ACE_TYPE && sid.Equals(users) && ace.Mask&modifyAccess == modifyAccess {
			return ace
		}
	}
	return nil
}

func TestEnsureDirsGrantsLogsToUsers(t *testing.T) {
	t.Setenv(HomeEnv, filepath.Join(t.TempDir(), "Veri Ş"))
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ace := usersAce(t, LogsDir())
	if ace == nil {
		t.Fatal("logs klasöründe Users değiştirme izni yok")
	}
	if want := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE); ace.Header.AceFlags&want != want {
		t.Errorf("izin devredilmiyor: flags=%#x", ace.Header.AceFlags)
	}
	// Yeni dosyalar (SYSTEM'in oluşturduğu log dahil) izni devralır.
	f := filepath.Join(LogsDir(), "gece_20261023.log")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ace := usersAce(t, f); ace == nil || ace.Header.AceFlags&windows.INHERITED_ACE == 0 {
		t.Errorf("dosya izni devralmadı: %+v", ace)
	}
	// İkinci çağrı (klasör var) izinlere dokunmaz ve hata vermez.
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
}
