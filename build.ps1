# SqlBackup sürüm build'i.
# Sürümün tek kaynağı wails.json "info.productVersion" alanıdır: exe dosya
# özellikleri oradan, "SqlBackup.exe version" çıktısı -ldflags ile buradan gelir.
#
#   .\build.ps1            # temiz build: build\bin\SqlBackup.exe + lang\
#   .\build.ps1 -Nsis      # + build\bin\SqlBackup-amd64-installer.exe (makensis gerekir)
param(
    [switch]$Nsis
)
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$version = (Get-Content wails.json -Raw -Encoding UTF8 | ConvertFrom-Json).info.productVersion
if (-not $version) { throw 'wails.json info.productVersion tanımlı değil' }

$wails = Join-Path (go env GOPATH) 'bin\wails.exe'
$buildArgs = @('build', '-clean', '-platform', 'windows/amd64', '-ldflags', "-X main.version=$version")
if ($Nsis) { $buildArgs += '-nsis' }

Write-Host "SqlBackup $version derleniyor..."
& $wails @buildArgs
if ($LASTEXITCODE -ne 0) { throw "wails build başarısız (çıkış kodu $LASTEXITCODE)" }

# Exe GUI alt sistemli; çıktısını cmd üzerinden oku.
$reported = (cmd /c "build\bin\SqlBackup.exe version 2>&1" | Out-String).Trim()
if ($reported -notmatch [regex]::Escape($version)) { throw "exe sürümü uyuşmuyor: '$reported'" }
if (-not (Test-Path build\bin\lang\tr.lng)) { throw 'build\bin\lang kopyalanmadı' }
Write-Host "Tamam: $reported"
