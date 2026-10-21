# Sauvegardes hors cluster

## SQLite (Litestream)

Les applications à disque unique répliquent déjà vers le **Garage** du cluster. Vous pouvez ajouter une **deuxième réplique** vers un S3 compatible externe (Scaleway, OVH, Backblaze B2, MinIO, etc.).

Définir sur le pod / le binaire MyInfra qui **pilote** le cluster :

| Variable | Exemple |
|----------|---------|
| `MYINFRA_OFFSITE_S3_ENDPOINT` | `https://s3.fr-par.scw.cloud` |
| `MYINFRA_OFFSITE_S3_BUCKET` | `myinfra-offsite` |
| `MYINFRA_OFFSITE_S3_REGION` | `fr-par` |
| `MYINFRA_OFFSITE_S3_ACCESS_KEY` | clé d’accès |
| `MYINFRA_OFFSITE_S3_SECRET_KEY` | secret |

Les objets sont rangés sous `clusters/<id-cluster>/apps/<id-app>/<service>/` dans le bucket. Après changement de variables, une **relivraison** des applications concernées met à jour la config Litestream.

Le Garage local reste la copie utilisée au démarrage sur le cluster ; le S3 externe est une **copie de secours** si le cluster disparaît (restauration manuelle / prochaine étape produit : assistant de restauration).

## PostgreSQL (CloudNativePG)

Quand les mêmes variables `MYINFRA_OFFSITE_S3_*` sont définies sur la copie **pilote**, chaque cluster PostgreSQL CNPG d’une application reçoit :

- un secret `myinfra-cnpg-offsite` (clés S3) ;
- une section `backup.barmanObjectStore` sur la ressource `Cluster` ;
- un `ScheduledBackup` (cron par défaut `0 4 * * *`, surcharge `MYINFRA_CNPG_BACKUP_CRON`).

Les sauvegardes WAL et de base sont rangées sous `clusters/<id-cluster>/cnpg/<nom-db>/` dans le bucket externe. Une **relivraison** de l’application applique la config. La restauration depuis ce dépôt se fait avec les outils CNPG / Barman (pas encore d’assistant dans l’UI).

## Téléchargement local

Sur la page d’une application, section **Sauvegardes** : une entrée par service et par source disponible.

| Source | Quand |
|--------|--------|
| **Stockage du cluster** | SQLite avec sauvegarde continue active (`backup` = prêt) |
| **S3 externe** | Même cas + variables `MYINFRA_OFFSITE_S3_*` sur la copie pilote |
| **Disque** | Migration en cours, disque seul, ou fichiers non reconnus |
| **PostgreSQL** | Service passé sur CNPG (`database` = prêt) |
| **Machine seule** | Application sur un serveur sans cluster |

API :

- `GET /api/applications/{id}/backups` — sources disponibles (téléchargement et restauration)
- `GET /api/applications/{id}/backups/{service}?source=…` — téléchargement
- `POST /api/applications/{id}/backups/{service}?source=…` — restauration (corps = fichier, max 1 Go ; copie **pilote** uniquement)

| Source restaurée | Fichier attendu |
|------------------|-----------------|
| `garage`, `offsite` | archive `.tar.gz` produite par le téléchargement |
| `postgres` | dump `pg_restore` (format custom) |
| `disk`, `machine` | archive `.tar.gz` du répertoire de données |

## Alertes (optionnelles)

MyInfra **démarre sans** e-mail ni ntfy. Pour activer des notifications plus tard :

- `MYINFRA_ALERT_NTFY_URL` — URL de publication ntfy
- `MYINFRA_ALERT_SMTP_HOST`, `MYINFRA_ALERT_SMTP_PORT`, `MYINFRA_ALERT_SMTP_USER`, `MYINFRA_ALERT_SMTP_PASSWORD`
- `MYINFRA_ALERT_EMAIL_FROM`, `MYINFRA_ALERT_EMAIL_TO`

Le paquet `internal/notify` n’envoie rien tant qu’aucune de ces variables n’est définie.

La copie **pilote** envoie une alerte à la **première** détection (puis au retour à la normale, sans message de rétablissement) pour :

- serveur SSH injoignable après plusieurs échecs ;
- machine signalée absente chez le provider ;
- cluster sans aucun serveur joignable ;
- application publiée dont l’URL ne répond plus.
