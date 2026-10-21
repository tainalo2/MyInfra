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

Pas encore exporté vers S3 externe : les bases répliquées CNPG et leurs dumps de migration restent liés au stockage du cluster. Prévu en phase 2 (plugin Barman / object store CNPG).

## Alertes

Non implémentées : voir point 2 de [ROADMAP.md](ROADMAP.md).
