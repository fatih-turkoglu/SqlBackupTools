# SqlBackup

Windows için Microsoft SQL Server otomatik yedekleme programı. Bir veya daha çok veritabanının **FULL** yedeğini alır, isteğe bağlı olarak doğrular, ZIP'ler ve eski yedekleri siler. İşler arayüzden hazırlanır, Windows Görev Zamanlayıcı ile zamanlanır.

- Tek exe: argümansız açılırsa arayüz, argümanla çalışırsa komut satırı (zamanlanmış görev bunu kullanır).
- Yerel ve uzak SQL Server; Windows veya SQL kimlik doğrulaması.
- Arayüz dili: İngilizce (varsayılan) ve Türkçe.
- Kapsam dışı: differential / transaction log yedeği, e-posta bildirimi.

## İndir

Derlemeye gerek yok; son sürümü indirin:

| Dosya | Ne zaman |
|---|---|
| [**SqlBackup-windows-amd64-setup.exe**](https://github.com/OWNER/SqlBackup/releases/latest/download/SqlBackup-windows-amd64-setup.exe) | **Önerilen.** `Program Files`'a kurar; zamanlanmış görevi SYSTEM hesabıyla çalıştırmak için gereklidir. |
| [SqlBackup-windows-amd64.zip](https://github.com/OWNER/SqlBackup/releases/latest/download/SqlBackup-windows-amd64.zip) | Taşınabilir; kurulum yapmadan denemek veya görevi kendi Windows hesabınızla çalıştırmak için. |
| [SHA256SUMS.txt](https://github.com/OWNER/SqlBackup/releases/latest/download/SHA256SUMS.txt) | İndirilen dosyanın özetini doğrulamak için. |

Önceki sürümler ve değişiklik notları: [Releases](https://github.com/OWNER/SqlBackup/releases).

- Program kod imzalı değildir; Windows SmartScreen ilk açılışta "Windows bilgisayarınızı korudu" diyebilir. **Ek bilgi → Yine de çalıştır** ile devam edin.
- İndirilen dosyayı doğrulamak için PowerShell'de çıkan değeri `SHA256SUMS.txt` ile karşılaştırın:
  ```powershell
  (Get-FileHash .\SqlBackup-windows-amd64-setup.exe -Algorithm SHA256).Hash
  ```

## Gereksinimler

- Windows 10 / 11 veya Windows Server 2016+ (64 bit)
- Microsoft Edge WebView2 Runtime (Windows 11'de yüklü; kurulum paketi gerekirse yükler)
- SQL Server 2012 veya üstü (SQL Server Express ile test edildi). Express ve Web sürümleri yedek sıkıştırmayı desteklemez; bu durumda sıkıştırma kapatılır ve uyarı yazılır.

## Kurulum

**Kurulum paketi:** `SqlBackup-windows-amd64-setup.exe` programı `C:\Program Files\SqlBackup` klasörüne kurar, Başlat menüsü ve masaüstü kısayolu ekler.

**Taşınabilir:** zip'i bir klasöre açın; `SqlBackup.exe` ile yanındaki `lang\` klasörü birlikte durmalıdır (`lang\` yoksa program yalnızca İngilizce çalışır). Zip'i kullanıcı klasörüne (Masaüstü, İndirilenler) açarsanız SYSTEM görevi oluşturulamaz (bkz. [Zamanlanmış görev](#zamanlanmış-görev)); bunun için kurulum paketini kullanın.

Program işlerini, ayarlarını ve loglarını `%ProgramData%\SqlBackup` klasöründe tutar (zamanlanmış görev SYSTEM hesabıyla da çalışabildiği için kullanıcı profili kullanılmaz):

```
%ProgramData%\SqlBackup\
  settings.json             dil, tema, log saklama süresi, varsayılan klasörler
  jobs\<iş>.json            iş tanımı (şifre yalnızca şifreli)
  jobs\<iş>.bat             isteğe bağlı batch dosyası
  logs\<iş>_<tarih>.log     günlük log
  logs\runs\*.json          her çalışmanın sonuç kaydı
```

Klasöre yalnızca SYSTEM, yöneticiler ve programı kullanan Windows hesabı erişebilir (iş dosyalarındaki şifreli SQL şifresi bu bilgisayarda çözülebildiği için diğer kullanıcılara kapalıdır). Önceki sürümlerin oluşturduğu klasörün izinleri program ilk açıldığında bu şekilde düzeltilir. Programı başka bir Windows hesabıyla da kullanacaksanız o hesabı yönetici olarak çalıştırın veya klasöre izin verin.

Kaldırma bu klasörü ve zamanlanmış görevleri **silmez**. Programı tamamen kaldırmak için önce arayüzden (veya `SqlBackup.exe task delete --job <iş>`) görevleri silin, sonra klasörü elle silin.

## Kullanım

1. **İşler → Yeni iş.** İşe bir ad verin (harf, rakam, `_ - .`; dosya ve görev adı olarak kullanılır).
2. **Bağlantı:** sunucu (`.`, `localhost`, `SUNUCU\INSTANCE`, IP), kimlik doğrulama ve gerekiyorsa protokol. *Bağlantıyı test et* sunucu sürümünü ve kullanılan protokolü gösterir. Protokol otomatikse sırayla denenir (yerel: Shared Memory, Named Pipes, TCP; uzak: TCP, Named Pipes); SQL Browser servisi gerekmez.
3. **Veritabanları:** yedeklenecekleri işaretleyin. *Yeni veritabanlarını da dahil et* açıksa çalışma anında sunucudaki tüm veritabanları (işaretlenmeyenler hariç) yedeklenir.
4. **Yedek:** yedek klasörü, dosya adı şablonu, sıkıştırma / checksum / doğrulama / copy-only.
5. **ZIP ve saklama:** isteğe bağlı ZIP (veritabanı başına veya tek dosya, `.bak` silme) ve saklama (gün veya adet).
6. **Zamanlama:** günlük / haftalık / saatlik; *Görevi oluştur* `\SqlBackup\<iş>` görevini oluşturur.
7. **Şimdi çalıştır** ile deneyin; sonuçlar **Loglar** sayfasında.

### Yedek klasörü: sunucu yolu ve erişim yolu

Yedek dosyasını programınız değil, **SQL Server servis hesabı** yazar. Bu yüzden iki yol vardır:

| Alan | Anlamı | Örnek |
|---|---|---|
| Sunucu yolu | SQL Server'ın gördüğü klasör (`BACKUP ... TO DISK`) | `D:\SqlBackups` |
| Erişim yolu | Programın aynı klasöre eriştiği yol; ZIP ve saklama için gerekir. Yerel sunucuda boş bırakın. | `\\SQLSRV01\SqlBackups` |

- **Yerel sunucu:** `C:\Program Files\Microsoft SQL Server\...\Backup` gibi varsayılan klasörlere yalnızca servis hesabı yazabilir; program orada ZIP/saklama yapamaz. `D:\SqlBackups` gibi bir klasör açıp hem servis hesabına (`NT Service\MSSQLSERVER` veya `NT Service\MSSQL$<INSTANCE>`) hem görevi çalıştıran hesaba Değiştirme izni verin:
  ```
  icacls D:\SqlBackups /grant "NT Service\MSSQL$SQLEXPRESS:(OI)(CI)M"
  ```
- **Uzak sunucu:** sunucu yolu, uzak makinedeki yerel yoldur (`D:\SqlBackups`); bu bilgisayardaki bir yol değildir. ZIP veya saklama istiyorsanız klasörü paylaşıma açın ve erişim yolu olarak UNC yolunu yazın. Görev SYSTEM ile çalışıyorsa paylaşıma bu bilgisayarın makine hesabı (`DOMAIN\BILGISAYAR$`) ile erişilir; paylaşım ve NTFS izinlerini ona göre verin.
- Hata iletisi "Operating system error 5" içeriyorsa servis hesabının klasöre yazma izni yoktur; "error 3" ise klasör sunucu makinesinde bulunamamıştır.

### Dosya adı şablonu

`{db}` zorunludur. Ayrıca `{server}`, `{job}` ve `{yyyyMMdd}`, `{HHmmss}`, `{yyyy-MM-dd}` gibi tarih parçaları kullanılabilir. `.bak` yoksa eklenir; aynı çalışmada çakışan adlara `_2`, `_3` eklenir. Saklama (retention) eski dosyaları bu şablona ve dosya adındaki tarihe göre bulur; şablonu değiştirirseniz eski adlı dosyalar artık silinmez. Aynı klasöre aynı şablonla aynı veritabanını yedekleyen iki iş birbirinin dosyalarını sayar.

## Zamanlanmış görev

- Görevler Görev Zamanlayıcı'da `\SqlBackup\<iş>` altında oluşturulur ve doğrudan `SqlBackup.exe run --job "<iş.json>"` çalıştırır. Görev ve `.bat` hiçbir şifre içermez.
- **SYSTEM:** oturum açık olmasa da çalışır; görevi oluşturmak/güncellemek yönetici onayı (UAC) ister. Program yalnızca yöneticilerin değiştirebildiği bir klasörde (kurulum paketiyle `Program Files`) olmalıdır; aksi halde exe'yi değiştirebilen biri kendi kodunu SYSTEM olarak çalıştırabileceği için görev oluşturulmaz. **Geçerli kullanıcı:** yalnızca o kullanıcı oturum açmışken çalışır, yönetici onayı gerekmez.
- Programı başka bir klasöre taşırsanız görevleri yeniden oluşturun (arayüz güncel olmayan görevi işaretler).
- Görevin "Son çalışma sonucu" programın çıkış kodudur (aşağıda).

## SQL Server yetkileri

Bağlanan hesap (Windows kimlik doğrulamasında **işi çalıştıran hesap**; SYSTEM görevinde `NT AUTHORITY\SYSTEM`, uzak sunucuda `DOMAIN\BILGISAYAR$`) için:

| Gereken | Neden |
|---|---|
| Sunucuda login | Bağlantı. SQL kimlik doğrulaması için sunucu "SQL Server ve Windows Kimlik Doğrulaması" modunda olmalı. |
| Her veritabanında `db_backupoperator` (veya `sysadmin`) | `BACKUP DATABASE` |
| `msdb`'de `backupset` / `backupmediafamily` okuma | Yedek boyutu. Yoksa yedek alınır, boyut gösterilmez (uyarı). |
| `CREATE DATABASE` izni (ör. `dbcreator` rolü) | `RESTORE VERIFYONLY`; yalnızca *Doğrula* açıksa. |

Örnek (SYSTEM görevi, yerel sunucu):

```sql
CREATE LOGIN [NT AUTHORITY\SYSTEM] FROM WINDOWS;  -- yoksa
USE [Veritabanim];
CREATE USER [NT AUTHORITY\SYSTEM] FOR LOGIN [NT AUTHORITY\SYSTEM];
ALTER ROLE db_backupoperator ADD MEMBER [NT AUTHORITY\SYSTEM];
```

`tempdb`, OFFLINE veritabanları ve veritabanı snapshot'ları yedeklenemez: açıkça seçildiyse hata, *yeni veritabanlarını da dahil et* ile geldiyse atlandı olarak raporlanır.

## Şifre güvenliği

SQL şifresi Windows DPAPI ile **bu bilgisayara** özel şifrelenip yalnızca iş dosyasında (`passwordEnc`) saklanır; log, `.bat`, görev ve hata iletilerine yazılmaz. İş dosyası başka bilgisayara kopyalanırsa şifre çözülemez; arayüz şifreyi yeniden ister. Uzak sunucuda SQL kimlik doğrulaması kullanıyorsanız **Bağlantıyı şifrele** seçeneğini sertifika doğrulamasıyla ("Sunucu sertifikasına güven" kapalı) açın; aksi halde şifre ağda araya giren birine açık olur ve program kaydederken uyarır.

## Komut satırı

```
SqlBackup.exe run   --job <iş> [-v]       işi çalıştırır (yedek, doğrulama, zip, saklama)
SqlBackup.exe test  --job <iş>            bağlantıyı ve yedek klasörü erişimini sınar
SqlBackup.exe list  --job <iş> [--json]   sunucudaki veritabanlarını listeler
SqlBackup.exe batch --job <iş> [--path <dosya.bat>]
SqlBackup.exe task create|delete|run|status --job <iş> [--elevate] [--json]
SqlBackup.exe version | help
```

`<iş>`: iş adı (`%ProgramData%\SqlBackup\jobs` içinde) veya bir `.json` dosyasının yolu. Genel seçenekler: `--lang <kod>`, `--home <veri klasörü>`, `--output <dosya>`.

Exe pencereli (GUI) bir uygulama olduğu için çıktısı konsolda ancak yönlendirilirse görünür:

```
cmd /c "SqlBackup.exe list --job Gunluk 2>&1"
SqlBackup.exe run --job Gunluk --output C:\Temp\sonuc.txt
```

**Çıkış kodları:** `0` tümü başarılı · `1` kısmi hata (bazı veritabanları, ZIP veya saklama başarısız; `task status`: görev yok) · `2` tamamen başarısız veya ayar/bağlantı hatası · `3` iş zaten çalışıyor (başlatılmadı).

## Diller

Dil dosyaları exe yanındaki `lang\` klasöründedir (`en.lng` exe'ye de gömülüdür). Yeni dil eklemek için [lang/README.md](lang/README.md).

## Geliştirme

Go 1.27+, Node.js ve Wails v2 CLI gerekir (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0`).

```powershell
.\build.ps1                  # temiz sürüm build'i → build\bin\SqlBackup.exe + lang\
.\build.ps1 -Nsis            # + kurulum paketi (NSIS kurulu olmalı)
.\build.ps1 -Release         # + dist\: setup.exe, taşınabilir zip, SHA256SUMS.txt
& "$(go env GOPATH)\bin\wails.exe" dev

go vet ./... ; go test ./internal/...
$env:SQLBACKUP_TEST_SERVER = '.\SQLEXPRESS'; go test ./internal/...   # SQL Server entegrasyon testleri
$env:SQLBACKUP_TEST_TASKS = '1'; go test ./internal/scheduler/       # Görev Zamanlayıcı entegrasyon testleri
```

Sürüm numarasının tek kaynağı `wails.json` → `info.productVersion` alanıdır.

### Sürüm yayınlama

Derlenmiş dosyalar repoya eklenmez (`dist/` yok sayılır); GitHub Releases'a yüklenir. Dosya adlarında sürüm olmadığı için yukarıdaki `releases/latest/download/...` linkleri her yeni sürümde kendiliğinden son sürümü gösterir.

1. `wails.json` → `info.productVersion` değerini artırıp commit edin.
2. Temiz çalışma ağacında `.\build.ps1 -Release` (commit edilmemiş değişiklik varsa uyarır).
3. Etiketleyip yayınlayın ([GitHub CLI](https://cli.github.com/) ile; etiket sürümle aynı olmalı):
   ```powershell
   git tag v0.5.0; git push origin v0.5.0
   gh release create v0.5.0 dist\* --title "SqlBackup 0.5.0" --generate-notes
   ```
   `gh` yoksa GitHub'da **Releases → Draft a new release** ile aynı etiketi seçip `dist\` içindeki üç dosyayı yükleyin.

## Lisans

[MIT](LICENSE)
