# Sécurité — copies, synchronisation, sondes

Revue ciblée sur le travail **multi-copies** (phase 1) et les routes sensibles (phase 3).

## Rôle pilote / réplique

- Seule la copie **pilote** exécute les actions qui touchent les machines (`drives()` dans `internal/api/auth.go`). Une réplique reçoit **409** sur transfert, déploiement, restauration, etc. (`guard_test.go`).
- `MYINFRA_REPLICA=1` ou `0` **fixe** le rôle : `takeLead` et les routes sync de changement de mandat refusent avec `role_fixed`.

## Synchronisation entre pairs

- `GET /api/sync/state`, `POST /api/sync/snapshot`, `POST /api/sync/yield`, `POST /api/sync/lead` exigent le jeton dérivé de la **clé maître** (`HMAC-SHA256` sur `myinfra-sync-v1`, `sync.go`). Pas de session navigateur.
- La clé de **développement** (`devMaster`) ne peut pas synchroniser : refus **403** sur ces routes.
- `POST /api/sync/yield` / `sync/lead` : le corps porte un `epoch` ; un mandat **≤** à celui en base est refusé (**409** `term_stale`) pour éviter un retour en arrière.
- `takeLead` (utilisateur authentifié) incrémente le mandat, appelle le pair avec le jeton sync, puis redémarre si le rôle change. Les destructions en cours sont reprises côté nouveau pilote (`resumeDestructions`).

## Interface et session

- `POST /api/copies/lead` : authentification utilisateur normale (pas le jeton sync).
- Limite de connexion par adresse IP (`gateSet`), `X-Forwarded-For` seulement depuis des proxies de confiance (`guard_test.go`).
- Requêtes cross-site marquées `Sec-Fetch-Site` : traitement renforcé pour les actions sensibles (`foreignBrowser`).

## Sondes (reachabilité)

- Les sondes SSH/HTTP **ne s’authentifient pas** aux applications : elles détectent une absence de réponse, pas la validité du contenu ou du certificat (`reach.go`, `InsecureSkipVerify` volontaire pour le knock HTTP).
- Les alertes optionnelles (`internal/notify`) ne partent que depuis la copie **pilote** et uniquement sur **transition** d’état (`alerts.go`), pour limiter le bruit.

## Recommandations opérationnelles

- Protéger `MYINFRA_MASTER_KEY` et les URL des pairs (`MYINFRA_PEERS`) comme des secrets d’administration.
- Exposer l’API derrière HTTPS ; ne faire confiance aux en-têtes de proxy que depuis le réseau du cluster.
- Après mise à jour, vérifier qu’une seule copie pilote les machines (écran **Copies**) avant des opérations destructives.
