package archive

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile, kısmen sıkıştırılabilir test verisi yazar.
func writeFile(t *testing.T, dir, name string, size int) string {
	t.Helper()
	data := bytes.Repeat([]byte("SqlBackup test verisi "), size/22+1)[:size]
	rnd := rand.New(rand.NewSource(int64(size)))
	for i := 0; i < size; i += 7 {
		data[i] = byte(rnd.Intn(256))
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreateAndVerify(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeFile(t, dir, "Satış_FULL.bak", 3<<20),
		writeFile(t, dir, "Stok_FULL.bak", 100),
		writeFile(t, dir, "Boş.bak", 0),
	}
	zipPath := filepath.Join(dir, "gece.zip")
	var last, total int64
	size, err := Create(context.Background(), zipPath, files, func(d, t int64) { last, total = d, t })
	if err != nil {
		t.Fatal(err)
	}
	if size <= 0 || last != total || total != 3<<20+100 {
		t.Errorf("boyut/ilerleme: size=%d last=%d total=%d", size, last, total)
	}
	if _, err := os.Stat(zipPath + ".tmp"); !os.IsNotExist(err) {
		t.Error("geçici dosya kalmamalı")
	}
	if err := Verify(context.Background(), zipPath, files); err != nil {
		t.Fatal(err)
	}
	// Kaynak dosyalara dokunulmamalı.
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Error(err)
		}
	}
	// Eksik dosya listesiyle doğrulama başarısız olmalı.
	if err := Verify(context.Background(), zipPath, files[:1]); err == nil {
		t.Error("girdi sayısı uyuşmazlığı yakalanmalı")
	}
}

func TestVerifyDetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	files := []string{writeFile(t, dir, "a.bak", 1<<20)}
	zipPath := filepath.Join(dir, "a.zip")
	if _, err := Create(context.Background(), zipPath, files, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// Sıkıştırılmış verinin ortasındaki bir baytı boz.
	data[len(data)/2] ^= 0xFF
	if err := os.WriteFile(zipPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(context.Background(), zipPath, files); err == nil {
		t.Fatal("bozuk zip yakalanmalı")
	} else {
		t.Log(err)
	}
}

func TestCreateErrors(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.bak", 10)
	sub := filepath.Join(dir, "alt")
	os.Mkdir(sub, 0o755)
	a2 := writeFile(t, sub, "A.BAK", 10)

	if _, err := Create(context.Background(), filepath.Join(dir, "x.zip"), nil, nil); err == nil {
		t.Error("boş liste hata vermeli")
	}
	if _, err := Create(context.Background(), filepath.Join(dir, "x.zip"), []string{a, filepath.Join(dir, "yok.bak")}, nil); err == nil {
		t.Error("olmayan dosya hata vermeli")
	}
	if _, err := Create(context.Background(), filepath.Join(dir, "x.zip"), []string{a, a2}, nil); err == nil {
		t.Error("aynı adlı dosyalar hata vermeli")
	}
	if _, err := Create(context.Background(), filepath.Join(dir, "yok", "x.zip"), []string{a}, nil); err == nil {
		t.Error("olmayan hedef klasör hata vermeli")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".zip") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("artık dosya kaldı: %s", e.Name())
		}
	}
}

func TestCreateCancel(t *testing.T) {
	dir := t.TempDir()
	files := []string{writeFile(t, dir, "a.bak", 4<<20)}
	ctx, cancel := context.WithCancel(context.Background())
	zipPath := filepath.Join(dir, "a.zip")
	_, err := Create(ctx, zipPath, files, func(done, _ int64) {
		if done > 1<<20 {
			cancel()
		}
	})
	if err == nil || !strings.Contains(err.Error(), "iptal") {
		t.Fatalf("iptal hatası bekleniyordu: %v", err)
	}
	for _, p := range []string{zipPath, zipPath + ".tmp"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s kalmamalı", p)
		}
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Error("kaynak dosya silinmemeli")
	}
}

func TestCheckAccess(t *testing.T) {
	dir := t.TempDir()
	if err := CheckAccess(dir); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("test dosyası kalmamalı: %v", entries)
	}
	if err := CheckAccess(filepath.Join(dir, "yok")); err == nil || !strings.Contains(err.Error(), "bulunamadı") {
		t.Errorf("olmayan klasör: %v", err)
	}
	f := writeFile(t, dir, "dosya.bak", 1)
	if err := CheckAccess(f); err == nil || !strings.Contains(err.Error(), "klasör değil") {
		t.Errorf("dosya yolu: %v", err)
	}
}
