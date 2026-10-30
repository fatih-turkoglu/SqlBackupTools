package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Veri klasörü izin modeli:
//
// Kök klasör (%ProgramData%\SqlBackup) devralmayı kesen (korumalı) bir DACL
// ile oluşturulur: SYSTEM, Administrators ve klasörü kullanan kullanıcı hesabı
// tam yetkili; Users dahil başka kimse erişemez. Alt klasörler (jobs, logs)
// ve dosyalar bunu devralır.
//
// Neden: %ProgramData% Users'a okuma ve klasör/dosya oluşturma izni verir.
//   - jobs\*.json'daki passwordEnc makine kapsamlı DPAPI ile şifrelidir; makinedeki
//     her kullanıcı çözebilir → dosyayı okuyabilen herkes SQL şifresini elde ederdi.
//   - Eskiden logs'a Users için Değiştirme izni veriliyordu (SYSTEM'in açtığı log'a
//     kullanıcı yazabilsin diye). Bu izin klasörün kendisini silmeyi de içerdiği
//     için logs bir junction ile değiştirilip SYSTEM'e istenen yola yazdırılabiliyordu.
//     Ortak yazım artık kullanıcı hesabına verilen (devredilen) tam yetkiyle sağlanır.

// accountSID, sid'in gerçek bir kullanıcı hesabı (yerel/etki alanı veya
// Entra ID) olup olmadığını döner; SYSTEM, gruplar ve hizmet hesapları değildir.
func accountSID(sid *windows.SID) bool {
	s := sid.String()
	return strings.HasPrefix(s, "S-1-5-21-") || strings.HasPrefix(s, "S-1-12-1-")
}

// dataDirACL, kök klasörün DACL'ıdır: SYSTEM, Administrators ve verilen
// hesaplar tam yetkili, alt klasör ve dosyalara devredilir.
func dataDirACL(accounts []*windows.SID) (*windows.ACL, error) {
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	entry := func(sid *windows.SID, typ windows.TRUSTEE_TYPE) windows.EXPLICIT_ACCESS {
		return windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  typ,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		}
	}
	entries := []windows.EXPLICIT_ACCESS{
		entry(system, windows.TRUSTEE_IS_USER),
		entry(admins, windows.TRUSTEE_IS_WELL_KNOWN_GROUP),
	}
	for _, a := range accounts {
		entries = append(entries, entry(a, windows.TRUSTEE_IS_USER))
	}
	return windows.ACLFromEntries(entries, nil)
}

// dataDirAccounts, kök klasörde tam yetki verilecek kullanıcı hesaplarıdır:
// süreci çalıştıran hesap ve (varsa) klasörün sahibi. SYSTEM olarak
// çalışırken (zamanlanmış görev) yalnızca sahip eklenir.
func dataDirAccounts(owner *windows.SID) ([]*windows.SID, error) {
	var out []*windows.SID
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	if accountSID(tu.User.Sid) {
		out = append(out, tu.User.Sid)
	}
	if owner != nil && accountSID(owner) && (len(out) == 0 || !owner.Equals(out[0])) {
		out = append(out, owner)
	}
	return out, nil
}

// secureDataDir, kök veri klasörünü korumalı DACL ile oluşturur. Klasör
// varsa ve DACL'ı korumalı değilse (eski sürümün oluşturduğu klasör) migrate
// true olduğunda izinler yeniden yazılır; yetki yoksa (klasörün sahibi başka
// bir kullanıcı) dokunulmaz. migrate yalnızca varsayılan klasör için verilir:
// --home ile gösterilen var olan bir klasörün (ör. bir sürücü kökü) izinleri
// değiştirilmemelidir.
func secureDataDir(dir string, migrate bool) error {
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return createSecureDir(dir)
	}
	if err != nil {
		return err
	}
	if !migrate {
		return nil
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED != 0 {
		return nil
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	accounts, err := dataDirAccounts(owner)
	if err != nil {
		return err
	}
	// SYSTEM olarak çalışırken sahip bir kullanıcı değilse (ör. klasör yönetici
	// olarak oluşturulmuş) arayüzü kullanan hesabı bilemeyiz; taşıma, klasörü
	// kullanan hesap çalıştırdığında yapılır.
	if len(accounts) == 0 {
		return nil
	}
	acl, err := dataDirACL(accounts)
	if err != nil {
		return err
	}
	// Alt klasör ve dosyalardaki devralınmış izinler de yeniden hesaplanır.
	err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil
	}
	return err
}

// createSecureDir, klasörü (üst klasörleri normal izinlerle) korumalı DACL
// ile oluşturur. İzin oluşturma anında verilir; arada başka bir kullanıcının
// alt klasör açabileceği bir an kalmaz.
func createSecureDir(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	accounts, err := dataDirAccounts(nil)
	if err != nil {
		return err
	}
	acl, err := dataDirACL(accounts)
	if err != nil {
		return err
	}
	sd, err := windows.NewSecurityDescriptor()
	if err != nil {
		return err
	}
	if err := sd.SetDACL(acl, true, false); err != nil {
		return err
	}
	if err := sd.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED); err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	err = windows.CreateDirectory(p, sa)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil
	}
	return err
}

// resetLogsACL, eski sürümlerin logs klasörüne eklediği açık izinleri
// (Users: Değiştirme) kaldırır; klasör yalnızca kökten devralır. Açık izin
// yoksa veya kök korumalı değilse (taşınmamış --home klasörü; ortak log
// yazımı hâlâ o izne bağlı) dokunulmaz.
func resetLogsACL(root, dir string) error {
	rsd, err := windows.GetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if control, _, err := rsd.Control(); err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return err
	}
	explicit := false
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE == 0 {
			explicit = true
			break
		}
	}
	if !explicit {
		return nil
	}
	// Boş (NULL olmayan) DACL + devralmaya açık: yalnızca kökün izinleri kalır.
	// NULL DACL herkese tam yetki demektir; asla verilmemeli.
	empty, err := emptyACL()
	if err != nil {
		return err
	}
	err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, empty, nil)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil
	}
	return err
}

// emptyACL, girdisi olmayan geçerli bir ACL döner.
func emptyACL() (*windows.ACL, error) {
	buf := make([]byte, 8) // sizeof(ACL)
	acl := (*windows.ACL)(unsafe.Pointer(&buf[0]))
	if err := initializeAcl(acl, uint32(len(buf)), aclRevision); err != nil {
		return nil, err
	}
	return acl, nil
}

const aclRevision = 2

var procInitializeAcl = windows.NewLazySystemDLL("advapi32.dll").NewProc("InitializeAcl")

func initializeAcl(acl *windows.ACL, size, rev uint32) error {
	if r, _, e := procInitializeAcl.Call(uintptr(unsafe.Pointer(acl)), uintptr(size), uintptr(rev)); r == 0 {
		return e
	}
	return nil
}
