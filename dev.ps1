param(
  [Parameter(Position = 0)]
  [string]$Command = ""
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

function Import-DotEnv {
  if (-not (Test-Path .env)) { return }
  Get-Content .env | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq "" -or $line.StartsWith("#")) { return }
    $pair = $line.Split("=", 2)
    if ($pair.Length -eq 2) {
      Set-Item -Path "Env:$($pair[0])" -Value $pair[1]
    }
  }
}

function Get-PgTarget {
  $url = $env:DATABASE_URL
  $rest = $url.Substring($url.IndexOf("://") + 3)
  $at = $rest.LastIndexOf("@")
  if ($at -ge 0) { $rest = $rest.Substring($at + 1) }
  $rest = ($rest -split "[/?]")[0]
  if ($rest.Contains(":")) {
    $parts = $rest.Split(":", 2)
    $script:PgHost = $parts[0]
    $script:PgPort = $parts[1]
  } else {
    $script:PgHost = $rest
    $script:PgPort = "5432"
  }
  return $script:PgHost -eq "127.0.0.1" -or $script:PgHost -eq "localhost"
}

function Find-PgBin {
  $cmd = Get-Command initdb -ErrorAction SilentlyContinue
  if ($cmd) { return Split-Path $cmd.Source }
  foreach ($dir in @(
      "/opt/homebrew/opt/postgresql@16/bin",
      "/usr/local/opt/postgresql@16/bin",
      "C:\Program Files\PostgreSQL\17\bin",
      "C:\Program Files\PostgreSQL\16\bin"
    )) {
    if (Test-Path (Join-Path $dir "initdb.exe")) { return $dir }
    if (Test-Path (Join-Path $dir "initdb")) { return $dir }
  }
  Write-Error "PostgreSQL est introuvable."
}

function Invoke-Psql {
  param([string]$Database, [string]$Sql, [string]$File, [string]$User)
  $psql = Join-Path $script:PgBin "psql"
  $psqlArgs = @("-h", "127.0.0.1", "-p", $script:PgPort, "-v", "ON_ERROR_STOP=1", "-d", $Database)
  if ($User) { $psqlArgs += @("-U", $User) }
  if ($File) { $psqlArgs += @("-f", $File) } else { $psqlArgs += @("-c", $Sql) }
  & $psql @psqlArgs
  if ($LASTEXITCODE -ne 0) { Write-Error "psql a échoué." }
}

function Test-OurPostgres {
  $pidFile = Join-Path $script:PgData "postmaster.pid"
  if (-not (Test-Path $pidFile)) { return $false }
  $procId = (Get-Content $pidFile -TotalCount 1)
  return $null -ne (Get-Process -Id $procId -ErrorAction SilentlyContinue)
}

function Confirm-Word {
  param([string]$Word)
  $answer = Read-Host "Tapez $Word pour confirmer"
  if ($answer -ne $Word) { Write-Error "Annulé." }
}

function Test-ApiBusy {
  $addr = if ($env:MYINFRA_ADDR) { $env:MYINFRA_ADDR } else { ":8080" }
  $port = $addr.Split(":")[-1]
  $listen = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
  if ($listen) { Write-Error "L'API écoute encore sur le port $port. Arrêtez ./dev d'abord." }
}

