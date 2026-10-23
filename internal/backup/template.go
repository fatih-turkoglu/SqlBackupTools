package backup

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// TemplateVars, dosya adı şablonundaki yer tutucuların değerleridir.
type TemplateVars struct {
	Server   string // {server}: sunucu adı (HOST\INSTANCE → HOST_INSTANCE)
	Database string // {db}
	Job      string // {job}
	Time     time.Time
}

// Tarih belirteçleri; uzun olanlar önce denenir (yyyy, yy'den önce).
var dateTokens = []struct{ token, layout string }{
	{"yyyy", "2006"},
	{"yy", "06"},
	{"MM", "01"},
	{"dd", "02"},
	{"HH", "15"},
	{"mm", "04"},
	{"ss", "05"},
}

// dateSeparators, tarih yer tutucusu içinde belirteçler arasında
// kullanılabilecek karakterlerdir (ör. {yyyy-MM-dd}, {yyyyMMdd_HHmmss}).
const dateSeparators = "-_. "

// invalidFileChars, Windows dosya adlarında kullanılamayan karakterlerdir.
const invalidFileChars = `\/:*?"<>|`

// ValidateTemplate, şablonun açılabilir olduğunu (kapanmamış parantez veya
// bilinmeyen yer tutucu olmadığını) kontrol eder.
func ValidateTemplate(tmpl string) error {
	_, err := ExpandTemplate(tmpl, TemplateVars{Server: "s", Database: "db", Job: "j", Time: time.Now()})
	return err
}

// ExpandTemplate, şablondaki yer tutucuları doldurur ve dosya adını döner.
// Değerlerdeki geçersiz dosya adı karakterleri '_' ile değiştirilir. Ad
// .bak ile bitmiyorsa uzantı eklenir.
//
// Yer tutucular: {db}, {server}, {job} ve tarih biçimleri: yyyy, yy, MM, dd,
// HH, mm, ss belirteçlerinin "-_. " ayırıcılarıyla birleşimi
// (ör. {yyyyMMdd}, {HHmmss}, {yyyy-MM-dd}).
func ExpandTemplate(tmpl string, v TemplateVars) (string, error) {
	var b strings.Builder
	err := walkTemplate(tmpl, func(lit string) { b.WriteString(lit) }, func(name string) error {
		val, err := placeholder(name, v)
		b.WriteString(val)
		return err
	})
	if err != nil {
		return "", err
	}

	out := strings.TrimSpace(b.String())
	if !strings.HasSuffix(strings.ToLower(out), ".bak") {
		out += ".bak"
	}
	return out, nil
}

// walkTemplate, şablonu sırayla sabit metin ve yer tutucu parçalarına ayırır.
// Parantez hatalarında ve ph'nin döndürdüğü ilk hatada durur.
func walkTemplate(tmpl string, literal func(string), ph func(name string) error) error {
	rest := tmpl
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			if strings.IndexByte(rest, '}') >= 0 {
				return errors.New("dosya adı şablonunda eşleşmeyen '}' var")
			}
			literal(rest)
			return nil
		}
		if strings.IndexByte(rest[:open], '}') >= 0 {
			return errors.New("dosya adı şablonunda eşleşmeyen '}' var")
		}
		literal(rest[:open])
		end := strings.IndexByte(rest[open:], '}')
		if end < 0 {
			return errors.New("dosya adı şablonunda kapanmamış '{' var")
		}
		if err := ph(rest[open+1 : open+end]); err != nil {
			return err
		}
		rest = rest[open+end+1:]
	}
}

func placeholder(name string, v TemplateVars) (string, error) {
	switch name {
	case "db":
		return sanitize(v.Database), nil
	case "server":
		return sanitize(v.Server), nil
	case "job":
		return sanitize(v.Job), nil
	}
	val, ok := formatDate(name, v.Time)
	if !ok {
		return "", fmt.Errorf("dosya adı şablonunda bilinmeyen yer tutucu: {%s}", name)
	}
	return val, nil
}

// formatDate, "yyyyMMdd" gibi bir ifadeyi verilen zamana göre biçimlendirir.
// Belirteçler Go zaman biçimine çevrilmeden tek tek biçimlendirilir; böylece
// "_2", ".000" gibi Go'ya özel diziler yanlış yorumlanmaz.
func formatDate(s string, t time.Time) (string, bool) {
	var b strings.Builder
	hasToken := false
outer:
	for s != "" {
		for _, d := range dateTokens {
			if strings.HasPrefix(s, d.token) {
				b.WriteString(t.Format(d.layout))
				s = s[len(d.token):]
				hasToken = true
				continue outer
			}
		}
		if strings.IndexByte(dateSeparators, s[0]) < 0 {
			return "", false
		}
		b.WriteByte(s[0])
		s = s[1:]
	}
	return b.String(), hasToken
}

// sanitize, değeri dosya adında kullanılabilir hale getirir.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(invalidFileChars, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
}
