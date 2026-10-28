---
paths:
  - "**/*.ps1"
---

# PowerShell

`build.ps1`, `dev.ps1` et `library-snapshot.ps1` sont les équivalents Windows de `./build`, `./dev` et `./library-snapshot`. `deploy/windows/*.ps1` empaquete l'installateur. Garder chaque script aligné avec son homologue shell.

- `$ErrorActionPreference = "Stop"` en tête.
- Les scripts à la racine restent dans le dépôt : `Set-Location $PSScriptRoot`.
- Ne pas committer `internal/boot/postgres.tar.gz` : le build le produit puis l'embarque.
