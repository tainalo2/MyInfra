# Catalogue

Chaque application du catalogue est un paquet MyInfra : un fichier `<id>.yaml`, au même format que les paquets que l'on crée dans l'interface (Applications > Paquets). Ces fichiers sont intégrés au binaire.

## Ajouter une application

1. Écrivez `<id>.yaml`. L'identifiant est en minuscules, avec des tirets (`uptime-kuma`). Il ne change plus ensuite : les templates y font référence.
2. Ajoutez l'identifiant dans `order.txt`, à la place où l'application doit apparaître. L'ordre des catégories suit celui de leur première application.
3. Lancez `go test ./internal/market/`.

Le plus simple est de partir d'une application proche, ou de l'écrire dans l'éditeur de paquets puis de l'exporter en YAML.

## Ce que le catalogue demande en plus d'un paquet

- `name`, `summary` et `category`. La catégorie est l'une de celles de `market.go` (`Sites`, `Collaboration`…).
- Un bloc `about` avec :
  - `site` : une adresse https ;
  - `logo` : le nom Simple Icons, ou un logo de `web/src/logos.tsx` ;
  - `description` : 80 caractères au moins ;
  - `translations` : `summary` et `description` pour `en`, `pt`, `it` et `de`.
- Chaque question (`inputs`) doit être lue quelque part, avec `{{input:clé}}`.

## Ce que les tests vérifient

Si un fichier est invalide, le chargement échoue et le message donne le fichier, la ligne et le problème. Une application démarre au boot sans que l'on ait à la tester à la main, et les starters (`starters.go`) ne citent que des applications sans question.

## Variables

Les variables du paquet (`env`) sont chiffrées et données à tous les services.

- `{{secret:NOM}}` tire un mot de passe à l'installation. Le même nom donne le même mot de passe partout, par exemple pour la base et l'application.
- `{{url}}` et `{{host}}` suivent le domaine de l'application à chaque livraison.

Le guide de l'éditeur de paquets détaille chaque champ.
