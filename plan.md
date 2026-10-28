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
- [x] Exit code: `0` tümü başarılı, `1` kısmi hata, `2` tamamen başarısız/config hatası (hiçbir DB yedeklenmediyse de `2`), `3` iş zaten çalışıyor
- [x] Aynı işin eşzamanlı ikinci çalışması engellenir (iş bazında; farklı işler paralel çalışabilir)
  - Süreçler arası adlandırılmış mutex `Global\SqlBackup.Job.<veri klasörü özeti>.<iş adı küçük harf>` (SYSTEM görevi ile GUI aynı kilidi görür; süreç çökerse OS bırakır)
  - Kilit tutuluyorsa hiçbir şey yapılmaz: `Result.AlreadyRunning = true`, uyarı mesajı, exit `3`
  - `backup.IsRunning(job)` → arayüzde durum gösterimi ve "Şimdi çalıştır" öncesi uyarı

### 1.5 ZIP (`internal/archive`)
- [x] `accessPath` erişilebilirlik kontrolü (yoksa anlaşılır hata) – okuma/yazma/silme gerçekten denenir; iş başında yapılır
  - Erişim yoksa yedekler **yine alınır**, yalnızca ZIP adımı hata verir (exit `1`)
- [x] `.bak` → `.zip` (archive/zip, Deflate; büyük dosyalar için stream ile, iptal destekli, zip64 otomatik)
- [x] Mod: DB başına ayrı zip (`.bak` adı → `.zip`) / tüm DB'ler tek zip (şablonda `{db}` yerine iş adı)
- [x] ZIP bütünlük kontrolü (zip'i açıp CRC + boyut doğrulama) sonrası, seçenek açıksa `.bak` silme
  - Önce `.zip.tmp`'ye yazılır, doğrulanınca asıl ada taşınır
- [x] ZIP başarısızsa `.bak` asla silinmez; doğrulaması (VERIFYONLY) başarısız yedek zip'lenmez

### 1.6 Retention
- [x] `accessPath` içinde iş adına ait dosyaları bulma (şablona uyan `.bak` / `.zip`)
  - Şablondan regex üretilir (`_2` eki ve `.bak`/`.zip` dahil, büyük/küçük harf duyarsız); `{db}` yalnızca bu çalışmada **başarılı** yedeklenen DB adlarıyla (tek zip'te iş adıyla) eşleşir → hatalı DB'nin ve başka DB'lerin eski yedeklerine dokunulmaz
  - Yedek zamanı dosya adından okunur; şablonda yıl+ay+gün yoksa retention uygulanmaz (uyarı)
  - Bu çalışmanın dosyaları hiçbir zaman silinmez; hiç başarılı yedek yoksa retention çalışmaz
- [x] "X günden eski" ve "son N adet tut" modları (N, DB başına çalışma sayısı; aynı çalışmanın `.bak` + `.zip`'i tek sayılır)
- [x] Silinen dosyaların loglanması (`Result.Retention.Deleted`); silme/erişim hatası exit `1`

### 1.7 Loglama (`internal/logging`)
- [x] `log/slog` ile dosyaya yazma: `logs\<job>_<yyyyMMdd>.log` (metin biçimi, ekleme kipi, çalışma başlangıç günü; `OpenJobLog` + `Handler`)
- [x] Her çalışma için JSON sonuç kaydı (`logs\runs\<job>_<yyyyMMdd_HHmmss>.json`, atomik yazma, aynı saniyede `_2`), Loglar ekranı bunu okuyacak (`SaveRun`, `ListRuns`, `ReadRun`, `ListLogs`)
- [x] Eski logların temizlenmesi (`Cleanup`, varsayılan 30 gün; tarih dosya adından, biçime uymayan dosyalara dokunulmaz)
- Paket `backup`/`config`'ten bağımsız; bağlama işi CLI (1.8) ve GUI'de: log aç → `backup.Run(Logger)` → `SaveRun(Result)` → `Cleanup`

### 1.8 CLI (`internal/cli`)
- [x] `main.go`: argüman varsa CLI, yoksa GUI (sürüm `-ldflags "-X main.version=..."`)
- [x] `--job` değeri iş adı (`jobs\<ad>.json`) veya `.json` dosya yolu olabilir
- [x] `SqlBackup.exe run --job <iş> [-v]` (iş zaten çalışıyorsa uyarı yazılır, exit `3`)
  - Log dosyası + JSON çalışma kaydı + eski log temizliği (30 gün); `-v` logları konsola da yazar; Ctrl+C iptal eder
  - İlerleme: konsolda tek satır (`\r`), yönlendirilmiş çıktıda aşama sonu satırları
- [x] `SqlBackup.exe test --job <iş>` (bağlantı + sunucu bilgisi; zip/saklama açıksa yedek klasörü erişimi → hata exit `1`)
- [x] `SqlBackup.exe list --job <iş> [--json]` (DB listesi + işin hangilerini yedekleyeceği / neden yedeklemeyeceği)
- [x] `version`, `help`; kullanım hataları exit `2`
- [x] CLI modunda konsol penceresi davranışı (GUI build'de `AttachConsole(ATTACH_PARENT_PROCESS)` + `CONOUT$`; çıktı yönlendirilmişse dokunulmaz)
  - Yönlendirilmiş çıktıyla gerçek exe test edildi; **etkileşimli cmd'de elle denenmeli** (cmd GUI exe'yi beklemez → `start /wait` önerilir; .bat bekler)
- [ ] Faz 1 kabul testi: elle yazılmış JSON ile uzak sunucudan yedek + zip + retention
  - Yerel SQL Express ile gerçek exe üzerinden yapıldı (test/list/run, zip + .bak silme + count retention ✓); **uzak sunucu** senaryosu bekliyor

---

## Faz 2 – Çoklu Dil Desteği (`internal/i18n`)

Exe ile aynı klasördeki `lang\` altında `<dilkodu>.lng` dosyaları (ör. `lang\en.lng`, `lang\tr.lng`). CLI ve GUI'deki **tüm** kullanıcıya görünen metinler bu dosyalardan gelir. Varsayılan dil **İngilizce**; ilk sürümde ek olarak yalnızca **Türkçe** dosyası üretilir.
Faz 1'den sonra yapılır (CLI metinleri de hazır olur), GUI'den (Faz 4) önce biter; böylece arayüz baştan çok dilli yazılır.

### 2.1 Dil dosyası biçimi
- [x] UTF-8 (BOM'lu/BOM'suz ikisi de okunur), satır başına `anahtar = değer`; `#` ile yorum, boş satırlar yok sayılır
- [x] Anahtarlar noktalı ve gruplu: `err.*`, `warn.*`, `log.*`, `reason.*`, `summary.*`, `cli.*`, `common.*`, `gui.*` (ör. `err.config.nameInvalid`)
- [x] Yer tutucular adlandırılmış: `{db}`, `{path}`, `{count}` (çevirmen sırayı değiştirebilir; `%s` kullanılmaz)
- [x] Çok satırlı metin için `\n`, ters bölü için `\\` kaçışı
- [x] Başlık anahtarları: `@code = tr`, `@name = Türkçe` (dil seçim listesinde gösterilir)

### 2.2 `internal/i18n` paketi
- [x] `lang\` klasörünün bulunması: `filepath.Dir(os.Executable())\lang`; `SQLBACKUP_LANG_DIR` ile değiştirilebilir (dev/test)
- [x] `en.lng` exe'ye `go:embed` ile gömülür → `lang\` klasörü yoksa/bozuksa program yine İngilizce çalışır
- [x] Geri düşme sırası: seçili dil → gömülü İngilizce → anahtarın kendisi (eksik anahtar uyarı olarak loglanır, program çökmez)
- [x] API: `i18n.T(key, args...)` (ör. `T("err.mssql.backup", "db", name, "err", err)`), `i18n.Errorf(key, args...)`, `i18n.SetLanguage(code)`, `i18n.Languages()` (kod + ad listesi), `i18n.All()` (frontend için tüm sözlük)
- [x] Eşzamanlı kullanıma güvenli (dil değişimi GUI'de çalışma anında olabilir)
- [x] Birim testleri: ayrıştırma, kaçışlar, yer tutucu, geri düşme, bozuk satır, eşzamanlılık
- [x] `lang/` repo kökünde bir Go paketi (`lang/embed.go`) → `en.lng` kökte durur ve gömülebilir

### 2.3 Dil seçimi
- [x] Öncelik: CLI `--lang <kod>` → ayarlar dosyası (`%ProgramData%\SqlBackup\settings.json` → `language`) → `en`
- [x] Zamanlanmış görev (CLI, SYSTEM hesabı) dili ayarlar dosyasından okur; `.bat` içine dil yazılmaz
- [x] Bilinmeyen dil kodu → İngilizce + uyarı
- [x] `settings.json` ayrıca `logKeepDays` tutar (CLI log temizliği bunu kullanır; varsayılan 30)

### 2.4 Mevcut kodun dönüştürülmesi
- [x] `internal/config` (`Validate` hataları vb.), `internal/mssql`, `internal/backup`, `internal/archive`, retention, logging ve CLI'daki tüm kullanıcıya görünen metinler `i18n.T` ile
- [x] Hata türleri: sabit metin yerine anahtar + parametre taşıyan hata (`i18n.Error`) — metin gösterim anında üretilir, `errors.Is/As` çalışmaya devam eder
- [x] SQL Server'dan gelen hata metinleri çevrilmez (olduğu gibi gösterilir), yalnızca etrafındaki açıklama çevrilir
- [x] Log dosyaları ve JSON çalışma kayıtları yazıldığı andaki dilde; kod yorumları ve commit mesajları Türkçe kalır
- [x] `lang\en.lng` ve `lang\tr.lng` dosyalarının üretilmesi (~170 anahtar)
- [x] Mevcut testler `TestMain` + `i18ntest.Use("tr")` ile Türkçe çalışır (tr.lng de sınanır); CLI testleri İngilizce/dil seçimini ayrıca sınar

### 2.5 GUI entegrasyonu (Faz 4 ile birlikte kullanılır)
- [x] Backend: `GetLanguages()`, `GetTranslations()`, `SetLanguage(code)` (ayarlara kaydeder)
- [x] Frontend (`frontend/src/i18n.ts`): `t('key', {param})` yardımcısı + `data-i18n` öznitelikli statik metinler; dil değişince yeniden çizim (yeniden başlatma gerekmez)
- [x] Ayarlar ekranında dil seçimi (liste `lang\` klasöründeki dosyalardan) – Faz 4.2'de Ayarlar sayfasına taşındı

### 2.6 Build ve tutarlılık
- [x] Kaynak dosyalar repo kökünde `lang\`; `wails build` sonrası `build\bin\lang\` içine kopyalanması (`wails.json` `postBuildHooks`, PowerShell ile)
- [x] Test (`internal/i18n/consistency_test.go`): Go kodundaki ve `frontend/src`'deki her anahtar `en.lng`'de var, `en.lng`'de kullanılmayan anahtar yok; diğer diller ile anahtar kümeleri ve yer tutucular birebir aynı
- [x] Yeni dil ekleme rehberi (`lang/README.md`, İngilizce): `en.lng` kopyala → çevir → `lang\`'a koy
- [x] İş kilidi adı veri klasörüne göre kapsamlandı (`Global\SqlBackup.Job.<veri klasörü özeti>.<iş>`): farklı `SQLBACKUP_HOME`'lardaki aynı adlı işler ve paralel test paketleri çakışmaz

---

## Faz 3 – Batch ve Zamanlanmış Görev (`internal/scheduler`)

### 3.1 Batch dosyası
- [x] Şablondan `.bat` üretimi (exe yolu + `run --job "<tam json yolu>" [--home "<veri klasörü>"]`, `exit /b`) – `scheduler.Batch`
- [x] Türkçe karakter / boşluk içeren yollar için doğru tırnaklama ve kod sayfası (`chcp 65001`)
  - BOM'suz UTF-8 + CRLF; `chcp` öncesi satırlar yalnızca ASCII; yollardaki `%` → `%%`; sondaki `\` ikilenir (`"D:\\"`)
  - Önceki kod sayfası geri yüklenir (açık konsoldan çalıştırılınca konsol bozulmaz); çıkış kodu programınki
  - Test gerçek `cmd.exe` ile çalıştırır (Türkçe + boşluk + `%PATH%` içeren yollar, kod sayfası geri yükleme)
- [x] Kaydetme konumu: varsayılan `jobs\<job>.bat`, `batch --job <iş> --path <dosya>` ile farklı yer

### 3.2 Zamanlanmış görev
**Karar:** `schtasks.exe` yerine Görev Zamanlayıcı **COM API'si** (`Schedule.Service`, go-ole). `schtasks /Query` çıktısı yerelleştirilmiş (Türkçe başlıklar, tarih biçimi) ve `/TR` 261 karakterle sınırlı; COM'da durum/tarih/sonuç tipli, hata kodları (`E_ACCESSDENIED`) ayırt edilebilir. Görev, XML tanımıyla kaydedilir.
**Karar:** Görev eylemi `.bat` değil, doğrudan exe (`run --job ... [--home ...]`): GUI alt sistemli exe pencere açmaz, kod sayfası sorunu olmaz. `.bat` elle/başka zamanlayıcı için ayrı üretilir.
- [x] Zamanlama modeli: günlük / haftalık (gün seçimi) / N saatte bir (başlangıç saatinden itibaren süresiz tekrar), başlangıç saati
- [x] Görev XML üretimi (`scheduler.TaskXML`): `\SqlBackup\<job>`; SYSTEM → `S-1-5-18` + `HighestAvailable`; geçerli kullanıcı → `InteractiveToken` (şifre saklanmaz, yalnızca oturum açıkken) + `LeastPrivilege`
  - Ayarlar: önceki çalışma sürerken yenisi başlamaz (`IgnoreNew`), kaçırılan çalışma sonra yapılır, pilde de çalışır, süre sınırı yok (`PT0S`; komut zaman aşımı işin ayarı)
  - Görev Zamanlayıcı eylemdeki `%AD%` kalıplarını genişletir ve kaçışı yok → iki+ `%` içeren yollar reddedilir
- [x] Görev sorgulama (`QueryTask`) → var mı, durum, son çalışma + sonuç (programın 0-3 kodları), sonraki çalışma, hesap (SID → ad), komut
- [x] Görev silme (boş kalan klasör de silinir) ve "Şimdi çalıştır" (`RunTask`)
- [x] Yönetici yetkisi yoksa UAC ile yükseltme (`ShellExecuteEx runas` + bekleme + çıkış kodu)
  - CLI: `task create|delete|run|status --job <iş> [--elevate] [--json]`; erişim reddinde `--elevate` exe'yi `--lang --home --output <geçici dosya>` ile yükseltilmiş başlatır, çıktıyı geri aktarır. GUI (Faz 4) aynı yolu kullanacak.
  - Denendi: yalnızca **SYSTEM görevi oluşturma/güncelleme** yükseltme ister; geçerli kullanıcı görevi, SYSTEM görevini çalıştırma/sorgulama/silme (kaydeden kullanıcı) istemez
- [x] Uyarılar (`scheduler.Warnings`): SYSTEM + Windows Auth → SQL'de `NT AUTHORITY\SYSTEM` login/yetki; SYSTEM + UNC erişim yolu → makine hesabı izni; geçerli kullanıcı → yalnızca oturum açıkken
- [x] Gerçek görevle denendi: Görev Zamanlayıcı üzerinden çalıştırma (Türkçe yollar), SYSTEM ↔ kullanıcı iş kilidi her iki yönde (exit 3), SYSTEM'in yetkisiz yedeği (262 → exit 2, kayıt yazıldı)
- [x] `%ProgramData%`'da SYSTEM'in oluşturduğu log dosyasına kullanıcı yazamıyordu (Users yalnızca RX) → `config.EnsureDirs` log klasörünü ilk oluştururken `Users:(OI)(CI)M` verir; `run` ve GUI açılışı log açmadan önce çağırır
- Entegrasyon testi: `SQLBACKUP_TEST_TASKS=1` (geçici `\SqlBackupTest_<nano>` klasörü, yönetici gerekmez, sonunda silinir)

---

## Faz 4 – Arayüz (Wails)

### 4.1 Backend köprüsü (`internal/app`)
**Karar:** Köprü `main` yerine `internal/app` paketinde (test edilebilir; `main` testte `frontend/dist` ve Wails ister). Frontend: `wailsjs/go/app/App`. Yaşam döngüsü `app.Startup(a)` / `app.Shutdown(a)` paket işlevleri (App yöntemi olsalardı frontend'e açılırlardı).
**Karar:** CLI ve GUI'nin ortak çalışma akışı `internal/jobrun.Execute` (EnsureDirs → log → `backup.Run` → kayıt → temizlik); log kaydı başlatanı belirtir (`log.cli.started` / `log.gui.started`).
- [x] Job CRUD: `ListJobs` (son çalışma, görev durumu, çalışıyor mu, doğrulama hataları, okunamayan dosyalar), `NewJob`, `GetJob`, `SaveJob`, `DeleteJob`, `DuplicateJob`
  - Şifre frontend'e gitmez: `GetJob` → `hasPassword` / `passwordError` (başka makineden kopyalanmış iş); `SaveJob` `passwordChanged=false` ise kayıtlı şifreyi korur, Windows kimliğinde siler
  - `SaveJob` geçersiz işi kaydetmez, hataları liste döner (`errors`); aynı ad (harf duyarsız) çakışması; yeniden adlandırmada eski dosya silinir, eski görev/.bat için uyarı (yalnızca harf değişikliğinde uyarı yok); kayıtlı görev ayarlarla uyuşmuyorsa uyarı
  - `DeleteJob`: görev (gerekirse UAC) → varsayılan .bat → iş dosyası; görev silinemezse hiçbir şey silinmez; çalışan iş silinemez/yeniden adlandırılamaz
- [x] `TestConnection(req)`, `ListDatabases(req)` (`ConnectionRequest`: girilen şifre veya `storedJob`'un kayıtlı şifresi), `CheckFolderAccess`, `PreviewFileName`
- [x] `RunJobNow(name)` + olaylar `backup:started`, `backup:progress`, `backup:done` (sonuç + uyarılar); iş zaten çalışıyorsa (bu arayüzde veya başka süreçte) hata; `CancelJob`, `IsJobRunning`, `RunningJobs`; kapanışta çalışmalar iptal edilip kayıt beklenir
- [x] `SaveBatch`, `DefaultBatchPath`, `CreateTask`, `DeleteTask`, `RunTask`, `GetTaskStatus` (`outdated`: görev XML'inden zamanlama geri okunup işle karşılaştırılır), `TaskWarnings`; `ErrAccessDenied` → `cli.RunElevated` ile `task <sub> --job <ad>` (UAC)
- [x] `ListRuns(job, limit)`, `ReadRun`, `ListLogs`, `ReadLog` (son 2 MB); yollar yalnızca log klasörü içinden okunur
- [x] Klasör seçme (`SelectFolder`) ve .bat kaydetme (`SelectBatchFile`) diyalogları; `GetAppInfo` (sürüm, klasörler, açılış hatası), `GetSettings` / `SaveSettings`

### 4.2 Tema ve layout
**Karar:** Framework yok (vanilla TS); arayüz `ui/dom.ts`'deki `h()` ile `createElement` üzerinden kurulur, dış veri asla `innerHTML`'e girmez. Tema `settings.json`'da (`theme`); açılış rengi ve başlık çubuğu `app.WindowTheme()` ile kayıtlı temaya göre (renk sıçraması olmasın).
- [x] CSS değişkenleri ile koyu tema (varsayılan) + açık tema (`styles/theme.css`, `<html data-theme>`; `Settings.Theme`, başlık çubuğu `WindowSetDark/LightTheme`)
  - bg `#0f1115`, panel `#171a21`, border `#262b36`, text `#e6e8ee`, accent `#4f8cff`, success `#3ecf8e`, error `#ff5c5c`
- [x] Sol menü + içerik alanı layout'u (`layout.ts`: sayfa listesi, `navigate`, `PageHandle{canLeave, dispose}`), pencere başlığı ve ikon (`build/appicon.png` → `icon.ico`, menüde aynı SVG logo), min. pencere 900×600
- [x] Ortak bileşenler (`ui/components.ts`, `toast.ts`, `modal.ts`): buton (Promise dönen tıklamada otomatik meşgul), input/select/field (ipucu + hata), switch, checkbox, tablo, rozet, uyarı kutusu, toast bildirim, modal (`confirmDialog`, `messageDialog`), ilerleme çubuğu (yüzde/belirsiz), spinner
- [x] Ayarlar sayfasında dil + tema + sürüm/veri klasörü; İşler ve Loglar şimdilik yer tutucu (`pages/placeholder.ts`); `startupError` toast ile gösterilir

### 4.3 Ekranlar
**Karar:** İş düzenleyici ayrı menü sayfası değil, İşler sayfasının alt görünümü (`pages/editor/`); sekmeler (Bağlantı, Veritabanları, Yedek, ZIP/Saklama, Zamanlama) tek `EditorState` üzerinde çalışır ve birlikte kaydedilir. Görev/.bat işlemleri kayıtlı işi kullandığından önce kaydetme önerilir (`ensureSaved`).
**Karar:** `backup:*` olayları uygulama boyunca tek yerde (`runs.ts`) dinlenir; iş bitince hangi sayfa açık olursa olsun bildirim gösterilir.
- [x] **İşler (Dashboard):** iş kartları, son çalışma durumu ("çalışıyor" + ilerleme çubuğu, başka süreçte çalışan dahil), sonraki çalışma, görev güncel değil / geçersiz ayar rozetleri, Yeni/Düzenle/Kopyala/Geçmiş/Sil/Şimdi Çalıştır/Durdur; 30 sn'de bir yenilenir
- [x] **Bağlantı:** sunucu, instance, port, protokol, kimlik tipi, kullanıcı/şifre (kayıtlı şifre gösterilmez; çözülemiyorsa uyarı), encrypt/trust, zaman aşımı, Test Et (sunucu bilgileri + sıkıştırma desteği)
- [x] **Veritabanları:** liste + çoklu seçim (tümünü seç), boyut/durum/kurtarma modeli/son yedek, "sistem DB hariç", "yeni DB'leri dahil et"; `excludedDatabases` seçimle birlikte doldurulur; sunucuda olmayan seçili DB uyarısı
- [x] **Yedek Ayarları:** `serverPath` (+ Gözat, sunucu varsayılanı), `accessPath` (+ erişimi sına), yerel/uzak açıklaması, dosya adı şablonu + canlı önizleme, compression/checksum/copy_only/verify, komut zaman aşımı
- [x] **ZIP ve Saklama:** ZIP aç/kapa, tek/DB başına ZIP, ZIP sonrası `.bak` sil, retention modu ve değeri, erişim uyarısı
- [x] **Zamanlama:** sıklık, gün, saat, aralık, çalışacak hesap, canlı görev uyarıları; görev durumu (son/sonraki çalışma, sonuç, güncel değil), `Görev Oluştur/Güncelle`, `Çalıştır`, `Sil`, `.bat Kaydet`
- [x] **Loglar:** iş filtresi, çalışma geçmişi tablosu + ayrıntı penceresi (DB/ZIP/saklama sonuçları), log dosyaları + görüntüleyici (büyük dosyada son 2 MB); İşler'den "Geçmiş" ile işe filtreli açılır
**Test:** `wails dev` + yüklü Edge (playwright-core) ile 18 adımlık uçtan uca senaryo yerel SQL Express'te geçti. Bulunan hata: şifreleme kapalıyken sertifika doğrulandığı için varsayılan SQL Express'e bağlanılamıyordu (entegrasyon testleri `TrustServerCert` ile gizliyordu) → `ConnString` düzeltildi, testler varsayılan ayarlarla bağlanıyor.
- [x] **Ayarlar:** dil ve tema (4.2), varsayılan klasörler (`Settings.DefaultServerPath/DefaultAccessPath` → `NewJob`), log saklama süresi

### 4.4 UX detayları
**Karar:** Doğrulama tek kaynakta (Go): `config.Validate` her hatayı `*config.FieldError{Field: "connection.server", …}` ile alana bağlar; `SaveJobResult.FieldErrors` (alan → metinler) frontend'de `field({name})` / `errorSlot(name)` altında gösterilir. Başarısız kaydetmeden sonra her değişiklik `ValidateJob` ile (300 ms gecikmeli) canlı doğrulanır; istemcide kural kopyası yok.
**Karar:** Pencere kapanış uyarısı Go'da (`OnBeforeClose` → `app.BeforeClose`, yerel Evet/Hayır kutusu, varsayılan Hayır): frontend takılsa bile pencere kapatılabilir. Frontend kaydedilmemiş durumu `SetUnsavedChanges` ile bildirir; çalışan işler `App.running`'den.
- [x] Form doğrulama ve anlaşılır hata mesajları: alan altında hata + kırmızı çerçeve, sekme başlığında hata sayısı, açık sekmede hata yoksa hatalı ilk sekmeye geçiş, üstte tüm hataların listesi; canlı yeniden doğrulama (`ValidateJob`). Şablon alanı önizleme ile zaten canlı doğrulanır.
- [x] Kaydedilmemiş değişiklik uyarısı: düzenleyicide menü/geri dönüşte sorulur (Ctrl+S kaydeder); pencere kapanırken kaydedilmemiş değişiklik ve bu arayüzden çalışan yedekler (iptal edilecek) için sorulur
- [x] Uzun işlemlerde kilitlenmeyen arayüz: Promise dönen düğmeler ve tablo satırları (Loglar'da kayıt/log okunurken) meşgul gösterilir, çift tıklama engellenir; render hatası sonsuz "yükleniyor" yerine sayfada hata olarak gösterilir (ör. bozuk `settings.json` ile Ayarlar)
**Test:** `wails dev` + Edge ile 4 adımlık senaryo (kaydet → alan hataları + sekme rozetleri → canlı düzeltme → Veritabanları hata satırı → başarılı kayıt, hatalar temizlenir) geçti; uyarısız pencere kapanışı gerçek exe ile denendi. Uyarılı kapanış (yerel ileti kutusu) birim testli (`TestCloseWarning`), elle denenmeli.

---

## Faz 5 – Kalite, Test ve Paketleme

### 5.1 Test
- [ ] Birim testleri: config, DPAPI, i18n, dosya adı şablonu, retention, zip, batch/schtasks komut üretimi
- [ ] Entegrasyon testi: yerel SQL Express ile yedek alma
- [ ] Uzak sunucu senaryosu: UNC `accessPath` ile zip + retention
- [ ] Hata senaryoları: yanlış şifre, erişilemeyen yol, dolu disk, OFFLINE DB

### 5.2 Paketleme
- [ ] Uygulama ikonu ve sürüm bilgisi (`wails.json`, `-ldflags` ile sürüm)
- [ ] `wails build -clean -platform windows/amd64`
- [ ] (Opsiyonel) NSIS installer (`wails build -nsis`) – `lang\` klasörü pakete dahil
- [ ] README: kurulum, kullanım, uzak sunucu yol açıklaması, SQL yetki gereksinimleri
