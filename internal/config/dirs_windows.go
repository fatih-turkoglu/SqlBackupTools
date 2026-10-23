package config

import (
	"golang.org/x/sys/windows"
)

// modifyAccess, Windows'taki "Değiştirme" (Modify) iznidir: okuma, yazma,
// çalıştırma ve silme (izin/sahiplik değiştirme hariç).
const modifyAccess = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_GENERIC_EXECUTE | windows.DELETE

// grantUsersModify, klasöre BUILTIN\Users için alt klasör ve dosyalara
// devredilen Değiştirme izni ekler (mevcut izinler korunur).
//
// Neden: %ProgramData%'da Users yalnızca okuyabilir ve yeni dosya
// oluşturabilir; dosyanın tam yetkisi oluşturana (CREATOR OWNER) aittir.
// Zamanlanmış görev (SYSTEM) günün log dosyasını oluşturursa, aynı gün işi
// arayüzden çalıştıran kullanıcı o dosyaya ekleme yapamaz ve eski logları
// temizleyemezdi. Loglarda gizli bilgi bulunmaz.
func grantUsersModify(dir string) error {
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	users, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: modifyAccess,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(users),
		},
	}}, dacl)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
