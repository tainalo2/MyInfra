# Makes the setup of MyInfra from the app built by ./build windows-app, on
# Windows with Inno Setup 6.
#   deploy/windows/package.ps1 1.4.0 amd64 dist/myinfra_windows_amd64_app.exe dist
param(
  [Parameter(Mandatory)] [string] $Version,
  [Parameter(Mandatory)] [ValidateSet("amd64", "arm64")] [string] $Arch,
  [Parameter(Mandatory)] [string] $Exe,
  [string] $OutDir = "dist"
)
$ErrorActionPreference = "Stop"

$iscc = (Get-Command iscc.exe -ErrorAction SilentlyContinue).Source
if (-not $iscc) {
  $iscc = Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6\ISCC.exe"
}
# Where winget puts it when installed for the current user only.
if (-not (Test-Path $iscc)) {
  $iscc = Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 6\ISCC.exe"
}
if (-not (Test-Path $iscc)) { throw "Inno Setup 6 est introuvable : https://jrsoftware.org/isdl.php" }

# Inno Setup only takes numbers: 1.4.0-rc.1 shows as 1.4.0.
$numeric = ($Version -split '-')[0]
$innoArch = if ($Arch -eq "amd64") { "x64compatible" } else { "arm64" }
$out = "MyInfra_${Version}_windows_${Arch}_setup"
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
& $iscc /Q "/DVersion=$numeric" "/DExe=$((Resolve-Path $Exe).Path)" "/DArch=$innoArch" "/DOut=$out" "/DOutDir=$((Resolve-Path $OutDir).Path)" (Join-Path $PSScriptRoot "myinfra.iss")
if ($LASTEXITCODE -ne 0) { throw "Inno Setup a échoué." }
Join-Path $OutDir "$out.exe"
