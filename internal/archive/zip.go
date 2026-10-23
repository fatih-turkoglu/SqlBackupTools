// Package archive, .bak dosyalarını zip'ler ve doğrular.
package archive

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ProgressFunc, zip ilerlemesini bayt cinsinden bildirir.
type ProgressFunc func(done, total int64)

// CheckAccess, programın klasörde okuma, yazma ve silme yapabildiğini
// gerçekten deneyerek kontrol eder. Yedek dosyalarını SQL Server servis
// hesabı yazdığı için programı çalıştıran hesabın erişimi olmayabilir.
func CheckAccess(dir string) error {
	st, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("klasör bulunamadı: %s", dir)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("klasöre erişim izni yok: %s", dir)
	case err != nil:
		return fmt.Errorf("klasöre erişilemiyor: %w", err)
	case !st.IsDir():
		return fmt.Errorf("yol bir klasör değil: %s", dir)
	}

	f, err := os.CreateTemp(dir, ".sqlbackup-access-*.tmp")
	if err != nil {
		return fmt.Errorf("klasöre yazma izni yok (%s): %w", dir, unwrapPath(err))
	}
	name := f.Name()
	_, werr := f.WriteString("SqlBackup erişim testi")
	f.Close()
	if werr != nil {
		os.Remove(name)
		return fmt.Errorf("klasöre yazılamıyor (%s): %w", dir, unwrapPath(werr))
	}
	if _, err := os.ReadFile(name); err != nil {
		os.Remove(name)
		return fmt.Errorf("klasörden okuma izni yok (%s): %w", dir, unwrapPath(err))
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("klasörde silme izni yok (%s): %w", dir, unwrapPath(err))
	}
	return nil
}

// Create, dosyaları zipPath'e Deflate ile sıkıştırır. Zip içinde dosyalar
// yalnızca adlarıyla yer alır. Önce geçici dosyaya yazılır, Verify ile
// doğrulanır ve ancak bundan sonra asıl ada taşınır; herhangi bir hatada
// geçici dosya silinir ve kaynak dosyalara dokunulmaz. Dönen değer zip
// dosyasının boyutudur.
func Create(ctx context.Context, zipPath string, files []string, progress ProgressFunc) (int64, error) {
	if len(files) == 0 {
		return 0, errors.New("zip'lenecek dosya yok")
	}
	var total int64
	seen := map[string]bool{}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			return 0, fmt.Errorf("yedek dosyası okunamadı: %w", err)
		}
		key := strings.ToLower(filepath.Base(f))
		if seen[key] {
			return 0, fmt.Errorf("zip içinde aynı adda iki dosya olamaz: %s", filepath.Base(f))
		}
		seen[key] = true
		total += st.Size()
	}

	tmp := zipPath + ".tmp"
	if err := writeZip(ctx, tmp, files, total, progress); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err := Verify(ctx, tmp, files); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, zipPath); err != nil {
		os.Remove(tmp)
		return 0, fmt.Errorf("zip dosyası adlandırılamadı: %w", err)
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func writeZip(ctx context.Context, path string, files []string, total int64, progress ProgressFunc) (err error) {
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("zip dosyası oluşturulamadı: %w", err)
	}
	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("zip dosyası yazılamadı: %w", cerr)
		}
	}()

	zw := zip.NewWriter(out)
	var done int64
	for _, f := range files {
		if err := addFile(ctx, zw, f, func(n int64) {
			done += n
			if progress != nil {
				progress(done, total)
			}
		}); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("zip dosyası yazılamadı: %w", err)
	}
	return nil
}

func addFile(ctx context.Context, zw *zip.Writer, path string, onWrite func(int64)) error {
	in, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("yedek dosyası okunamadı: %w", err)
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(st)
	if err != nil {
		return err
	}
	h.Name = filepath.Base(path)
	h.Method = zip.Deflate
	w, err := zw.CreateHeader(h)
	if err != nil {
		return fmt.Errorf("zip girdisi oluşturulamadı: %w", err)
	}
	r := &ctxReader{ctx: ctx, r: in, onRead: onWrite}
	if _, err := io.Copy(w, r); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("zip iptal edildi: %w", ctx.Err())
		}
		return fmt.Errorf("%s zip'lenemedi: %w", h.Name, err)
	}
	return nil
}

// Verify, zip'i açar ve her girdiyi sonuna kadar okur; archive/zip okuma
// sonunda CRC32'yi kontrol eder. Ayrıca girdilerin kaynak dosyalarla aynı
// ad ve boyutta olduğunu doğrular.
func Verify(ctx context.Context, zipPath string, files []string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("zip doğrulanamadı: %w", err)
	}
	defer zr.Close()

	want := map[string]int64{}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			return fmt.Errorf("zip doğrulanamadı: %w", err)
		}
		want[filepath.Base(f)] = st.Size()
	}
	if len(zr.File) != len(want) {
		return fmt.Errorf("zip doğrulanamadı: %d girdi bekleniyordu, %d bulundu", len(want), len(zr.File))
	}
	for _, zf := range zr.File {
		size, ok := want[zf.Name]
		if !ok {
			return fmt.Errorf("zip doğrulanamadı: beklenmeyen girdi %s", zf.Name)
		}
		if int64(zf.UncompressedSize64) != size {
			return fmt.Errorf("zip doğrulanamadı: %s boyutu uyuşmuyor (%d / %d)", zf.Name, zf.UncompressedSize64, size)
		}
		rc, err := zf.Open()
		if err != nil {
			return fmt.Errorf("zip doğrulanamadı (%s): %w", zf.Name, err)
		}
		n, err := io.Copy(io.Discard, &ctxReader{ctx: ctx, r: rc})
		rc.Close()
		if err != nil {
			return fmt.Errorf("zip doğrulanamadı (%s): %w", zf.Name, err)
		}
		if n != size {
			return fmt.Errorf("zip doğrulanamadı: %s okunan boyut uyuşmuyor", zf.Name)
		}
	}
	return nil
}

// ctxReader, büyük dosyalarda iptalin kopyalama sırasında da çalışmasını sağlar.
type ctxReader struct {
	ctx    context.Context
	r      io.Reader
	onRead func(int64)
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if n > 0 && c.onRead != nil {
		c.onRead(int64(n))
	}
	return n, err
}

// unwrapPath, *PathError içindeki asıl hatayı döner; mesajda yol iki kez
// görünmesin diye.
func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
