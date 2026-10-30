package scheduler

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"

	"sqlbackup/internal/i18n"
)

// SYSTEM görevi exe'yi en yüksek yetkiyle çalıştırır. Exe'yi (veya yanına
// DLL/klasör koymayı, klasörü yeniden adlandırmayı) yönetici olmayan bir
// hesap yapabiliyorsa o hesap kendi kodunu SYSTEM olarak çalıştırabilir
// (ör. build\bin, İndirilenler, kullanıcıya özel kurulum). Program Files
// altındaki kurulum güvenlidir.

// trustedSIDs, exe ve klasörlerinde yazma izni olabilecek hesaplardır.
var trustedSIDs = []string{
	"S-1-5-18",     // SYSTEM
	"S-1-5-32-544", // Administrators
	"S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464", // TrustedInstaller
	"S-1-3-0", // CREATOR OWNER (yalnızca devredilen girdi; sahip ayrıca denetlenir)
	"S-1-3-4", // OWNER RIGHTS (sahip ayrıca denetlenir)
}

// Exe, klasörü ve üst klasörler için tehlikeli izinler.
const (
	fileAddFile      = 0x2  // klasörde dosya oluşturma (WD)
	fileAddSubdir    = 0x4  // klasörde alt klasör oluşturma (AD)
	fileDeleteChild  = 0x40 // klasördeki öğeyi silme/yeniden adlandırma
	genericAll       = windows.GENERIC_ALL
	genericWrite     = windows.GENERIC_WRITE
	ownerChangeMask  = windows.WRITE_DAC | windows.WRITE_OWNER
	exeUnsafeMask    = windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.DELETE | ownerChangeMask | genericAll | genericWrite
	exeDirUnsafeMask = fileAddFile | fileAddSubdir | fileDeleteChild | windows.DELETE | ownerChangeMask | genericAll | genericWrite
	// Üst klasörde dosya/klasör oluşturmak zararsızdır (ör. C:\'de Authenticated
	// Users); alt klasörü silme/yeniden adlandırma ve izin değiştirme değildir.
	ancestorUnsafeMask = fileDeleteChild | windows.DELETE | ownerChangeMask | genericAll
)

// CheckExeSecurity, exe'nin SYSTEM görevinde güvenle kullanılabileceğini
// denetler: exe'yi, klasörünü ve üst klasörleri yalnızca SYSTEM,
// Administrators ve TrustedInstaller değiştirebilmeli (sahip de bunlardan
// biri olmalı). Reddeden (deny) girdiler hesaba katılmaz; denetim temkinlidir.
func CheckExeSecurity(exe string) error {
	p, err := filepath.Abs(exe)
	if err != nil {
		return i18n.Errorf("err.scheduler.path", "path", exe, "err", err)
	}
	mask := uint32(exeUnsafeMask)
	for i := 0; ; i++ {
		if who, err := unsafeAccess(p, mask); err != nil {
			return i18n.Errorf("err.scheduler.exeCheck", "path", p, "err", err)
		} else if who != "" {
			return i18n.Errorf("err.scheduler.unsafeExe", "exe", exe, "path", p, "account", who)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
		if i == 0 {
			mask = exeDirUnsafeMask
		} else {
			mask = ancestorUnsafeMask
		}
	}
}

// unsafeAccess, yolda güvenilmeyen bir hesabın mask'teki izinlerden birine
// sahip olup olmadığını denetler ve o hesabın adını (yoksa boş) döner.
func unsafeAccess(path string, mask uint32) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return "", err
	}
	// Sahip izinleri her zaman değiştirebilir (WRITE_DAC).
	if owner != nil && !trusted(owner) {
		return accountName(owner.String()), nil
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return "", err
	}
	if dacl == nil {
		return "Everyone", nil // NULL DACL: herkese tam yetki
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return "", err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if uint32(ace.Mask)&mask != 0 && !trusted(sid) {
			return accountName(sid.String()), nil
		}
	}
	return "", nil
}

func trusted(sid *windows.SID) bool {
	s := sid.String()
	for _, t := range trustedSIDs {
		if s == t {
			return true
		}
	}
	return false
}
