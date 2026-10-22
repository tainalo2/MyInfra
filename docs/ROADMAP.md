# Feuille de route MyInfra

Vue courte — socle **v0.1.0-rc.8** livré, priorités **suivantes**.

## Déjà en place

Sauvegardes et restauration (S3 externe, CNPG, téléchargements UI), alertes, validation fournisseurs / DNS / registre, build depuis Git, comptes **admin** / **déploiement** / **lecture seule** avec 2FA, copies synchronisées.

→ Détails : [BACKUPS.md](BACKUPS.md), [VALIDATION.md](VALIDATION.md), [SECURITY_COPIES.md](SECURITY_COPIES.md), [CLUSTER_UPGRADE.md](CLUSTER_UPGRADE.md).

## Prochain

1. **Multi-comptes** — finaliser droits, UI et tests (connexions réservées à l’admin, rôles déploiement / lecture seule).
2. **Qualité** — parcours critiques couverts en CI (auth, rôles, déploiement minimal).
3. **Release stable** — version sans `-rc`, image publiée, notes de version.
4. **Produit** — retours terrain (réglages, doc, petits écarts UX).

L’ordre peut changer selon les issues ouvertes sur le dépôt.
