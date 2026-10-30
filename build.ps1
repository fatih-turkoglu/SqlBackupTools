# SqlBackup sürüm build'i.
# Sürümün tek kaynağı wails.json "info.productVersion" alanıdır: exe dosya
# özellikleri oradan, "SqlBackup.exe version" çıktısı -ldflags ile buradan gelir.
#
#   .\build.ps1            # temiz build: build\bin\SqlBackup.exe + lang\
#   .\build.ps1 -Nsis      # + build\bin\SqlBackup-amd64-installer.exe (makensis gerekir)
#   .\build.ps1 -Release   # -Nsis + dist\ (GitHub Releases'a yüklenecek dosyalar):
#                          #   SqlBackup-windows-amd64-setup.exe, SqlBackup-windows-amd64.zip, SHA256SUMS.txt
# Dosya adlarında sürüm yok: README'deki releases/latest/download/<ad> linkleri her sürümde çalışır.
param(
    [switch]$Nsis,
    [switch]$Release
)
if ($Release) { $Nsis = $true }
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$version = (Get-Content wails.json -Raw -Encoding UTF8 | ConvertFrom-Json).info.productVersion
if (-not $version) { throw 'wails.json info.productVersion tanımlı değil' }

$wails = Join-Path (go env GOPATH) 'bin\wails.exe'
$buildArgs = @('build', '-clean', '-platform', 'windows/amd64', '-ldflags', "-X main.version=$version")
if ($Nsis) {
    $buildArgs += '-nsis'
    # NSIS kurulumu PATH'e eklemez; wails makensis'i PATH'te arar.
    if (-not (Get-Command makensis -ErrorAction SilentlyContinue)) {
        $nsisDir = Join-Path ${env:ProgramFiles(x86)} 'NSIS'
        if (-not (Test-Path (Join-Path $nsisDir 'makensis.exe'))) { throw 'makensis bulunamadı (NSIS kurulu değil)' }
        $env:PATH = "$nsisDir;$env:PATH"
    }
}

Write-Host "SqlBackup $version derleniyor..."
& $wails @buildArgs
if ($LASTEXITCODE -ne 0) { throw "wails build başarısız (çıkış kodu $LASTEXITCODE)" }

# Exe GUI alt sistemli; çıktısını cmd üzerinden oku.
$reported = (cmd /c "build\bin\SqlBackup.exe version 2>&1" | Out-String).Trim()
if ($reported -notmatch [regex]::Escape($version)) { throw "exe sürümü uyuşmuyor: '$reported'" }
if (-not (Test-Path build\bin\lang\tr.lng)) { throw 'build\bin\lang kopyalanmadı' }
Write-Host "Tamam: $reported"

if (-not $Release) { return }

if (git status --porcelain) { Write-Warning 'Çalışma ağacında commit edilmemiş değişiklik var; paket bunları da içerir.' }

$dist = Join-Path $PSScriptRoot 'dist'
if (Test-Path $dist) { Remove-Item $dist -Recurse -Force }
New-Item -ItemType Directory $dist | Out-Null

$setup = Join-Path $dist 'SqlBackup-windows-amd64-setup.exe'
Copy-Item build\bin\SqlBackup-amd64-installer.exe $setup

# Taşınabilir zip: kökte SqlBackup\ klasörü (exe + lang\ + README + LICENSE).
# Compress-Archive (PS 5.1) girişlere ters eğik çizgi yazar; girişler '/' ile elle eklenir.
Add-Type -AssemblyName System.IO.Compression, System.IO.Compression.FileSystem
$zip = Join-Path $dist 'SqlBackup-windows-amd64.zip'
$files = @(
    @{ Src = 'build\bin\SqlBackup.exe'; Dst = 'SqlBackup/SqlBackup.exe' },
    @{ Src = 'README.md'; Dst = 'SqlBackup/README.md' },
    @{ Src = 'README.tr.md'; Dst = 'SqlBackup/README.tr.md' },
    @{ Src = 'LICENSE'; Dst = 'SqlBackup/LICENSE.txt' }
)
foreach ($lng in Get-ChildItem build\bin\lang\*.lng) {
    $files += @{ Src = $lng.FullName; Dst = "SqlBackup/lang/$($lng.Name)" }
}
$archive = [System.IO.Compression.ZipFile]::Open($zip, 'Create')
try {
    foreach ($f in $files) {
        $src = (Resolve-Path $f.Src).Path
        [void][System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive, $src, $f.Dst, 'Optimal')
    }
} finally {
    $archive.Dispose()
}

# sha256sum biçimi: "<özet>  <dosya>" (Get-FileHash ile de karşılaştırılabilir).
$sums = Get-ChildItem $dist -File | Sort-Object Name | ForEach-Object {
    '{0}  {1}' -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
}
[System.IO.File]::WriteAllText((Join-Path $dist 'SHA256SUMS.txt'), (($sums -join "`n") + "`n"))

Write-Host "Sürüm dosyaları: $dist"
Get-ChildItem $dist | ForEach-Object { Write-Host ('  {0,-36} {1,12:N0} bayt' -f $_.Name, $_.Length) }
Write-Host ''
Write-Host 'Yayınlamak için (GitHub CLI):'
Write-Host "  git tag v$version; git push origin v$version"
Write-Host "  gh release create v$version dist\* --title `"SqlBackup $version`" --generate-notes"
