$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
if (-not (Test-Path web/node_modules)) { npm ci --prefix web }
npm run build --prefix web
if (Test-Path internal/ui/dist) { Remove-Item -Recurse -Force internal/ui/dist }
Copy-Item -Recurse web/dist internal/ui/dist
# The only file of the folder git keeps, for a go build without the interface.
New-Item -ItemType File -Force -Path internal/ui/dist/.keep | Out-Null
New-Item -ItemType Directory -Force -Path dist | Out-Null

function Get-PostgresArtifact($goos, $goarch) {
  switch ("$goos-$goarch") {
    "darwin-arm64" { return "darwin-arm64v8" }
    "darwin-amd64" { return "darwin-amd64" }
    "linux-amd64" { return "linux-amd64" }
    "linux-arm64" { return "linux-arm64v8" }
    "windows-amd64" { return "windows-amd64" }
    default { return "" }
  }
}

function Get-Postgres($goos, $goarch) {
  $art = Get-PostgresArtifact $goos $goarch
  if (-not $art) { return $false }
  $ver = "16.15.0"
  $url = "https://repo1.maven.org/maven2/io/zonky/test/postgres/embedded-postgres-binaries-$art/$ver/embedded-postgres-binaries-$art-$ver.jar"
  $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString())
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    $jar = Join-Path $tmp "pg.jar"
    Invoke-WebRequest -Uri $url -OutFile $jar -UseBasicParsing
    # The .jar is a zip, which the tar of Windows reads. Files are extracted to
    # disk: a PowerShell redirection would rewrite the bytes as text.
    $txz = (tar -tf $jar | Select-String -Pattern '\.txz$' | Select-Object -First 1).Line
    if (-not $txz) { throw "Pas d'archive PostgreSQL dans $url" }
    tar -xf $jar -C $tmp $txz
    if ($LASTEXITCODE -ne 0) { throw "Archive PostgreSQL illisible." }
    $root = Join-Path $tmp "root"
    New-Item -ItemType Directory -Path $root | Out-Null
    tar -xJf (Join-Path $tmp $txz) -C $root
    if ($LASTEXITCODE -ne 0) { throw "Archive PostgreSQL illisible." }
    if (-not ((Test-Path (Join-Path $root "bin/postgres")) -or (Test-Path (Join-Path $root "bin/postgres.exe")))) {
      throw "PostgreSQL absent de l'archive pour $goos/$goarch."
    }
    tar -czf (Join-Path $PSScriptRoot "internal/boot/postgres.tar.gz") -C $root .
    if ($LASTEXITCODE -ne 0) { throw "PostgreSQL ne s'empaquette pas." }
  } finally {
    Remove-Item -Recurse -Force $tmp
  }
  return $true
}

function Build-One($goos, $goarch, $out) {
  $env:CGO_ENABLED = "0"
  $env:GOOS = $goos
  $env:GOARCH = $goarch
  $tags = @()
  if (Get-Postgres $goos $goarch) {
    $tags = @("-tags", "postgresembed")
  } elseif ("$goos-$goarch" -ne "windows-arm64") {
    Write-Error "PostgreSQL embarqué introuvable pour $goos/$goarch."
  }
  $version = if ($env:VERSION) { $env:VERSION } else { "dev" }
  go build -trimpath @tags -ldflags "-s -w -X main.version=$version -X main.image=$env:IMAGE" -o $out ./cmd/myinfra
  Write-Output $out
}

$target = if ($args.Count -ge 1) { $args[0] } else { "host" }
$arch = if ($args.Count -ge 2) { $args[1] } else { "amd64" }
switch ($target) {
  "host" {
    $os = go env GOOS
    $cpu = go env GOARCH
    $ext = ""
    if ($os -eq "windows") { $ext = ".exe" }
    Build-One $os $cpu "dist/myinfra$ext"
  }
  "all" {
    Build-One linux amd64 dist/myinfra_linux_amd64
    Build-One linux arm64 dist/myinfra_linux_arm64
    Build-One darwin amd64 dist/myinfra_darwin_amd64
    Build-One darwin arm64 dist/myinfra_darwin_arm64
    Build-One windows amd64 dist/myinfra_windows_amd64.exe
    Build-One windows arm64 dist/myinfra_windows_arm64.exe
  }
  { $_ -in "linux", "darwin", "windows" } {
    $ext = ""
    if ($target -eq "windows") { $ext = ".exe" }
    Build-One $target $arch "dist/myinfra_${target}_${arch}${ext}"
  }
  default { Write-Error "Usage: .\build.ps1 [all|linux|darwin|windows] [amd64|arm64]" }
}
