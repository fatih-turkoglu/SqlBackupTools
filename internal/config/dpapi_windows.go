package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// entropy, DPAPI'ye verilen ek anahtardır; başka uygulamaların aynı makine
// kapsamıyla şifrelenmiş veriyi kolayca çözmesini zorlaştırır.
var entropy = []byte("SqlBackup.v1.connection-password")

// Protect, metni makine kapsamında (CRYPTPROTECT_LOCAL_MACHINE) DPAPI ile
// şifreler ve base64 döner. Makine kapsamı seçildi çünkü görev SYSTEM ya da
// farklı bir kullanıcıyla çalışabilir; şifre yalnızca bu makinede çözülebilir.
func Protect(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	in := newBlob([]byte(plain))
	ent := newBlob(entropy)
	var out windows.DataBlob
	err := windows.CryptProtectData(in, nil, ent, 0, nil,
		windows.CRYPTPROTECT_LOCAL_MACHINE|windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return "", fmt.Errorf("şifre şifrelenemedi: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return base64.StdEncoding.EncodeToString(blobBytes(out)), nil
}

// Unprotect, Protect ile üretilmiş değeri çözer.
func Unprotect(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", errors.New("şifreli değer geçersiz (base64 değil)")
	}
	in := newBlob(raw)
	ent := newBlob(entropy)
	var out windows.DataBlob
	err = windows.CryptUnprotectData(in, nil, ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return "", fmt.Errorf("şifre çözülemedi (başka bir makinede mi oluşturuldu?): %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(blobBytes(out)), nil
}

func newBlob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func blobBytes(b windows.DataBlob) []byte {
	if b.Size == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(b.Data, b.Size)...)
}

// SetPassword, düz metin şifreyi şifreleyip bağlantıya yazar.
func (c *Connection) SetPassword(plain string) error {
	enc, err := Protect(plain)
	if err != nil {
		return err
	}
	c.PasswordEnc = enc
	return nil
}

// Password, bağlantının şifresini çözerek döner.
func (c *Connection) Password() (string, error) {
	return Unprotect(c.PasswordEnc)
}
