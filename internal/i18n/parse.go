package i18n

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Parse, bir .lng dosyasının içeriğini ayrıştırır.
//
// Biçim: UTF-8 (BOM'lu veya BOM'suz), satır başına "anahtar = değer".
// Boşluk/sekme ile başlayan veya boş satırlar ve "#" ile başlayan satırlar
// yok sayılır. Değerde "\n" satır sonu, "\t" sekme, "\\" ters bölü olur.
// "@code" ve "@name" başlık anahtarlarıdır.
//
// Bozuk ve tekrarlanan satırlar atlanır (tekrarda ilk değer kalır) ve
// hepsi birlikte hata olarak döner; geçerli satırlar yine kullanılabilir.
func Parse(data []byte) (map[string]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	out := map[string]string{}
	var errs []error
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			errs = append(errs, fmt.Errorf("line %d: invalid entry: %q", n, line))
			continue
		}
		if _, dup := out[key]; dup {
			errs = append(errs, fmt.Errorf("line %d: duplicate key: %s", n, key))
			continue
		}
		v, err := unescape(strings.TrimSpace(val))
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %s: %w", n, key, err))
			continue
		}
		out[key] = v
	}
	if err := sc.Err(); err != nil {
		errs = append(errs, err)
	}
	return out, errors.Join(errs...)
}

func unescape(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			return "", errors.New(`trailing '\'`)
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		default:
			return "", fmt.Errorf(`unknown escape '\%c'`, s[i])
		}
	}
	return b.String(), nil
}

// Placeholders, metindeki {ad} yer tutucularını sıralı ve tekrarsız döner.
func Placeholders(s string) []string {
	var out []string
	seen := map[string]bool{}
	for {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			return out
		}
		end := strings.IndexByte(s[open:], '}')
		if end < 0 {
			return out
		}
		name := s[open+1 : open+end]
		if isPlaceholderName(name) && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		s = s[open+1:]
	}
}

func isPlaceholderName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}
