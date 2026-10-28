# MyInfra

Un binaire Go (`cmd/myinfra`) sert l'API et embarque l'interface. PostgreSQL garde l'état. Les machines sont pilotées en SSH.

- `cmd/myinfra` : point d'entrée
- `internal/api` : HTTP et MCP
- `internal/store` : PostgreSQL (pgx), schéma dans `schema.sql`
- `internal/deploy`, `internal/manifest`, `internal/agent`, `internal/provision`, `internal/catalog` : livraison, Docker/k3s, fournisseurs
- `web/src` : React 19, Vite 6, Tailwind 4. Le build est copié dans `internal/ui/dist` puis embarqué.
- Paquets et modèles : dépôt voisin `MyInfra-templates-and-packages-library`, via `MYINFRA_LIBRARY_DIR` (`./dev` le pose s'il est à côté).

Ne pas lire `dist/`, `.pgdata/`, `web/node_modules/`, `web/dist/`, `internal/ui/dist/` (sauf `.keep`), `internal/boot/postgres.tar.gz`, `internal/boot/postgres.txz`, `docs/screenshots/`, `.env`.

Dev : `./dev` (API `:8080`, Vite `:5173`, Postgres dans `.pgdata`). Build : `./build`. Tests : `go test ./...`.
