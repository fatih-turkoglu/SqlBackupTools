# SqlBackup – Geliştirme Planı

Windows üzerinde çalışan, Go + Wails tabanlı, koyu tema destekli MSSQL otomatik yedekleme programı.

**Kararlar**
- Arayüz: **Wails v2** (Go + WebView2, frontend: Vite + TypeScript)
- Sunucu: Yerel **ve uzak** SQL Server desteklenir
- Yedek tipi: **Sadece FULL** (ilk sürüm)
- Ek işlem: **ZIP** (opsiyonel) + ZIP sonrası `.bak` silme (opsiyonel). E-posta yok.
- Çalışma modları: Tek `SqlBackup.exe` → argümansız = GUI, `run --job <dosya>` = CLI (penceresiz)

**Uzak sunucu notu:** `BACKUP TO DISK` dosyayı SQL Server makinesine yazar. Bu yüzden her işte iki yol tutulur:
- `serverPath` → SQL Server'ın gördüğü yol (ör. `D:\Backups` veya `\\nas\sqlbak`)
- `accessPath` → Bu programın aynı klasöre eriştiği yol (ör. `\\sqlsrv01\Backups$` veya `\\nas\sqlbak`). Yerel sunucuda boş bırakılırsa `serverPath` kullanılır.
ZIP, `.bak` silme ve retention işlemleri `accessPath` üzerinden yapılır.

---

## Faz 0 – Proje İskeleti

### 0.1 Ortam
- [x] Go (1.22+), Node.js (LTS), Wails CLI kurulumu (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
- [x] `wails doctor` ile ortam kontrolü
- [x] WebView2 runtime kontrolü

### 0.2 Proje oluşturma
- [x] `wails init -n SqlBackup -t vanilla-ts` ile proje iskeleti
- [x] `go.mod` modül adı ve bağımlılıklar (`go-mssqldb`, `golang.org/x/sys/windows`)
- [x] Klasör yapısı:
  ```
  main.go, app.go
  internal/config, internal/mssql, internal/backup,
  internal/archive, internal/scheduler, internal/logging, internal/cli
  frontend/
  ```
- [x] `.gitignore`, `git init`, ilk commit

---

## Faz 1 – Çekirdek (GUI olmadan uçtan uca yedek)

### 1.1 Config modeli (`internal/config`)
- [x] `Job` struct: `name`, `connection`, `databases`, `excludedDatabases`, `includeNewDatabases`, `excludeSystemDatabases`, `backup`, `archive`, `retention`, `schedule`
- [x] `Connection`: `server`, `port`, `protocol`, `instance`, `auth (sql|windows)`, `user`, `passwordEnc`, `encrypt`, `trustServerCert`, `timeoutSec`
- [x] `Backup`: `serverPath`, `accessPath`, `fileNameTemplate`, `compression`, `checksum`, `copyOnly`, `verify`
- [x] `Archive`: `zipEnabled`, `deleteBakAfterZip`, `zipPerDatabase (tek zip / DB başına zip)`
- [x] `Retention`: `mode (days|count|none)`, `value`
- [x] JSON okuma/yazma, doğrulama (validate) fonksiyonu
- [x] Uygulama veri klasörü: `%ProgramData%\SqlBackup\{jobs,logs}`

### 1.2 Şifre saklama – DPAPI
- [x] `CryptProtectData` / `CryptUnprotectData` sarmalayıcı (`CRYPTPROTECT_LOCAL_MACHINE` kapsamı)
- [x] Config'e yalnızca şifreli + base64 değer yazılması
- [x] Birim testi: şifrele → çöz eşitliği

### 1.3 MSSQL katmanı (`internal/mssql`)
- [x] Bağlantı dizesi üretimi (SQL Auth + Windows Auth/SSPI, instance/port, encrypt seçenekleri)
- [x] Protokol seçimi: otomatik (yerel: Shared Memory → Named Pipes → TCP, uzak: TCP → Named Pipes) veya zorunlu `tcp|np|lpc`; SQL Browser kapalıyken de bağlanır
- [x] `TestConnection()` → sunucu sürümü ve edition bilgisi döner
- [x] `ListDatabases()` → ad, durum, recovery model, boyut (MB), sistem DB mi
- [x] `BackupDatabase(db, file, opts)` → `BACKUP DATABASE ... TO DISK ... WITH INIT, FORMAT, COMPRESSION?, CHECKSUM?, COPY_ONLY?, STATS=5`
- [x] `STATS` mesajlarını yakalayıp ilerleme callback'i (`func(db string, pct int)`)
- [x] `VerifyBackup(file)` → `RESTORE VERIFYONLY FROM DISK ...`
- [x] COMPRESSION desteklenmeyen edition (Express) tespiti ve uyarı
- [x] Uzun süreli komutlar için context + timeout yönetimi

### 1.4 Yedek çalıştırıcı (`internal/backup`)
- [x] Sıkıştırma desteklenmiyorsa (Express/Web) COMPRESSION kapatılıp uyarı loglanması
- [x] Dosya adı şablonu: `{server}`, `{db}`, `{job}`, tarih belirteçleri (`yyyy yy MM dd HH mm ss` + `-_. ` ayırıcılar, ör. `{yyyyMMdd}`, `{HHmmss}`, `{yyyy-MM-dd}`); tüm dosyalar çalışma başlangıç zamanıyla adlandırılır
- [x] Hedef DB listesinin çözümlenmesi (seçili / tümü / yeni eklenenler, sistem DB hariç, OFFLINE olanları atla)
  - Seçili modda bulunamayan/OFFLINE DB **hata** sayılır; "yeni DB'leri dahil et" modunda OFFLINE **atlanır**, tempdb sessizce çıkarılır
- [x] DB'leri sırayla yedekleme; biri hata verse de diğerlerine devam etme (doğrulama hatası da DB'yi başarısız yapar)
- [x] Çalışma sonucu özeti: DB bazında başarı/hata, süre, dosya boyutu (`msdb.backupset` üzerinden)
- [x] Exit code: `0` tümü başarılı, `1` kısmi hata, `2` tamamen başarısız/config hatası (hiçbir DB yedeklenmediyse de `2`)

