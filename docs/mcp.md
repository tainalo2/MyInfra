# Piloter MyInfra depuis un assistant IA (MCP)

MyInfra embarque un serveur [MCP](https://modelcontextprotocol.io) (Model Context Protocol). Un assistant compatible (Claude Code, Claude Desktop, Cursor, VS Code, …) peut alors lire l'infrastructure et agir dessus en langage naturel : « déploie un Nextcloud sur le cluster SW avec cloud.mondomaine.fr », « trouve-moi trois serveurs de 8 Go en Europe à moins de 15 € par mois ».

## Mise en route

1. Dans MyInfra, ouvrez **Réglages › Assistants IA (MCP)**.
2. Créez un **jeton d'accès** : un nom (l'assistant et le poste, par exemple), un rôle et une durée. Le jeton (`myi_…`) n'est affiché qu'une fois.
3. Cliquez sur **Brancher mon assistant** : le pas-à-pas donne la configuration de chaque client, déjà remplie avec l'adresse et le jeton.

L'adresse est `<MYINFRA_PUBLIC_URL>/mcp`, en Streamable HTTP, avec l'en-tête `Authorization: Bearer <jeton>`. Pour Claude Code :

```sh
claude mcp add --transport http myinfra https://myinfra.exemple.fr/mcp \
  --header "Authorization: Bearer myi_…"
```

Ouvrir `/mcp` dans un navigateur mène à la page des réglages.

## Droits

- Un jeton porte un rôle (**Lecture seule**, **Déploiement**, **Administrateur**) et n'a jamais plus de droits que la personne qui l'a créé. Le rôle effectif est le plus bas des deux, relu à chaque requête : rétrograder une personne rétrograde ses jetons, la supprimer les supprime.
- Chaque outil appelle les routes de l'API par le même routeur que l'interface, avec le même jeton. Les rôles, la réplique, les opérations en cours et toutes les vérifications s'appliquent exactement comme dans l'interface.
- L'assistant ne voit que les outils que son rôle permet :
  - lecture seule : tout lire, chercher des offres, prévoir un changement ;
  - déploiement : installer, acheter, supprimer ;
  - administration : en plus, écrire le DNS.
- Un jeton ne crée ni ne révoque de jeton. Les jetons se gèrent depuis l'interface. Comme les sessions, chaque copie de MyInfra garde les siens.

## Confirmations

Acheter des machines, supprimer une machine, un cluster, une application ou un paquet, et toute modification DNS qui touche l'existant se font **en deux temps** :

1. Le premier appel ne fait rien. Il rend un récapitulatif et un code (`ABC-DEF`) :
   - pour un achat : les machines, le provider, le lieu, le prix, le coût théorique actuel et après achat, le budget ;
   - pour une suppression : ce qui sera détruit et l'économie ;
   - pour le DNS : les enregistrements avant et après.
2. L'assistant montre le récapitulatif et attend votre accord.
3. Le second appel, avec le code, agit. Le code n'est valable :
   - que pour le même jeton, le même outil et les mêmes arguments ;
   - que pendant 10 minutes, et une seule fois ;
   - que si rien n'a changé entre-temps (prix, enregistrements DNS).

   Dans le cas contraire, rien n'est fait et un nouveau récapitulatif est rendu.

C'est l'assistant qui relaie la question : ses instructions l'y obligent. Pour un usage sans aucun risque, donnez-lui un jeton en lecture seule.

Refusés par l'assistant, à faire depuis l'interface :
- détruire ce qui héberge MyInfra ;
- ajouter des identifiants de provider ou de DNS, et révéler un secret ;
- gérer les utilisateurs ou les jetons ;
- ouvrir un terminal, transférer MyInfra.

## Historique

Chaque action faite par un assistant est dans **Activité**, marquée **MCP** avec le nom du jeton. Cela vaut aussi pour les refus et les codes rejetés. Un jeton utilisé directement sur l'API est marqué **API**. Les dernières actions MCP sont aussi listées sur la page des réglages.

## Ce que l'on peut demander

### Explorer

- « Qu'est-ce qui tourne sur mon infra ? » · « Combien me coûte l'infra par mois ? »
- « Quels clusters j'ai, et combien de place reste-t-il sur SW ? » · « Quels domaines puis-je utiliser ? »
- « Quelles applis de stockage cloud existent dans le catalogue ? » · « Qu'a-t-on fait via MCP cette semaine ? »

### Acheter et construire

