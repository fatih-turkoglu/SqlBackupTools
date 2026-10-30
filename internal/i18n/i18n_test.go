package i18n

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// useDir, testin sonunda dil klasörünü ve dili geri alır.
func useDir(t *testing.T, d string) {
	t.Helper()
	SetDir(d)
	t.Cleanup(func() {
		SetDir("")
		SetLanguage(Default)
	})
}

func writeLang(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParse(t *testing.T) {
	data := "\xef\xbb\xbf# yorum\n@code = xx\n\n  a.b = Merhaba {ad}  \nc=  satır1\\nsatır2\\t\\\\son\nd = a=b # yorum değil\n"
	m, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"@code": "xx",
		"a.b":   "Merhaba {ad}",
		"c":     "satır1\nsatır2\t\\son",
		"d":     "a=b # yorum değil",
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("sonuç: %#v", m)
	}
}

func TestParseErrors(t *testing.T) {
	data := "iyi = 1\nbozuk satır\n= değer\niki kelime = x\niyi = 2\nkacis = a\\q\nson = a\\\nsonra = 3\n"
	m, err := Parse([]byte(data))
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	for _, line := range []string{"line 2", "line 3", "line 4", "line 5", "line 6", "line 7"} {
		if !strings.Contains(err.Error(), line) {
			t.Errorf("hata %s içermeli: %v", line, err)
		}
	}
	// Bozuk satırlar atlanır, geçerliler kullanılır; tekrarda ilk değer kalır.
	if !reflect.DeepEqual(m, map[string]string{"iyi": "1", "sonra": "3"}) {
		t.Errorf("sonuç: %#v", m)
	}
}

func TestPlaceholders(t *testing.T) {
	got := Placeholders("{a} ve {b_2} {a} '{' {kötü ad} {{c}} {}")
	if !reflect.DeepEqual(got, []string{"a", "b_2", "c"}) {
		t.Errorf("yer tutucular: %v", got)
	}
}

func TestFallback(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	writeLang(t, dir, "xx.lng", "@name = Deneme\ncli.yes = evet-xx\nözel = {a} ile {b}, {a}\n")

	if err := SetLanguage("XX"); err != nil {
		t.Fatal(err)
	}
	if Language() != "xx" {
		t.Errorf("dil: %s", Language())
	}
	if got := T("cli.yes"); got != "evet-xx" {
		t.Errorf("seçili dil: %s", got)
	}
	if got := T("cli.no"); got != "no" {
		t.Errorf("İngilizce'ye düşmeli: %s", got)
	}
	if got := T("yok.boyle.anahtar"); got != "yok.boyle.anahtar" {
		t.Errorf("anahtar dönmeli: %s", got)
	}
	found := false
	for _, k := range Missing() {
		found = found || k == "yok.boyle.anahtar"
	}
	if !found {
		t.Error("eksik anahtar kaydedilmeli")
	}
	// Yer tutucular: tekrar, eksik argüman, değer içinde yer tutucu (tekrar işlenmez).
	if got := T("özel", "a", "{b}", "b", 2); got != "{b} ile 2, {b}" {
		t.Errorf("biçim: %s", got)
	}
	if got := T("özel", "a", 1); got != "1 ile {b}, 1" {
		t.Errorf("eksik argüman: %s", got)
	}
	if All()["cli.yes"] != "evet-xx" || All()["cli.no"] != "no" {
		t.Error("All seçili dili İngilizce üzerine bindirmeli")
	}
}

func TestSetLanguageErrors(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	writeLang(t, dir, "yy.lng", "cli.yes = evet-yy\nbozuk\n")

	for _, c := range []string{"zz", "../tr", "x", ""} {
		err := SetLanguage(c)
		if c == "" {
			if err != nil {
				t.Errorf("boş kod İngilizce seçmeli: %v", err)
			}
		} else if !errors.Is(err, ErrUnknownLanguage) {
			t.Errorf("%q: ErrUnknownLanguage bekleniyordu: %v", c, err)
		}
		if Language() != Default || T("cli.yes") != "yes" {
			t.Errorf("%q: İngilizce seçilmeli", c)
		}
	}
	// Bozuk satır dili engellemez, hata döner.
	if err := SetLanguage("yy"); err == nil || errors.Is(err, ErrUnknownLanguage) || T("cli.yes") != "evet-yy" {
		t.Errorf("bozuk dosya: %v, %s", err, T("cli.yes"))
	}
	// Diskte en.lng yokken İngilizce gömülü dosyadan gelir.
	if err := SetLanguage("en"); err != nil || T("cli.yes") != "yes" {
		t.Errorf("en: %v", err)
	}
}

