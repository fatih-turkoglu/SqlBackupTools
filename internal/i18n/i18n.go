// Package i18n, kullanıcıya görünen tüm metinleri dil dosyalarından sağlar.
//
// Dil dosyaları exe'nin yanındaki lang\<kod>.lng dosyalarıdır (bkz. Parse).
// Varsayılan dil İngilizce'dir ve exe'ye gömülüdür; lang\ klasörü yoksa
// program yine İngilizce çalışır. Bir anahtar seçili dilde yoksa İngilizce,
// orada da yoksa anahtarın kendisi döner.
//
// Metinlerde yer tutucular adlandırılmıştır: T("backup.db.failed", "db", ad, "err", err)
// → "{db}" ve "{err}" yerine değerler yazılır.
package i18n

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"sqlbackup/lang"
)

// Default, varsayılan dil kodudur.
const Default = "en"

// DirEnv, dil klasörünü değiştirmek için kullanılan ortam değişkenidir (dev/test).
const DirEnv = "SQLBACKUP_LANG_DIR"

var (
	mu      sync.RWMutex
	dir     string // boşsa Dir() varsayılanı
	code    = Default
	current map[string]string // seçili dil (İngilizce seçiliyse diskteki en.lng)
	english map[string]string // gömülü İngilizce
	missing = map[string]bool{}
	// systemLanguages, işletim sisteminin dil etiketlerini tercih sırasıyla döner.
	systemLanguages = windowsLanguages
)

func init() {
	english, _ = Parse(lang.English)
}

// Dir, dil dosyalarının klasörünü döner: SetDir ile verilen, yoksa
// SQLBACKUP_LANG_DIR, yoksa exe'nin yanındaki lang klasörü.
func Dir() string {
	mu.RLock()
	d := dir
	mu.RUnlock()
	if d != "" {
		return d
	}
	if d := os.Getenv(DirEnv); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		return "lang"
	}
	return filepath.Join(filepath.Dir(exe), "lang")
}

// SetDir, dil klasörünü değiştirir (testler için). Seçili dil yeniden yüklenmez.
func SetDir(d string) {
	mu.Lock()
	dir = d
	mu.Unlock()
}

// SetSystemLanguages, Detect'in kullandığı sistem dili kaynağını değiştirir
// (testler için; nil → Windows dilleri).
func SetSystemLanguages(f func() []string) {
	if f == nil {
		f = windowsLanguages
	}
	mu.Lock()
	systemLanguages = f
	mu.Unlock()
}

// Detect, Windows arayüz diline uyan kullanılabilir dilin kodunu döner:
// tercih edilen her dil için önce tam etiket (pt-br.lng), sonra ana dil
// (pt.lng) aranır. Uyan dil yoksa İngilizce.
func Detect() string {
	mu.RLock()
	f := systemLanguages
	mu.RUnlock()
	available := map[string]bool{}
	for _, l := range Languages() {
		available[l.Code] = true
	}
	for _, tag := range f() {
		tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
		base, _, _ := strings.Cut(tag, "-")
		for _, c := range []string{tag, base} {
			if available[c] {
				return c
			}
		}
	}
	return Default
}

// ErrUnknownLanguage, istenen dilin dosyası bulunamadığında döner.
var ErrUnknownLanguage = errors.New("unknown language")

// SetLanguage, dili değiştirir. Kod boşsa İngilizce seçilir. Dosya
// bulunamazsa İngilizce seçilir ve ErrUnknownLanguage döner; dosyada bozuk
// satırlar varsa dil yine seçilir, satır hataları döner.
func SetLanguage(c string) error {
	c = strings.ToLower(strings.TrimSpace(c))
	if c == "" {
		c = Default
	}
	if !validCode(c) {
		setCurrent(Default, nil)
		return fmt.Errorf("%w: %q", ErrUnknownLanguage, c)
	}
	data, err := os.ReadFile(filepath.Join(Dir(), c+".lng"))
	if err != nil {
		setCurrent(Default, nil)
		if c == Default {
			return nil // gömülü İngilizce yeterli
		}
		return fmt.Errorf("%w: %q (%s)", ErrUnknownLanguage, c, Dir())
	}
	m, perr := Parse(data)
	setCurrent(c, m)
	if perr != nil {
		return fmt.Errorf("%s.lng: %w", c, perr)
	}
	return nil
}

