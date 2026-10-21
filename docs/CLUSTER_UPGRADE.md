# Mise à jour du cluster de test (phase 1)

Checklist pour aligner toutes les copies MyInfra sur une release qui expose `POST /api/sync/yield` (à partir de **v0.1.0-rc.7** ; publier **rc.8** si des correctifs sync / lead / destruction ne sont pas encore dans rc.7).

1. **Image** — Mettre à jour le pod ou le `compose` vers `ghcr.io/tainalo2/myinfra:v0.1.0-rc.8` (ou la rc publiée).
2. **Toutes les copies** — Même version sur chaque instance ; une copie ancienne refuse la cession de main (`main_outdated`).
3. **Copies** — Vérifier prise de main / rendu de main ; forcer une reprise si la principale est injoignable.
4. **Destruction** — Lancer une suppression serveur, tuer la copie pilote, prendre la main sur l'autre : l'étape SSH doit reprendre.
5. **Synchronisation** — Mises à jour serveur (`updates`, `update_jobs`) et état `preparing` visibles sur l'autre copie après sync.

Variables utiles :

- `MYINFRA_PEERS` — URLs des autres copies, injectées au démarrage (liste locale complétée après chaque sync réussi).
- `MYINFRA_REPLICA=1` — Force le rôle réplique (développement avec une copie principale).