func TestLanguages(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	writeLang(t, dir, "tr.lng", "@name = Türkçe\n")
	writeLang(t, dir, "de.LNG", "x = y\n") // @name yok → kod
	writeLang(t, dir, "notlar.txt", "")
	writeLang(t, dir, "k.lng", "") // geçersiz kod
	got := Languages()
	want := []LanguageInfo{{"de", "de"}, {"en", "English"}, {"tr", "Türkçe"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diller: %v", got)
	}
	useDir(t, filepath.Join(dir, "yok"))
	if got := Languages(); !reflect.DeepEqual(got, []LanguageInfo{{"en", "English"}}) {
		t.Errorf("klasör yokken yalnızca İngilizce: %v", got)
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	writeLang(t, dir, "tr.lng", "")
	writeLang(t, dir, "pt.lng", "")
	writeLang(t, dir, "zh-tw.lng", "")
	t.Cleanup(func() { SetSystemLanguages(nil) })
	tests := []struct {
		system []string
		want   string
	}{
		{[]string{"tr-TR", "en-US"}, "tr"},
		{[]string{"pt-BR"}, "pt"},             // ana dile düşer
		{[]string{"zh-TW", "tr-TR"}, "zh-tw"}, // tam etiket önce
		{[]string{"zh_CN", "ja-JP"}, Default}, // uyan yok
		{[]string{"ja-JP", "pt-PT"}, "pt"},    // tercih sırasında sonraki dil
		{[]string{"en-GB", "tr-TR"}, Default}, // İngilizce her zaman var
		{nil, Default},
	}
	for _, tt := range tests {
		SetSystemLanguages(func() []string { return tt.system })
		if got := Detect(); got != tt.want {
			t.Errorf("%v: %s, beklenen %s", tt.system, got, tt.want)
		}
	}
	// Gerçek Windows dilleri okunabilmeli (sonuç makineye bağlı).
	SetSystemLanguages(nil)
	if len(windowsLanguages()) == 0 {
		t.Error("Windows arayüz dili okunamadı")
	}
}

func TestError(t *testing.T) {
	base := errors.New("Access is denied.")
	err := Errorf("err.archive.noWrite", "path", `D:\Yedek`, "err", base)
	if err.Error() != `no write permission on folder (D:\Yedek): Access is denied.` {
		t.Errorf("metin: %s", err)
	}
	if !errors.Is(err, base) {
		t.Error("errors.Is sarmalanan hatayı bulmalı")
	}
	var ie *Error
	if !errors.As(err, &ie) || ie.Key != "err.archive.noWrite" {
		t.Error("errors.As *Error bulmalı")
	}

	// Metin gösterim anında üretilir: dil değişince aynı hata yeni dilde görünür.
	dir := t.TempDir()
	useDir(t, dir)
	writeLang(t, dir, "xx.lng", "err.archive.noWrite = yazılamıyor {path}: {err}\n")
	SetLanguage("xx")
	if err.Error() != `yazılamıyor D:\Yedek: Access is denied.` {
		t.Errorf("yeni dilde: %s", err)
	}
}

func TestConcurrent(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	writeLang(t, dir, "xx.lng", "cli.yes = evet\n")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for k := 0; k < 200; k++ {
				if i%2 == 0 {
					SetLanguage([]string{"xx", "en"}[k%2])
				}
				if v := T("cli.yes"); v != "yes" && v != "evet" {
					t.Errorf("beklenmeyen: %s", v)
				}
				_ = All()
			}
		}(i)
	}
	wg.Wait()
}