func setCurrent(c string, m map[string]string) {
	mu.Lock()
	code, current = c, m
	mu.Unlock()
}

// validCode, dil kodunun dosya adı olarak güvenli olduğunu kontrol eder (ör. en, tr, pt-br).
func validCode(c string) bool {
	if len(c) < 2 || len(c) > 10 {
		return false
	}
	for _, r := range c {
		if !(r >= 'a' && r <= 'z' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// Language, seçili dil kodunu döner.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return code
}

// LanguageInfo, seçilebilir bir dildir.
type LanguageInfo struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Languages, kullanılabilir dilleri döner: lang klasöründeki .lng dosyaları
// ve her zaman İngilizce. Ad dosyadaki @name başlığından okunur.
func Languages() []LanguageInfo {
	found := map[string]string{Default: english["@name"]}
	if entries, err := os.ReadDir(Dir()); err == nil {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.EqualFold(filepath.Ext(name), ".lng") {
				continue
			}
			c := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
			if !validCode(c) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(Dir(), name))
			if err != nil {
				continue
			}
			m, _ := Parse(data)
			if n := m["@name"]; n != "" {
				found[c] = n
			} else if found[c] == "" {
				found[c] = c
			}
		}
	}
	out := make([]LanguageInfo, 0, len(found))
	for c, n := range found {
		out = append(out, LanguageInfo{Code: c, Name: n})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Code < out[k].Code })
	return out
}

// lookup, anahtarın metnini döner: seçili dil → gömülü İngilizce → anahtar.
func lookup(key string) string {
	mu.RLock()
	v, ok := current[key]
	if !ok {
		v, ok = english[key]
	}
	mu.RUnlock()
	if ok {
		return v
	}
	mu.Lock()
	missing[key] = true
	mu.Unlock()
	return key
}

// Missing, şimdiye kadar hiçbir dilde bulunamayan anahtarları döner
// (loglamak için; normalde boş olmalıdır, testler bunu garanti eder).
func Missing() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(missing))
	for k := range missing {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// T, anahtarın seçili dildeki metnini yer tutucuları doldurarak döner.
// args ad/değer çiftleridir; değerler fmt.Sprint ile yazılır.
func T(key string, args ...any) string {
	return format(lookup(key), args)
}

func format(s string, args []any) string {
	if len(args) < 2 || !strings.Contains(s, "{") {
		return s
	}
	pairs := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok {
			continue
		}
		pairs = append(pairs, "{"+name+"}", fmt.Sprint(args[i+1]))
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// All, frontend için seçili dilin tüm metinlerini döner (eksikler İngilizce'den).
func All() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]string, len(english))
	for k, v := range english {
		out[k] = v
	}
	for k, v := range current {
		out[k] = v
	}
	return out
}

// Error, metni gösterim anında seçili dilde üretilen bir hatadır. Error
// türündeki argümanlar sarmalanır; errors.Is/As onlar üzerinden çalışır.
type Error struct {
	Key  string
	Args []any
}

// Errorf, anahtar ve ad/değer çiftlerinden bir hata oluşturur.
func Errorf(key string, args ...any) error {
	return &Error{Key: key, Args: args}
}

func (e *Error) Error() string { return T(e.Key, e.Args...) }

// Unwrap, argümanlardaki hataları döner.
func (e *Error) Unwrap() []error {
	var out []error
	for i := 1; i < len(e.Args); i += 2 {
		if err, ok := e.Args[i].(error); ok && err != nil {
			out = append(out, err)
		}
	}
	return out
}
