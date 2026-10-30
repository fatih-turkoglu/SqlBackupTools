package config

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileAllAccess, dosya/klasör için GENERIC_ALL karşılığıdır (FILE_ALL_ACCESS).
const fileAllAccess = 0x1F01FF

// aces, yoldaki DACL'ın bu nesneye uygulanan izin veren ACE'lerini SID metnine
// göre (aynı SID'in maskeleri birleştirilerek) döner.
func aces(t *testing.T, path string) (map[string]*windows.ACCESS_ALLOWED_ACE, bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, _ := sd.Control()
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil {
		t.Fatalf("%s: NULL DACL (herkese tam yetki)", path)
	}
	out := map[string]*windows.ACCESS_ALLOWED_ACE{}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		// Yalnızca devredilen (inherit-only) girdiler bu nesneye uygulanmaz.
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if prev := out[sid]; prev != nil {
			merged := *prev
			merged.Mask |= ace.Mask
			ace = &merged
		}
		out[sid] = ace
	}
	return out, control&windows.SE_DACL_PROTECTED != 0
}

func wellKnown(t *testing.T, typ windows.WELL_KNOWN_SID_TYPE) string {
	t.Helper()
	sid, err := windows.CreateWellKnownSid(typ)
	if err != nil {
		t.Fatal(err)
	}
	return sid.String()
}

func currentUser(t *testing.T) string {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return tu.User.Sid.String()
}

// checkSecure, klasörün (ve içindeki dosyanın) yalnızca SYSTEM,
// Administrators ve geçerli kullanıcıya açık olduğunu denetler.
func checkSecure(t *testing.T, path string, wantProtected bool) {
	t.Helper()
	got, protected := aces(t, path)
	if protected != wantProtected {
		t.Errorf("%s: korumalı=%v, beklenen %v", path, protected, wantProtected)
	}
	allowed := map[string]bool{
		wellKnown(t, windows.WinLocalSystemSid):           true,
		wellKnown(t, windows.WinBuiltinAdministratorsSid): true,
		currentUser(t): true,
	}
	for sid := range got {
		if !allowed[sid] {
			t.Errorf("%s: beklenmeyen izin: %s", path, sid)
		}
	}
	if ace := got[currentUser(t)]; ace == nil || ace.Mask&fileAllAccess != fileAllAccess {
		t.Errorf("%s: kullanıcının tam yetkisi yok: %+v", path, ace)
	}
}

func TestEnsureDirsSecuresDataDir(t *testing.T) {
	t.Setenv(HomeEnv, filepath.Join(t.TempDir(), "Veri Ş"))
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	checkSecure(t, DataDir(), true)
	checkSecure(t, JobsDir(), false)
	checkSecure(t, LogsDir(), false)

	// SYSTEM'in veya kullanıcının açtığı log ve iş dosyaları aynı izinleri devralır.
	for _, f := range []string{filepath.Join(LogsDir(), "gece_20261023.log"), JobPath("gece")} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		checkSecure(t, f, false)
	}
	// İkinci çağrı (klasör var) hata vermez.
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
}

// grantUsers, eski sürümlerin verdiği izni taklit eder: Users'a devredilen Değiştirme.
func grantUsers(t *testing.T, dir string) {
	t.Helper()
	users, _ := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, _ := sd.DACL()
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.DELETE,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(users)},
	}}, dacl)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSecureDataDirMigratesOldInstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "SqlBackup")
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(filepath.Join(root, "jobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	grantUsers(t, root) // %ProgramData%'dan devralınan Users okuma/oluşturma izni yerine
	grantUsers(t, logs) // eski EnsureDirs'in logs izni
	job := filepath.Join(root, "jobs", "gece.json")
	logFile := filepath.Join(logs, "gece_20261023.log")
	for _, f := range []string{job, logFile} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	users := wellKnown(t, windows.WinBuiltinUsersSid)
	if got, _ := aces(t, job); got[users] == nil {
		t.Fatal("hazırlık: iş dosyası Users iznini devralmadı")
	}

	// migrate=false (--home ile verilen var olan klasör): dokunulmaz.
	if err := secureDataDir(root, false); err != nil {
		t.Fatal(err)
	}
	if err := resetLogsACL(root, logs); err != nil {
		t.Fatal(err)
	}
	if got, protected := aces(t, root); protected || got[users] == nil {
		t.Error("migrate=false iken izinler değişti")
	}
	if got, _ := aces(t, logs); got[users] == nil {
		t.Error("kök taşınmamışken logs izni kaldırıldı")
	}

	if err := secureDataDir(root, true); err != nil {
		t.Fatal(err)
	}
	if err := resetLogsACL(root, logs); err != nil {
		t.Fatal(err)
	}
	checkSecure(t, root, true)
	checkSecure(t, logs, false)
	checkSecure(t, job, false)
	checkSecure(t, logFile, false)
}

func TestEmptyACLIsNotNull(t *testing.T) {
	acl, err := emptyACL()
	if err != nil || acl == nil || acl.AceCount != 0 {
		t.Fatalf("emptyACL: %+v %v", acl, err)
	}
}
