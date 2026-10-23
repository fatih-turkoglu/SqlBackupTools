package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"sqlbackup/internal/config"
)

// RetentionResult, saklama kuralının uygulanma sonucudur.
type RetentionResult struct {
	Mode      string        `json:"mode"`
	Value     int           `json:"value"`
	Deleted   []DeletedFile `json:"deleted"`
	ElapsedMs int64         `json:"elapsedMs"`
	Error     string        `json:"error,omitempty"`
}

// DeletedFile, saklama kuralıyla silinen bir dosyadır.
type DeletedFile struct {
	// File, erişim yolu (accessPath) üzerindeki tam yoldur.
	File  string    `json:"file"`
	Bytes int64     `json:"bytes"`
	Time  time.Time `json:"time"` // dosya adından okunan yedek zamanı
}

// errNoDate: şablonda tarih yoksa her çalışma aynı dosyanın üzerine yazar;
// biriken dosya olmadığı için saklama kuralı uygulanmaz (uyarı verilir).
var errNoDate = errors.New("dosya adı şablonunda tarih (yıl, ay ve gün) olmadığı için saklama kuralı uygulanmadı")

// retentionPattern, işin dosya adı şablonundan üretilen eşleştiricidir.
// Şablonun ürettiği adlara ek olarak çakışma eki (_2, _3) ve .bak/.zip
// uzantıları kabul edilir (bkz. uniqueFileName, zip adlandırması).
type retentionPattern struct {
	re *regexp.Regexp
	// dbGroups, {db} yer tutucularının grup indeksleridir (boşsa şablonda {db} yok).
	dbGroups []int
	// dateGroups, tarih belirteçlerinin grup indeksleri ve belirteçleridir.
	dateGroups []dateGroup
}

type dateGroup struct {
	index int
	token string
}

