# Écrire un paquet MyInfra

Un paquet décrit une application : ses services, ses disques, ses variables et les questions posées à l'installation. Il s'écrit en YAML (ou en JSON) et commence toujours par :

```yaml
type: myinfra-package
version: 1
```

Vérifiez toujours le texte avec `check_package` avant de l'enregistrer : chaque problème est rendu avec son chemin et sa ligne.

`version: 1` est la version du format, pas celle de l'application. Les paquets de la bibliothèque (le catalogue) ont ce même format. Ils sont publiés dans le dépôt `tainalo2/MyInfra-templates-and-packages-library`, chacun avec sa version dans un fichier à part.

## Les services

Un service fait tourner une image : le site, sa base de données, un cache, une tâche de fond. De 1 à 12 services. Chaque service a un nom en minuscules, avec des tirets, par lequel les autres le joignent sur le réseau de l'application (`db`, `cache`).

## Les services publics

Un service qui porte `public: true` reçoit un domaine et son certificat HTTPS. Indiquez dans `port` le port où il répond (80, 3000…). Il en faut au moins un ; le premier est le **service principal**, celui de l'adresse de l'application. Les autres services publics reçoivent chacun leurs propres noms à l'installation (et, sans nom, une adresse donnée par MyInfra). Les services non publics restent privés à l'application. Une tâche qui n'écoute rien porte `noPort: true`.

## Les disques

Ce qu'un service écrit hors de ses disques disparaît au redémarrage. Donnez-lui un disque (`volumes`) sur le dossier de ses données, avec une taille en Gio (`sizeGi`). Le premier disque est sauvegardé comme celui de toute application.

## Les disques partagés

Quand plusieurs services doivent lire et écrire les mêmes fichiers (un client de téléchargement et une médiathèque, par exemple), déclarez un disque partagé une fois, avec sa taille, et montez-le par son nom dans chaque service :

```yaml
disks:
  - name: media
    sizeGi: 200
services:
  - name: jellyfin
    volumes:
      - path: /config
        sizeGi: 8
      - path: /data
        disk: media
```

Un volume qui cite un disque partagé n'a pas de taille : c'est celle du disque. Les services qui montent un disque partagé tournent en une seule copie, tous sur le même nœud, celui qui porte le disque. Le disque reste tant qu'un service le monte.

## Les fichiers de configuration

Un fichier (`files`) est posé en lecture seule dans le conteneur, au chemin indiqué. Changer un fichier redémarre les copies du service.

## Les variables et les secrets

Les variables communes (`env` en haut du paquet) vont à tous les services et sont chiffrées : mettez-y les mots de passe, avec `{{secret:nom}}`. Le même nom donne le même mot de passe partout, tiré à chaque installation. C'est la seule place des `{{secret:…}}`. Les variables d'un service (`env` dans le service) ne vont qu'à lui, sans secret.

## L'ordre de démarrage

`dependsOn` fait patienter un service jusqu'à ce que ceux qu'il cite répondent sur leur port : le site attend sa base. Pas de boucle.

## Santé et ressources

`health.http` donne le chemin HTTP qui répond quand une copie est prête (et `health.port` s'il diffère) : une mise à jour qui ne démarre pas laisse l'ancienne version en place. `resources.cpu` réserve du processeur (`0.5` ou `500m`), `resources.memory` plafonne la mémoire (`512Mi`, `2Gi`).

## Ce que MyInfra remplit pour vous

- `{{url}}` : l'adresse de l'application (son service principal), `https://…` ; elle suit le domaine.
- `{{host}}` : son nom de domaine seul ; il suit le domaine.
- `{{url:service}}`, `{{host:service}}` : l'adresse et le nom d'un autre service public, par exemple `{{url:jellyfin}}`.
- `{{secret:nom}}` : un mot de passe aléatoire tiré à l'installation, le même partout où « nom » revient. Variables communes seulement.
- `{{encoded:nom}}` : le même, encodé en base64, pour les applications qui l'attendent ainsi.
- `{{input:clé}}` : la réponse à une question posée à l'installation, déclarée dans `inputs`.

## Chaque champ du format

- `name`, `summary`, `category` : le nom, le résumé et la catégorie qui présentent le paquet. Catégories : Sites, Collaboration, Code et données, Identité, Fichiers et tableaux, Automatisation, Échanges, Notes et liens, Comptes personnels, Médias, Outils.
- `inputs` : les questions posées à l'installation, `{key, label}` ; `key` est en minuscules, lu par `{{input:key}}`.
- `env` : les variables communes à tous les services, chiffrées.
- `services[].name`, `services[].image` : le nom du service et son image, par exemple `postgres:16`.
- `services[].public`, `services[].port` : `public: true` pour un service qui reçoit un domaine (le premier est le principal) ; `port`, celui où le service répond.
- `services[].noPort` : `true` pour une tâche de fond qui n'écoute aucun port.
- `services[].replicas` : le nombre de copies, de 1 à 10 (0 pour un service arrêté).
- `services[].command`, `services[].args` : `command` remplace le point d'entrée de l'image ; `args` le suit, un mot par élément.
- `services[].ports` : des ports de plus, `{name, port}`, ouverts aux autres services seulement.
- `disks` : les disques partagés, `{name, sizeGi}`.
- `services[].volumes` : les disques, `{path, sizeGi}` ; le premier est le disque principal. `{path, disk}` monte un disque partagé.
- `services[].files` : les fichiers posés en lecture seule, `{path, content}`.
- `services[].env` : les variables de ce service seul, sans secret.
- `services[].health` : `{http, port}`.
- `services[].resources` : `{cpu, memory}`.
- `services[].dependsOn` : les services à attendre avant de démarrer.
- `about` : facultatif ; `site`, `logo` (nom Simple Icons), `description`, et `translations` (`en`, `pt`, `it`, `de`) avec `summary` et `description`.

## Un paquet complet

Umami et sa base :

```yaml
type: myinfra-package
version: 1
name: Umami
summary: Une mesure d'audience sobre.
category: Sites
env:
  APP_SECRET: '{{secret:SECRET}}'
  DATABASE_URL: postgresql://umami:{{secret:DB}}@postgres:5432/umami
  POSTGRES_DB: umami
  POSTGRES_PASSWORD: '{{secret:DB}}'
  POSTGRES_USER: umami
services:
  - name: web
    image: umamisoftware/umami:postgresql-v2.17.0
    public: true
    port: 3000
    dependsOn: [postgres]
    health:
      http: /api/heartbeat
  - name: postgres
    image: postgres:16
    port: 5432
    volumes:
      - path: /var/lib/postgresql/data
        sizeGi: 8
```

## Partir d'un fichier existant

`convert_to_package` transforme un docker-compose, des manifests Kubernetes ou un chart Helm en paquet, sans rien enregistrer : relisez le résultat et ses avertissements avant `create_package`.
