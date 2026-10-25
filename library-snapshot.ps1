# Remplace la bibliothèque intégrée au binaire par une release publiée.
# ./library-snapshot.ps1 v1.2.0
# $env:MYINFRA_LIBRARY_REPO = "compte/depot" vise un fork.
param([Parameter(Mandatory = $true)][string]$Tag)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

$repo = if ($env:MYINFRA_LIBRARY_REPO) { $env:MYINFRA_LIBRARY_REPO } else { "tainalo2/MyInfra-templates-and-packages-library" }
$out = "internal/library/snapshot"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  foreach ($file in "library.tar.gz", "SHA256SUMS", "SHA256SUMS.sig") {
    Invoke-WebRequest -UseBasicParsing -OutFile (Join-Path $tmp $file) "https://github.com/$repo/releases/download/$Tag/$file"
  }
  # Refusée si la signature n'est pas d'une clé que MyInfra connaît.
  go run ./cmd/library verify $tmp
  if ($LASTEXITCODE -ne 0) { throw "signature refusée" }
  foreach ($file in "library.tar.gz", "SHA256SUMS", "SHA256SUMS.sig") {
    Copy-Item (Join-Path $tmp $file) $out -Force
  }
  Write-Output "bibliothèque intégrée : $Tag"
} finally {
  Remove-Item -Recurse -Force $tmp
}