// newRetentionPattern, şablondan eşleştirici üretir. {db} yalnızca series
// içindeki adlarla eşleşir; {server} ve {job} sabit değerlerle eşleşir.
// Şablonda yıl, ay ve gün belirteçlerinin hepsi yoksa dosyaların zamanı
// belirlenemeyeceği için hata döner.
func newRetentionPattern(tmpl, server, job string, series []string) (*retentionPattern, error) {
	body := strings.TrimSpace(tmpl)
	if strings.HasSuffix(strings.ToLower(body), ".bak") {
		body = body[:len(body)-len(".bak")]
	}

	// Uzun adlar önce denensin (ör. "Satis_Arsiv", "Satis"'ten önce).
	names := make([]string, 0, len(series))
	for _, s := range series {
		names = append(names, regexp.QuoteMeta(sanitize(s)))
	}
	sort.Slice(names, func(i, k int) bool { return len(names[i]) > len(names[k]) })
	dbExpr := "(" + strings.Join(names, "|") + ")"

	p := &retentionPattern{}
	var b strings.Builder
	b.WriteString("(?i)^")
	group := 0
	tokens := map[string]bool{}
	err := walkTemplate(body, func(lit string) { b.WriteString(regexp.QuoteMeta(lit)) }, func(name string) error {
		switch name {
		case "db":
			group++
			p.dbGroups = append(p.dbGroups, group)
			b.WriteString(dbExpr)
			return nil
		case "server":
			b.WriteString(regexp.QuoteMeta(sanitize(server)))
			return nil
		case "job":
			b.WriteString(regexp.QuoteMeta(sanitize(job)))
			return nil
		}
		if _, ok := formatDate(name, time.Time{}); !ok {
			return fmt.Errorf("dosya adı şablonunda bilinmeyen yer tutucu: {%s}", name)
		}
		s := name
	outer:
		for s != "" {
			for _, d := range dateTokens {
				if strings.HasPrefix(s, d.token) {
					group++
					p.dateGroups = append(p.dateGroups, dateGroup{group, d.token})
					tokens[d.token] = true
					fmt.Fprintf(&b, `(\d{%d})`, len(d.layout))
					s = s[len(d.token):]
					continue outer
				}
			}
			b.WriteString(regexp.QuoteMeta(s[:1]))
			s = s[1:]
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !(tokens["yyyy"] || tokens["yy"]) || !tokens["MM"] || !tokens["dd"] {
		return nil, errNoDate
	}
	b.WriteString(`(?:_\d+)?\.(?:bak|zip)$`)
	if p.re, err = regexp.Compile(b.String()); err != nil {
		return nil, err
	}
	return p, nil
}

// match, dosya adı işe aitse serisini (küçük harf DB adı; şablonda {db}
// yoksa boş) ve adından okunan yedek zamanını döner.
func (p *retentionPattern) match(name string) (series string, t time.Time, ok bool) {
	m := p.re.FindStringSubmatch(name)
	if m == nil {
		return "", time.Time{}, false
	}
	for i, g := range p.dbGroups {
		v := strings.ToLower(m[g])
		if i == 0 {
			series = v
		} else if v != series {
			return "", time.Time{}, false
		}
	}

	// Aynı belirteç birden çok kez geçerse ilki kullanılır.
	vals := map[string]int{}
	for _, d := range p.dateGroups {
		if _, seen := vals[d.token]; seen {
			continue
		}
		n, _ := strconv.Atoi(m[d.index])
		vals[d.token] = n
	}
	year, ok := vals["yyyy"]
	if !ok {
		year = 2000 + vals["yy"]
	}
	month, day := vals["MM"], vals["dd"]
	t = time.Date(year, time.Month(month), day, vals["HH"], vals["mm"], vals["ss"], 0, time.Local)
	// Geçersiz tarihler (ör. 13. ay) normalize edilir; bu durumda dosya bizim değildir.
	if t.Month() != time.Month(month) || t.Day() != day || t.Hour() != vals["HH"] ||
		t.Minute() != vals["mm"] || t.Second() != vals["ss"] {
		return "", time.Time{}, false
	}
	return series, t, true
}

// retentionFile, işe ait olduğu belirlenen bir dosyadır.
type retentionFile struct {
	Path   string
	Series string
	Time   time.Time
	Size   int64
}

// expiredFiles, saklama kuralına göre silinmesi gereken dosyaları döner.
//   - days: zamanı now'dan value günden daha eski olanlar
//   - count: her seride en yeni value çalışma (zaman damgası) tutulur; aynı
//     çalışmanın .bak ve .zip dosyaları tek çalışma sayılır
//
// protect içindeki yollar (bu çalışmanın dosyaları) hiçbir zaman silinmez.
func expiredFiles(files []retentionFile, mode string, value int, now time.Time, protect map[string]bool) []retentionFile {
	var out []retentionFile
	keep := func(f retentionFile) bool { return protect[strings.ToLower(f.Path)] }
	switch mode {
	case config.RetentionDays:
		cutoff := now.AddDate(0, 0, -value)
		for _, f := range files {
			if f.Time.Before(cutoff) && !keep(f) {
				out = append(out, f)
			}
		}
	case config.RetentionCount:
		times := map[string][]time.Time{}
		for _, f := range files {
			times[f.Series] = append(times[f.Series], f.Time)
		}
		kept := map[string]map[time.Time]bool{}
		for s, ts := range times {
			sort.Slice(ts, func(i, k int) bool { return ts[i].After(ts[k]) })
			kept[s] = map[time.Time]bool{}
			for _, t := range ts {
				if len(kept[s]) >= value && !kept[s][t] {
					break
				}
				kept[s][t] = true
			}
		}
		for _, f := range files {
			if !kept[f.Series][f.Time] && !keep(f) {
				out = append(out, f)
			}
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Path < out[k].Path })
	return out
}

// retention, saklama kuralını erişim yolu üzerinde uygular. Yalnızca bu
// çalışmada başarıyla yedeklenen veritabanlarının (tek zip'te iş adının)
// dosyalarına dokunulur; böylece yedeği alınamayan bir veritabanının eski
// yedekleri silinmez ve aynı klasörü kullanan başka işlerin dosyaları
// (farklı DB'ler) etkilenmez.
func (rn *runner) retention(ctx context.Context) {
	j := rn.job
	if ctx.Err() != nil {
		rn.warn("işlem iptal edildi; saklama kuralı uygulanmadı")
		return
	}

	var series []string
	protect := map[string]bool{}
	dir := j.Backup.EffectiveAccessPath()
	for _, d := range rn.res.Databases {
		if d.Status == StatusSuccess {
			series = append(series, d.Database)
		}
		if d.File != "" {
			protect[strings.ToLower(filepath.Join(dir, serverBaseName(d.File)))] = true
		}
	}
	for _, a := range rn.res.Archives {
		protect[strings.ToLower(a.File)] = true
	}
	if len(series) == 0 {
		rn.log.Warn("başarılı yedek olmadığı için saklama kuralı uygulanmadı")
		return
	}
	if j.Archive.ZipEnabled && !j.Archive.ZipPerDatabase {
		series = append(series, j.Name)
	}

	rt := &RetentionResult{Mode: j.Retention.Mode, Value: j.Retention.Value, Deleted: []DeletedFile{}}
	rn.res.Retention = rt
	start := rn.now()
	defer func() { rt.ElapsedMs = rn.now().Sub(start).Milliseconds() }()

	if err := rn.applyRetention(dir, series, protect, rt); err != nil {
		rt.Error = err.Error()
		rn.log.Error("saklama kuralı uygulanamadı", "error", err)
		return
	}
	rn.log.Info("saklama kuralı uygulandı", "mode", rt.Mode, "value", rt.Value, "deleted", len(rt.Deleted))
}

func (rn *runner) applyRetention(dir string, series []string, protect map[string]bool, rt *RetentionResult) error {
	j := rn.job
	if rn.accessErr != nil {
		return rn.accessErr
	}
	p, err := newRetentionPattern(j.Backup.FileNameTemplate, rn.res.Server, j.Name, series)
	if errors.Is(err, errNoDate) {
		rn.warn(err.Error())
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("yedek klasörü okunamadı: %w", err)
	}

	var files []retentionFile
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		s, t, ok := p.match(e.Name())
		if !ok {
			continue
		}
		f := retentionFile{Path: filepath.Join(dir, e.Name()), Series: s, Time: t}
		if info, err := e.Info(); err == nil {
			f.Size = info.Size()
		}
		files = append(files, f)
	}

	var errs []error
	for _, f := range expiredFiles(files, j.Retention.Mode, j.Retention.Value, rn.res.Start, protect) {
		if err := os.Remove(f.Path); err != nil {
			errs = append(errs, err)
			continue
		}
		rt.Deleted = append(rt.Deleted, DeletedFile{File: f.Path, Bytes: f.Size, Time: f.Time})
		rn.log.Info("eski yedek silindi", "file", f.Path, "size", formatBytes(f.Size),
			"time", f.Time.Format("2006-01-02 15:04:05"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("bazı eski yedekler silinemedi: %w", errors.Join(errs...))
	}
	return nil
}
