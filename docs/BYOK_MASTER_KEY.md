# Clé maîtresse BYOK (clés d'API des assistants IA)

Les utilisateurs confient à MyInfra leurs propres clés d'API (BYOK : Claude,
Codex…). Elles sont chiffrées au repos en AES-256-GCM par une **clé maîtresse
dédiée**, distincte de `MYINFRA_MASTER_KEY`.

## Décisions

1. **Deux clés, deux rôles.** `MYINFRA_MASTER_KEY` (le « coffre ») scelle les
   secrets d'infrastructure, signe la synchronisation entre copies et les
   webhooks Git. La clé BYOK ne sert qu'aux clés d'API des assistants. Faire
   tourner l'une ne touche jamais l'autre.
2. **Où elle vit.**
   - Copie serveur (`DATABASE_URL` posée) : uniquement la variable
     d'environnement `MYINFRA_BYOK_MASTER_KEY` (32 octets aléatoires, base64,
     même forme que `MYINFRA_MASTER_KEY`). Elle n'est jamais écrite sur disque
     ni en base. À conserver dans le gestionnaire de secrets de l'hébergeur,
     comme `MYINFRA_MASTER_KEY`.
   - Copie locale (`~/.myinfra`, PostgreSQL embarqué) : fichier
     `~/.myinfra/byok.key` (droits `0600`), généré au premier démarrage à côté
     de `master.key`. `MYINFRA_BYOK_MASTER_KEY` l'emporte si elle est posée.
3. **Absente ou invalide : mode dégradé, jamais de crash.** Sans clé, MyInfra
   démarre normalement et journalise
   `assistant désactivé : MYINFRA_BYOK_MASTER_KEY manquante`. Une clé illisible
   (mauvaise taille, base64 invalide, fichier corrompu) est journalisée et
   traitée de la même façon. Dans les deux cas :
   - `GET /api/assistant/status` → `200 {"enabled":false,"assistant_unavailable":true,"reason":"no_master_key"}`
   - `POST /api/assistant/chat` → `200 {"assistant_unavailable":true,"reason":"no_master_key"}`
   - `PUT|DELETE /api/assistant/keys/{provider}` → `503`
   - tout le reste de MyInfra fonctionne.
4. **Rotation sans perte.** La clé en cours de remplacement se passe dans
   `MYINFRA_BYOK_MASTER_KEY_PREVIOUS` ; au démarrage, chaque entrée que la
   nouvelle clé n'ouvre pas est ouverte avec l'ancienne et rechiffrée avec la
   nouvelle (`internal/assistant.(*KeyStore).Rekey`). Les entrées qu'aucune
   des deux n'ouvre sont signalées dans le journal : leurs utilisateurs
   ressaisissent leur clé.

## Installation sur une instance fraîche

Copie serveur (Docker / compose) :

```sh
export MYINFRA_MASTER_KEY=$(head -c 32 /dev/urandom | base64)        # coffre
export MYINFRA_BYOK_MASTER_KEY=$(head -c 32 /dev/urandom | base64)   # clés BYOK
docker compose up -d
```

Vérifier : le journal ne doit pas contenir `assistant désactivé`, et
`GET /api/assistant/status` (connecté) répond sans `assistant_unavailable`.

Copie locale : rien à faire, `~/.myinfra/byok.key` est créé au premier
lancement. À sauvegarder avec `master.key` (voir `docs/BACKUPS.md`).

## Rotation

1. Générer la nouvelle clé : `NEW=$(head -c 32 /dev/urandom | base64)`.
2. Redémarrer avec les deux :
   `MYINFRA_BYOK_MASTER_KEY=$NEW MYINFRA_BYOK_MASTER_KEY_PREVIOUS=<ancienne>`.
   Le journal indique `clés BYOK rechiffrées avec la nouvelle clé maîtresse`
   (et, le cas échéant, `clés BYOK illisibles … à saisir de nouveau`).
3. Au redémarrage suivant, retirer `MYINFRA_BYOK_MASTER_KEY_PREVIOUS`.
4. Détruire l'ancienne clé.

Copie locale : remplacer le contenu de `~/.myinfra/byok.key` par `$NEW` et
passer l'ancienne valeur dans `MYINFRA_BYOK_MASTER_KEY_PREVIOUS` le temps d'un
démarrage.

Une rotation de `MYINFRA_MASTER_KEY` ne requiert rien côté BYOK, et
inversement.

## Format des entrées scellées

`base64(nonce || AES-256-GCM(clé d'API))`, le nom du fournisseur en donnée
associée. Toute persistance (`KeyStore.Export` / `Import`) doit garder cette
forme à l'identique : c'est ce que lit `Rekey`. Ce format est figé avant la
v0.1.0 ; le changer après imposerait un rechiffrement de toutes les clés
stockées.

## Limite connue

À ce jour les entrées scellées vivent en mémoire (`KeyStore`) : elles sont
perdues au redémarrage, et la rotation au démarrage ne s'applique qu'aux
entrées que la persistance fournira. La persistance en base (table dédiée,
entrées au format ci-dessus, chargées via `Import` au démarrage) est un
ticket de suite ; le contrat de clé, de rotation et de mode dégradé décrit
ici ne change pas avec elle.
