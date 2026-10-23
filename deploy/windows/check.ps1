# Checks the setup as a person uses it: installs MyInfra, opens it from its
# Start menu shortcut, closes its window, which must stop MyInfra and its
# PostgreSQL, then uninstalls it. The release runs it on Windows.
#   deploy/windows/check.ps1 dist/MyInfra_1.4.0_windows_amd64_setup.exe
param([Parameter(Mandatory)] [string] $Setup)
$ErrorActionPreference = "Stop"

$data = Join-Path $env:RUNNER_TEMP "myinfra"
$env:MYINFRA_HOME = $data
$env:MYINFRA_ADDR = "127.0.0.1:8080"
$pidFile = Join-Path $data "pgdata/postmaster.pid"

function Step($text) { Write-Host "$(Get-Date -Format HH:mm:ss) $text" }
function Fail($text) {
  Get-Process MyInfra, postgres -ErrorAction SilentlyContinue | Format-Table Id, ProcessName, MainWindowTitle | Out-String | Write-Host
  foreach ($log in "myinfra.log", "pg.log") {
    $path = Join-Path $data $log
    if (Test-Path $path) { Write-Host "--- $log"; Get-Content $path | Write-Host }
  }
  throw $text
}
# Run waits for that process alone: Start-Process -Wait would also wait for
# what it starts.
function Run($exe) {
  $process = Start-Process $exe -ArgumentList "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART" -PassThru
  if (-not $process.WaitForExit(120000)) { Fail "$exe ne se termine pas." }
}
function Health {
  try {
    Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 http://127.0.0.1:8080/api/health | Out-Null
    return $true
  } catch {
    return $false
  }
}

Step "Installation"
Run $Setup
$exe = Join-Path $env:LOCALAPPDATA "Programs\MyInfra\MyInfra.exe"
if (-not (Test-Path $exe)) { Fail "MyInfra.exe n'est pas installé." }
$link = Join-Path ([Environment]::GetFolderPath("Programs")) "MyInfra.lnk"
if (-not (Test-Path $link)) { Fail "Le raccourci du menu Démarrer manque." }

Step "Ouverture depuis le menu Démarrer"
Start-Process $link
$up = $false
$log = Join-Path $data "myinfra.log"
foreach ($i in 1..120) {
  if (Health) { $up = $true; break }
  # Stopped on an error, MyInfra shows it and waits: no need to wait more.
  if ((Test-Path $log) -and (Select-String -Quiet -Path $log -Pattern "level=ERROR")) { break }
  Start-Sleep 1
}
if (-not $up) { Fail "MyInfra ne démarre pas depuis son raccourci." }
Step "MyInfra répond"

$app = $null
foreach ($i in 1..30) {
  $app = Get-Process MyInfra -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowTitle -eq "MyInfra" } | Select-Object -First 1
  if ($app) { break }
  Start-Sleep 1
}
if (-not $app) { Fail "La fenêtre MyInfra n'apparaît pas." }
Step "Fenêtre ouverte"

if (-not $app.CloseMainWindow()) { Fail "La fenêtre ne reçoit pas la demande de fermeture." }
foreach ($i in 1..30) {
  if (-not (Test-Path $pidFile) -and -not (Get-Process MyInfra -ErrorAction SilentlyContinue)) { break }
  Start-Sleep 1
}
if (Test-Path $pidFile) { Fail "PostgreSQL tourne encore après la fermeture de la fenêtre." }
if (Get-Process MyInfra -ErrorAction SilentlyContinue) { Fail "MyInfra tourne encore après la fermeture de la fenêtre." }
Step "Fermée : MyInfra et PostgreSQL arrêtés"

Step "Désinstallation"
Run (Join-Path $env:LOCALAPPDATA "Programs\MyInfra\unins000.exe")
# The uninstaller finishes from a copy of itself.
foreach ($i in 1..30) {
  if (-not (Test-Path $exe)) { break }
  Start-Sleep 1
}
if (Test-Path $exe) { Fail "La désinstallation laisse MyInfra.exe." }
Step "Désinstallé"
