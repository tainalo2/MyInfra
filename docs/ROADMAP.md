# Feuille de route MyInfra

État au **v0.1.0-rc.8**.

## 1. Stabiliser et publier — fait

- Release **rc.8** : sync `updates` / `update_jobs`, `preparing_since`, reprise des destructions au `takeLead`, `POST /api/sync/yield`.
- Captures d’écran vérifiées en CI ; checklist cluster : [CLUSTER_UPGRADE.md](CLUSTER_UPGRADE.md).

## 2. Protéger les données — fait

Objectif : ne plus dépendre uniquement du Garage et des bases **dans** le cluster.

| Livrable | Statut |
|----------|--------|
| Réplication SQLite (Litestream) vers un **S3 externe** en plus du Garage | **MVP** : variables `MYINFRA_OFFSITE_S3_*` (voir [BACKUPS.md](BACKUPS.md)) |
| **Téléchargement local** par scénario (garage, offsite, disque, PostgreSQL, machine seule) | **Fait** — UI + API |
| Sauvegardes PostgreSQL (CNPG) vers S3 externe | **Fait** — Barman + `ScheduledBackup` si `MYINFRA_OFFSITE_S3_*` |
| Restauration guidée (upload, même format que le téléchargement) | **Fait** — UI + `POST` par scénario |
| Alertes e-mail / **ntfy** (optionnelles au démarrage ; `internal/notify`) | **Fait** — serveur, provider, cluster, application (copie pilote) |

## 3. Valider l’existant

- Providers : couvrir les 6 restants sur 12 en E2E.
- DNS Cloudflare.
- Registre privé pour le pull d’images.
- Revue sécurité (routes copies, reprise de main, sondes).

## 4. Fonctionnalités

- Build depuis Git (BuildKit / Kaniko, webhook).
- Multi-utilisateurs, rôles, 2FA.
- Coût réel (serveurs disparus, alerte budget).
- Templates : import URL, modèles avec champs guidés.

---

Ordre recommandé : terminer **2** (sauvegardes + alertes), puis **3**, puis **4**.