- « Trouve-moi le serveur le moins cher avec 4 vCPU et 8 Go en France. »
- « Trois serveurs chez Hetzner ou Scaleway, en Europe, à moins de 15 € par mois chacun, en cluster SW avec trois plans de contrôle. »
- « Je veux exactement un CCX23 à Helsinki. » S'il n'existe pas, l'assistant propose le modèle le plus proche ou un autre lieu.
- « Ajoute deux nœuds au cluster SW. »
- « Un serveur seul pour un petit site, budget 5 €. » S'il n'y en a pas : « rien sous 5 € avec 2 Go ; le plus proche est … à 5,49 € ».

La recherche respecte le provider, le pays, le continent, le lieu, le processeur, la mémoire, le disque (un volume est ajouté si l'offre le permet), le prix maximum et le modèle exact. Quand rien ne convient, elle rend les offres les plus proches, chacune avec ce qui lui manque, et dit pourquoi :
- aucun compte pour ce provider ;
- modèle inconnu ;
- devise différente : les prix ne sont jamais convertis.

### Paquets

- « Transforme ce docker-compose en paquet. » · « Importe le chart Helm bitnami/wordpress. » · « Convertis ces manifests Kubernetes. »
- « Écris un paquet Gitea avec PostgreSQL, un disque de 20 Gi, sur le port 3000. » L'assistant lit le guide `myinfra://guide/packages` et vérifie le paquet avant de l'enregistrer.

### Déployer

- « Déploie un Nextcloud redondé sur SW avec cloud.mondomaine.fr. » L'assistant dit service par service ce qui est vraiment redondé. Aujourd'hui seul Redis l'est : Nextcloud et MariaDB restent à une copie. Il demande votre accord avant d'installer.
- « Déploie github.com/moi/api@main avec une MariaDB, exposé sur le port 5050, sur SW », puis « garde-le comme paquet ».
- « Installe Uptime Kuma sur la machine vps-1. »

### Exploiter

- « Pourquoi Immich ne démarre pas ? » · « Redémarre le pod web de Nextcloud. » · « Mets Ghost en pause. »
- « Ajoute files.mondomaine.fr à Nextcloud. » · « Change SMTP_HOST de Ghost. » · « Passe Vikunja à trois copies. »
- « Quelles mises à jour sont disponibles, et quel est leur impact ? Fais celle de Gitea. »

### DNS

Les types écrits sont A, AAAA, CNAME, TXT et MX, chez Cloudflare et OVH. Les autres types sont seulement lus.

- « Liste les enregistrements de mondomaine.fr. »
- « Crée un CNAME docs.mondomaine.fr vers monorg.github.io. » Le nom est libre : il est créé tout de suite.
- « Fais pointer www vers mon nouveau serveur. » L'assistant montre l'avant et l'après, puis attend votre accord.
- « Mets en place les MX de Proton Mail. » Les MX remplacés sont listés, avec un avertissement sur le courrier.
- « Supprime old.mondomaine.fr. » · « Nettoie les noms orphelins laissés par l'ancien serveur. »
- « Change l'adresse de cloud.mondomaine.fr. » Refusé : c'est le nom d'une application, il se change avec ses domaines.

Une modification demande votre accord dans ces cas :
- elle remplace ou supprime un enregistrement existant ;
- un CNAME doit s'installer seul sur son nom ;
- elle masque un joker ;
- elle touche la racine du domaine, le courrier (MX, SPF, DKIM, DMARC) ou les certificats (`_acme-challenge`).

Les noms que MyInfra écrit pour les applications, et les NS et SOA, sont refusés.

## Détails techniques

- **Transport.** Streamable HTTP sans session :
  - chaque `POST /mcp` porte son jeton et reçoit sa réponse en JSON ;
  - `GET` et `DELETE` répondent 405 ;
  - versions du protocole : `2025-03-26`, `2025-06-18`, `2025-11-25`.
- **Erreurs.** Une erreur de l'API revient comme résultat d'outil (`isError`), avec le message en français et, pour un paquet, chaque problème avec sa ligne. L'assistant peut corriger et réessayer.
- **Opérations longues.** Achats, installations et suppressions continuent même si l'assistant cesse d'attendre. `get_order`, `get_application` et `get_destruction` suivent leur avancement. La construction d'une image Git peut dépasser le délai par défaut des clients : pour Claude Code, augmentez `MCP_TOOL_TIMEOUT`.
- **Coûts.** Les coûts sont ceux de la page d'accueil : prix horaire × 730 h, machines seules. La facture réelle peut différer : plafond mensuel, taxes, volumes.
- **Routes REST.** Elles servent aussi en dehors du MCP :
  - `POST /api/offers/search` ;
  - `GET /api/costs` ;
  - `GET /api/{servers,clusters,applications}/{id}/delete/impact` ;
  - `GET /api/dns/zones/{zone}/records` ;
  - `POST /api/dns/records/plan` ;
  - `PUT` et `DELETE /api/dns/records` ;
  - `GET`, `POST` et `DELETE /api/mcp/tokens`.