function Ensure-Role {
  $role = (& (Join-Path $script:PgBin "psql") -h 127.0.0.1 -p $script:PgPort -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='myinfra'").Trim()
  if ($role -ne "1") { Invoke-Psql -Database postgres -Sql "CREATE ROLE myinfra LOGIN" }
  $db = (& (Join-Path $script:PgBin "psql") -h 127.0.0.1 -p $script:PgPort -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='myinfra'").Trim()
  if ($db -ne "1") { Invoke-Psql -Database postgres -Sql "CREATE DATABASE myinfra OWNER myinfra" }
}

function Ensure-Postgres {
  if (-not (Get-PgTarget)) { return }
  if ($script:PgPort -notmatch '^\d+$') { Write-Error "Port invalide dans DATABASE_URL." }
  $script:PgBin = Find-PgBin
  if (Test-OurPostgres) {
    Write-Output "PostgreSQL : .pgdata, port $($script:PgPort)"
    Ensure-Role
    return
  }
  & (Join-Path $script:PgBin "pg_isready") -h 127.0.0.1 -p $script:PgPort | Out-Null
  if ($LASTEXITCODE -eq 0) {
    Write-Error "Le port $($script:PgPort) est déjà pris par un autre PostgreSQL. Arrêtez-le. Les données de dev sont dans .pgdata."
  }
  $version = Join-Path $script:PgData "PG_VERSION"
  if (-not (Test-Path $version)) {
    if ((Test-Path $script:PgData) -and (Get-ChildItem $script:PgData -Force | Select-Object -First 1)) {
      Write-Error ".pgdata n'est pas un cluster PostgreSQL."
    }
    New-Item -ItemType Directory -Force -Path $script:PgData | Out-Null
    Write-Output "Création du cluster dans .pgdata"
    & (Join-Path $script:PgBin "initdb") --locale=en_US.UTF-8 --encoding=UTF8 --auth=trust -D $script:PgData
    if ($LASTEXITCODE -ne 0) { Write-Error "initdb a échoué." }
    @"
listen_addresses = '127.0.0.1'
port = $($script:PgPort)
"@ | Add-Content -Path (Join-Path $script:PgData "postgresql.conf")
  } else {
    $conf = Get-Content (Join-Path $script:PgData "postgresql.conf") -Raw
    if ($conf -notmatch "(?m)^port = $($script:PgPort)$") {
      if ($script:PgMode -eq "reset" -and $conf -match "(?m)^port = (\d+)$") {
        $script:PgPort = $Matches[1]
      } else {
        Write-Error "Le cluster .pgdata n'écoute pas sur le port $($script:PgPort). Lancez ./dev reset pour le recréer."
      }
    }
  }
  & (Join-Path $script:PgBin "pg_ctl") -D $script:PgData -l (Join-Path $script:PgData "server.log") -w -t 30 start
  if ($LASTEXITCODE -ne 0) { Write-Error "PostgreSQL n'a pas démarré." }
  Write-Output "PostgreSQL : .pgdata, port $($script:PgPort)"
  Ensure-Role
}

function Save-Dump {
  $dir = Join-Path $env:USERPROFILE ".myinfra"
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  Write-Output "Sauvegarde dans $($script:Backup)"
  & (Join-Path $script:PgBin "pg_dump") -h 127.0.0.1 -p $script:PgPort -U myinfra --no-password --no-owner --no-acl -d myinfra -f $script:Backup
  if ($LASTEXITCODE -ne 0) { Write-Error "Sauvegarde impossible. .pgdata est conservé." }
}

Import-DotEnv
if (-not $env:DATABASE_URL) {
  Write-Error "DATABASE_URL manquant. Renseignez-le dans .env."
}

# Un clone de la bibliothèque à côté : MyInfra la lit telle quelle.
$library = Join-Path (Split-Path $PSScriptRoot -Parent) "MyInfra-templates-and-packages-library"
if (-not $env:MYINFRA_LIBRARY_DIR -and (Test-Path $library)) {
  $env:MYINFRA_LIBRARY_DIR = $library
}

$script:PgData = Join-Path $PSScriptRoot ".pgdata"
$script:Backup = Join-Path (Join-Path $env:USERPROFILE ".myinfra") "dev.sql"
$script:PgBin = ""
$script:PgHost = ""
$script:PgPort = ""
$script:PgMode = "start"

if ($Command -eq "reset") {
  if (-not (Get-PgTarget)) { Write-Error "DATABASE_URL n'est pas local. Rien n'est effacé." }
  if (-not (Test-Path (Join-Path $script:PgData "PG_VERSION"))) {
    Write-Output "Aucun cluster dans .pgdata."
    exit 0
  }
  Test-ApiBusy
  Confirm-Word "reset"
  $script:PgMode = "reset"
  Ensure-Postgres
  Save-Dump
  & (Join-Path $script:PgBin "pg_ctl") -D $script:PgData -m fast -w stop
  Remove-Item -Recurse -Force $script:PgData
  Write-Output "Cluster effacé. Pour revenir en arrière : ./dev restore"
  exit 0
}

if ($Command -eq "restore") {
  if (-not (Get-PgTarget)) { Write-Error "DATABASE_URL n'est pas local. Rien n'est restauré." }
  if (-not (Test-Path $script:Backup)) { Write-Error "Aucune sauvegarde dans $($script:Backup)" }
  Test-ApiBusy
  Confirm-Word "restore"
  Ensure-Postgres
  Invoke-Psql -Database postgres -Sql "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'myinfra' AND pid <> pg_backend_pid()"
  Invoke-Psql -Database postgres -Sql "DROP DATABASE IF EXISTS myinfra"
  Invoke-Psql -Database postgres -Sql "CREATE DATABASE myinfra OWNER myinfra"
  Invoke-Psql -Database myinfra -User myinfra -File $script:Backup
  Write-Output "Sauvegarde restaurée. Relancez ./dev"
  exit 0
}

if ($Command -ne "") { Write-Error "Commande inconnue : $Command" }

Ensure-Postgres

# Réplique par défaut seulement quand une copie principale la synchronise :
# seule, une réplique enregistre des demandes que personne n'exécute.
if (-not $env:MYINFRA_REPLICA) {
  $env:MYINFRA_REPLICA = if ($env:MYINFRA_PEERS) { "1" } else { "0" }
}

$addr = if ($env:MYINFRA_ADDR) { $env:MYINFRA_ADDR } else { ":8080" }
$port = $addr.Split(":")[-1]
$listen = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
if ($listen) {
  Write-Error "Le port $port est déjà pris."
}

if (-not (Test-Path web/node_modules)) {
  npm ci --prefix web
}

if ($env:MYINFRA_REPLICA -eq "1") {
  Write-Output "Réplique : cette copie ne déploie pas les machines."
} else {
  Write-Output "Cette copie déploie les machines."
}
if ($env:MYINFRA_PEERS) {
  Write-Output "Synchronisation : $($env:MYINFRA_PEERS)"
}

$api = Start-Process -FilePath go -ArgumentList "run","./cmd/myinfra" -PassThru -NoNewWindow
$web = $null
try {
  $ready = $false
  for ($i = 0; $i -lt 60; $i++) {
    if ($api.HasExited) { Write-Error "L'API s'est arrêtée." }
    try {
      Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$port/api/health" | Out-Null
      $ready = $true
      break
    } catch {
      Start-Sleep -Milliseconds 500
    }
  }
  if (-not $ready) { Write-Error "L'API ne répond pas." }
  $ui = Get-NetTCPConnection -LocalPort 5173 -State Listen -ErrorAction SilentlyContinue
  if ($ui) {
    Write-Output "Interface déjà lancée : http://127.0.0.1:5173"
  } else {
    $web = Start-Process -FilePath npm -ArgumentList "run","vite" -WorkingDirectory (Join-Path $PSScriptRoot "web") -PassThru -NoNewWindow
    Write-Output "Interface : http://127.0.0.1:5173"
  }
  Write-Output "API : http://127.0.0.1:$port"
  while (-not $api.HasExited) {
    if ($web -and $web.HasExited) { Write-Error "L'interface s'est arrêtée." }
    Start-Sleep -Seconds 1
  }
} finally {
  foreach ($proc in @($web, $api)) {
    if (-not $proc -or $proc.HasExited) { continue }
    Get-CimInstance Win32_Process -Filter "ParentProcessId=$($proc.Id)" -ErrorAction SilentlyContinue |
      ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
  }
}
