package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProtectRoundTrip(t *testing.T) {
	for _, plain := range []string{"S3cret!", "Çok_Gizli_Şifre_ğüİ", strings.Repeat("x", 500)} {
		enc, err := Protect(plain)
		if err != nil {
			t.Fatal(err)
		}
		if enc == plain || strings.Contains(enc, plain) {
			t.Fatal("şifreli değer düz metni içeriyor")
		}
		got, err := Unprotect(enc)
		if err != nil {
			t.Fatal(err)
		}
		if got != plain {
			t.Fatalf("beklenen %q, gelen %q", plain, got)
		}
	}
}

func TestProtectEmpty(t *testing.T) {
	enc, err := Protect("")
	if err != nil || enc != "" {
		t.Fatalf("boş şifre boş dönmeli: %q %v", enc, err)
	}
	plain, err := Unprotect("")
	if err != nil || plain != "" {
		t.Fatalf("boş değer boş dönmeli: %q %v", plain, err)
	}
}

func TestUnprotectInvalid(t *testing.T) {
	if _, err := Unprotect("!!!"); err == nil {
		t.Fatal("geçersiz base64 için hata bekleniyordu")
	}
	if _, err := Unprotect("AAAA"); err == nil {
		t.Fatal("geçersiz DPAPI verisi için hata bekleniyordu")
	}
}

func TestPasswordNeverSerializedInPlain(t *testing.T) {
	var c Connection
	if err := c.SetPassword("GizliParola123"); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(c)
	if strings.Contains(string(data), "GizliParola123") {
		t.Fatal("JSON düz metin şifre içeriyor")
	}
	got, err := c.Password()
	if err != nil || got != "GizliParola123" {
		t.Fatalf("şifre çözülemedi: %q %v", got, err)
	}
}
