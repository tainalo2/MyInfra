---
name: myinfra-mcp
description: >-
  Alimente le serveur MCP de MyInfra à chaque ajout, modification ou
  suppression de fonctionnalité (écran, route, comportement utilisateur).
  À suivre dans MyInfra dès qu'une feature change, avant de conclure.
---

# MCP à jour avec les fonctionnalités

Une fonctionnalité que l'interface ou l'API expose doit l'être aussi pour l'assistant, dans le même changement. Une fonctionnalité retirée ou dont le contrat change doit disparaître ou suivre. Ne pas laisser le MCP en retard.

## Quand

À chaque ajout, modification ou suppression qui change ce qu'une personne peut faire : route HTTP, écran, champ, confirmation, rôle. Un détail interne sans effet pour la personne (refactor, commentaire) n'ajoute pas d'outil.

## Quoi faire

1. Outil dans `internal/api/mcp_tools_*.go`, enregistré dans `mcpTools` (`internal/api/mcp_tool.go`). Nom en snake_case, 3 à 41 caractères.
2. Le rôle de l'outil est celui de sa route la plus exigeante (`read`, `deploy`, `admin`). Chaque route déclarée dans `Routes` existe. Un outil `deploy` ou `admin` a au moins une route qui refuse le rôle d'en dessous.
3. L'outil appelle l'API par le routeur, comme les outils déjà là. Il ne duplique pas la logique.
4. Achat, suppression, DNS qui touche l'existant, application OpenTofu, lancement de modèle, mise à jour de MyInfra, prise ou passation de main : deux temps, champ `confirmation`, `Destructive: true`.
5. Un secret s'écrit et ne se relit pas. Ne pas le remettre dans la réponse, l'erreur ou l'historique.
6. Si l'assistant doit changer de comportement (nouveau deux-temps, nouvelle limite), une ligne dans `mcpInstructions` (`internal/api/mcp.go`) et dans `docs/mcp.md`.
7. Suppression : retirer l'outil, sa doc et la phrase d'instruction qui n'a plus lieu.

Rester dans l'interface, sans outil : terminal, transfert de MyInfra, utilisateurs et jetons, destruction de ce qui héberge MyInfra, téléchargement d'une clé privée, dépôt du fichier d'une sauvegarde ou d'une archive d'image.

## Vérifier

`go test ./internal/api/ ./internal/offerfind/ -count=1`

Les tests `TestMCPToolsAreWellFormed` et `TestMCPToolsFollowRoles` couvrent le catalogue. Un outil qui décide quelque chose (filtre, accord, secret masqué) a un test qui l'exerce.