### 1.5 ZIP (`internal/archive`)
- [x] `accessPath` erişilebilirlik kontrolü (yoksa anlaşılır hata) – okuma/yazma/silme gerçekten denenir; iş başında yapılır
  - Erişim yoksa yedekler **yine alınır**, yalnızca ZIP adımı hata verir (exit `1`)
- [x] `.bak` → `.zip` (archive/zip, Deflate; büyük dosyalar için stream ile, iptal destekli, zip64 otomatik)
- [x] Mod: DB başına ayrı zip (`.bak` adı → `.zip`) / tüm DB'ler tek zip (şablonda `{db}` yerine iş adı)
- [x] ZIP bütünlük kontrolü (zip'i açıp CRC + boyut doğrulama) sonrası, seçenek açıksa `.bak` silme
  - Önce `.zip.tmp`'ye yazılır, doğrulanınca asıl ada taşınır
- [x] ZIP başarısızsa `.bak` asla silinmez; doğrulaması (VERIFYONLY) başarısız yedek zip'lenmez

### 1.6 Retention
- [ ] `accessPath` içinde iş adına ait dosyaları bulma (şablona uyan `.bak` / `.zip`)
- [ ] "X günden eski" ve "son N adet tut" modları
- [ ] Silinen dosyaların loglanması

### 1.7 Loglama (`internal/logging`)
- [ ] `log/slog` ile dosyaya yazma: `logs\<job>_<yyyyMMdd>.log`
- [ ] Her çalışma için JSON sonuç kaydı (`logs\runs\<job>_<timestamp>.json`), Loglar ekranı bunu okuyacak
- [ ] Eski logların temizlenmesi (ör. 30 gün)

### 1.8 CLI (`internal/cli`)
- [ ] `main.go`: argüman varsa CLI, yoksa GUI
- [ ] `SqlBackup.exe run --job <path>`
- [ ] `SqlBackup.exe test --job <path>` (yalnızca bağlantı testi)
- [ ] `SqlBackup.exe list --job <path>` (DB listesi)
- [ ] CLI modunda konsol penceresi davranışı (GUI build'de `AttachConsole` ile çıktı)
- [ ] Faz 1 kabul testi: elle yazılmış JSON ile uzak sunucudan yedek + zip + retention

---

## Faz 2 – Batch ve Zamanlanmış Görev (`internal/scheduler`)

### 2.1 Batch dosyası
- [ ] Şablondan `.bat` üretimi (exe yolu + `run --job "..."`, `exit /b %ERRORLEVEL%`)
- [ ] Türkçe karakter / boşluk içeren yollar için doğru tırnaklama ve kod sayfası (`chcp 65001`)
- [ ] Kaydetme konumu: varsayılan `jobs\<job>.bat`, kullanıcı farklı yer seçebilir

### 2.2 Zamanlanmış görev
- [ ] Zamanlama modeli: günlük / haftalık (gün seçimi) / N saatte bir, başlangıç saati
- [ ] `schtasks /Create` komutu üretimi (`/TN "SqlBackup\<job>"`, `/RU SYSTEM` veya kullanıcı, `/RL HIGHEST`, `/F`)
- [ ] Görev sorgulama (`schtasks /Query /XML`) → var mı, sonraki çalışma, son sonuç
- [ ] Görev silme ve "Şimdi çalıştır" (`schtasks /Run`)
- [ ] Yönetici yetkisi yoksa UAC ile yükseltme (`ShellExecute runas`)
- [ ] Uyarı: SYSTEM + Windows Auth seçiminde SQL tarafında yetki gereksinimi

---

## Faz 3 – Arayüz (Wails)

### 3.1 Backend köprüsü (`app.go`)
- [ ] Job CRUD: `ListJobs`, `GetJob`, `SaveJob`, `DeleteJob`, `DuplicateJob`
- [ ] `TestConnection(conn, password)`, `ListDatabases(conn, password)`
- [ ] `RunJobNow(name)` + ilerleme için Wails event'leri (`backup:progress`, `backup:done`)
- [ ] `SaveBatch`, `CreateTask`, `DeleteTask`, `GetTaskStatus`
- [ ] `ListRuns`, `ReadLog`
- [ ] Klasör seçme diyaloğu (`runtime.OpenDirectoryDialog`)

### 3.2 Tema ve layout
- [ ] CSS değişkenleri ile koyu tema (varsayılan) + açık tema
  - bg `#0f1115`, panel `#171a21`, border `#262b36`, text `#e6e8ee`, accent `#4f8cff`, success `#3ecf8e`, error `#ff5c5c`
- [ ] Sol menü + içerik alanı layout'u, pencere başlığı ve ikon
- [ ] Ortak bileşenler: buton, input, switch, checkbox, tablo, toast bildirim, modal, ilerleme çubuğu

### 3.3 Ekranlar
- [ ] **İşler (Dashboard):** iş kartları/tablosu, son çalışma durumu, sonraki çalışma, Yeni/Düzenle/Kopyala/Sil/Şimdi Çalıştır
- [ ] **Bağlantı:** sunucu, port/instance, kimlik tipi, kullanıcı/şifre, encrypt/trust, Test Et
- [ ] **Veritabanları:** liste + çoklu seçim, boyut/durum, "tümü", "sistem DB hariç", "yeni DB'leri dahil et"
- [ ] **Yedek Ayarları:** `serverPath`, `accessPath` (+ yerel/uzak açıklaması), dosya adı şablonu + önizleme, compression/checksum/copy_only/verify
- [ ] **ZIP ve Saklama:** ZIP aç/kapa, mod, ZIP sonrası `.bak` sil, retention modu ve değeri
- [ ] **Zamanlama:** sıklık, gün, saat, çalışacak hesap; `.bat Kaydet`, `Görev Oluştur`, `Görevi Sil`, görev durumu
- [ ] **Loglar:** çalışma geçmişi tablosu, detay ve log görüntüleyici
- [ ] **Ayarlar:** tema, varsayılan klasörler, log saklama süresi

### 3.4 UX detayları
- [ ] Form doğrulama ve anlaşılır hata mesajları
- [ ] Kaydedilmemiş değişiklik uyarısı
- [ ] Uzun işlemlerde kilitlenmeyen arayüz (async + spinner)

---

## Faz 4 – Kalite, Test ve Paketleme

### 4.1 Test
- [ ] Birim testleri: config, DPAPI, dosya adı şablonu, retention, zip, batch/schtasks komut üretimi
- [ ] Entegrasyon testi: yerel SQL Express ile yedek alma
- [ ] Uzak sunucu senaryosu: UNC `accessPath` ile zip + retention
- [ ] Hata senaryoları: yanlış şifre, erişilemeyen yol, dolu disk, OFFLINE DB

### 4.2 Paketleme
- [ ] Uygulama ikonu ve sürüm bilgisi (`wails.json`, `-ldflags` ile sürüm)
- [ ] `wails build -clean -platform windows/amd64`
- [ ] (Opsiyonel) NSIS installer (`wails build -nsis`)
- [ ] README: kurulum, kullanım, uzak sunucu yol açıklaması, SQL yetki gereksinimleri
