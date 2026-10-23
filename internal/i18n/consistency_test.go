package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// keyLiteral, kaynak koddaki çeviri anahtarı biçimindeki string sabitleridir.
// Anahtarlar her zaman sabit olarak yazılmalı (dinamik üretilmemeli) ki bu
// test hepsini bulabilsin.
var keyLiteral = regexp.MustCompile(`"((?:err|warn|log|reason|summary|cli|common|gui)\.[A-Za-z0-9_.]+)"`)

// guiLiteral, frontend kaynaklarındaki anahtarlardır: t('gui.x'), data-i18n="gui.x".
var guiLiteral = regexp.MustCompile(`['"](gui\.[A-Za-z0-9_.]+)['"]`)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// usedKeys, test dışı Go dosyalarında ve frontend/src altındaki .ts/.html
// dosyalarında geçen anahtarları döner.
func usedKeys(t *testing.T) map[string][]string {
	t.Helper()
	root := repoRoot()
	used := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "build", ".git", "wailsjs", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		re := keyLiteral
		switch {
		case strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go"):
		case strings.HasPrefix(rel, filepath.Join("frontend", "src")) &&
			(strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".html")):
			re = guiLiteral
		default:
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			// Yorumlardaki örnekler sayılmaz.
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				used[m[1]] = append(used[m[1]], rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return used
}

func loadLangFiles(t *testing.T) map[string]map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot(), "lang", "*.lng"))
	if err != nil || len(files) == 0 {
		t.Fatalf("dil dosyası bulunamadı: %v", err)
	}
	out := map[string]map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m, err := Parse(data)
		if err != nil {
			t.Errorf("%s ayrıştırılamadı: %v", filepath.Base(f), err)
		}
		code := strings.TrimSuffix(filepath.Base(f), ".lng")
		if m["@code"] != code || m["@name"] == "" {
			t.Errorf("%s: @code (%q) dosya adıyla aynı olmalı ve @name dolu olmalı", filepath.Base(f), m["@code"])
		}
		out[code] = m
	}
	return out
}

func TestEmbeddedEnglishMatchesFile(t *testing.T) {
	langs := loadLangFiles(t)
	if len(english) == 0 || len(english) != len(langs["en"]) {
		t.Errorf("gömülü İngilizce (%d anahtar) lang/en.lng (%d) ile aynı olmalı", len(english), len(langs["en"]))
	}
}

func TestUsedKeysExist(t *testing.T) {
	en := loadLangFiles(t)["en"]
	used := usedKeys(t)
	var missing []string
	for k, where := range used {
		if _, ok := en[k]; !ok {
			missing = append(missing, k+" ("+strings.Join(where, ", ")+")")
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("en.lng'de yok: %s", m)
	}

	var unused []string
	for k := range en {
		if strings.HasPrefix(k, "@") {
			continue
		}
		if _, ok := used[k]; !ok {
			unused = append(unused, k)
		}
	}
	sort.Strings(unused)
	for _, k := range unused {
		t.Errorf("en.lng'de kullanılmayan anahtar: %s", k)
	}
}

func TestLanguagesMatchEnglish(t *testing.T) {
	langs := loadLangFiles(t)
	en := langs["en"]
	for code, m := range langs {
		if code == "en" {
			continue
		}
		for k, v := range en {
			if strings.HasPrefix(k, "@") {
				continue
			}
			tv, ok := m[k]
			if !ok {
				t.Errorf("%s.lng: eksik anahtar %s", code, k)
				continue
			}
			want, got := Placeholders(v), Placeholders(tv)
			sort.Strings(want)
			sort.Strings(got)
			if strings.Join(want, ",") != strings.Join(got, ",") {
				t.Errorf("%s.lng: %s yer tutucuları farklı: %v, en: %v", code, k, got, want)
			}
		}
		for k := range m {
			if _, ok := en[k]; !ok && !strings.HasPrefix(k, "@") {
				t.Errorf("%s.lng: en.lng'de olmayan anahtar %s", code, k)
			}
		}
	}
}
