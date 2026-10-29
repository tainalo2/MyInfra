MyInfra met une application en ligne sans demander de connaître Docker ni Kubernetes. L'interface est **claire, large, et presque sans couleur** : du texte en encre sur un fond pierre, des filets plutôt que des ombres, une action principale par écran. La couleur ne sert qu'au bouton de cette action et à l'état d'un domaine, d'un serveur ou d'un déploiement. Il n'y a pas de couleur de marque.

## Ton et contenu

- Parlez d'**applications, de serveurs et d'adresses**. Jamais à l'écran : ingress, namespace, port 443, classe d'ingress, type d'instance, cloud-init, Swarm. Un serveur se choisit par un lieu et une taille (petit, moyen, grand).
- Phrases courtes, déclaratives, au présent. Le vouvoiement en français : « Tapez le nom de l'application pour confirmer. »
- Une liste vide le dit simplement, avec un point : « Aucune application. », « Aucun cluster. » — puis un seul bouton pour en créer un.
- Une action irréversible fait taper un mot pour confirmer et dit ce qui disparaît : « Ses noms de domaine sont retirés, ainsi que ses charges sur le cluster. »
- Majuscule en début de libellé seulement (« Clés SSH », « Nouvelle application »). Pas d'emoji, pas de point d'exclamation.
- Cinq langues : anglais, français, allemand, italien, portugais. Prévoyez des libellés jusqu'à un tiers plus longs qu'en anglais.
- Chaque page nomme son onglet : « Applications · MyInfra ».
- Noms des réglages : **Hébergeurs** (pas « Providers »), **Domaines** (comptes DNS et adresses réunis), **Images** (registres privés et images importées réunis), **API et MCP** (pas « Assistants IA »), **Git**.
- Exposition d'un service, deux mots seulement : « **Par le proxy** » (un domaine et son certificat HTTPS, ports 80 et 443 tenus par MyInfra) et « **Port ouvert directement** » (« Ports ouverts sur Internet » : TCP ou UDP sur l'adresse de chaque serveur, sans proxy). Jamais « ingress », « LoadBalancer » ni « hostPort ».

## Couleur

- Page sur `color-canvas` ; cartes, champs, menus et feuilles sur `color-surface`, bordés de `color-line` (1px).
- Texte principal en `color-ink`. Libellés, métadonnées et messages vides en `color-muted`. Aides et détails techniques en `color-faint`, jamais pour une phrase importante, et jamais sur `color-status` (4,4:1).
- Survol : le fond passe à `color-status` (boutons, options de menu, liens de navigation, cartes cliquables) ; la bordure d'un contrôle bordé passe à `color-ink`.
- États : `color-ok`, `color-warn`, `color-bad`, sur le statut seulement — une pastille de 8px `radius-full` **et** le mot (« En cours », « Échec »). Une couleur d'état ne sert jamais seule.
- `color-bad` sert aussi aux actions destructrices (texte) et au fond du bouton `danger`.
- `color-warn` marque aussi un **port ouvert directement** sur Internet (le lien sur un plan, la bordure du service, le mot « direct ») : c'est une exposition à surveiller, pas une erreur.
- `color-link` pour un lien dans une phrase, souligné au survol avec un décalage de 3px. Les liens de navigation restent en `color-ink`.
- Les `color-code-*` ne servent que dans l'éditeur de code et la coloration des fichiers YAML/JSON.
- Thème sombre : mêmes rôles, mêmes noms, valeurs inversées sur la pierre (`html.dark`). Rien ne change de rôle d'un thème à l'autre.

## Typographie

- **Geist** pour tout le texte, **Geist Mono** seulement pour une commande à coller, une empreinte ou un nom de domaine.
- Corps en `body` (15px, interligne 1,5). Les champs passent à 16px sur téléphone, pour qu'iOS ne zoome pas.
- Titre de page en `page-title` (24px, graisse 500), `page-title-mobile` (20px) sous 768px. Titre de section en `section-title` : 16px, graisse normale — la hiérarchie vient de l'espace, pas de la graisse.
- Métadonnées et chiffres en `small` (13px), légendes en `caption` (12px).
- Deux graisses seulement : 400 et 500. Pas de gras, pas d'italique, pas de capitales forcées.

## Espace et mise en page

- Pas de 4px ; les valeurs courantes sont `space-2`, `space-4`, `space-6` et `space-12`.
- Bouton : `space-2` vertical, `space-4` horizontal, `space-2` entre icône et texte. Champ : `space-2` × `space-3`. Carte : `space-4` de padding, `space-3` entre ses lignes.
- Gouttière de page `space-4` sur téléphone, `space-6` à partir de 768px. Titre de page suivi de `space-6` (téléphone) ou `space-12` (ordinateur).
- Listes : deux colonnes de cartes à partir de 768px (`space-4` d'écart) ou des lignes séparées par un filet `color-line` en haut, au choix de la personne.
- Un formulaire reste dans une colonne d'environ 720px.
- Sur écran tactile, tout bouton atteint `touch-target` (44px).

## Rayons, bordures, ombres

- Un seul rayon : `radius-md` (6px), pour les boutons, champs, cartes, menus, toasts et dialogues. `radius-full` pour les pastilles et les spinners.
- Les surfaces se séparent par un filet `color-line`, pas par une ombre. `shadow-sm` seulement pour ce qui flotte au-dessus du contenu (panneaux de l'aperçu, toasts, menu d'activité) ; `shadow-lg` seulement pour la palette de commandes.
- Pas de dégradé, pas d'illustration, pas de carte à bordure gauche colorée.

## États et interaction

- Focus : `outline` 2px en `color-focus`, décalé de 2px, au clavier seulement (`focus-visible`). Un champ actif prend une bordure `color-focus`. Pas d'anneau bleu.
- Désactivé : `opacity-disabled` (0,4), curseur normal.
- Bouton principal survolé : `opacity-primary-hover` (0,84) sur `color-ink`.
- Transitions de 120ms `ease`, seulement sur le fond, la bordure et l'opacité, et seulement sur un appareil qui survole. Pas d'autre animation que le spinner, la pulsation des squelettes **et les liens d'un plan** (voir Plan).
- Chargement : des barres `color-line` `radius-sm` qui pulsent, à la forme de ce qui arrive, avec un libellé « Chargement » pour les lecteurs d'écran.

## Composants

Les composants sont ceux de l'application, compilés tels quels : `window.MyInfra.Button`, `MyInfra.Field`, etc. (React 18 ou plus). Chargez `tokens.css`, puis `components/bundle.css`, puis `components/bundle.js`. Appelez `MyInfra.setLang("fr")` pour choisir la langue des libellés intégrés (« Annuler », « Copier », les statuts).

- **Actions** — `Button` (`primary` pour la seule action principale de l'écran, `secondary`, `danger` dans une confirmation), `TextButton` pour toutes les autres actions, `IconButton` quand l'icône suffit, `ActionsMenu` + `MenuItem` pour les actions rares d'un objet, `CopyButton` après une valeur.
- **Formulaires** — chaque contrôle (`Input`, `Select`, `TextArea`) dans un `Field` ; plusieurs contrôles sous un même nom dans un `Group` ; `ErrorText` au-dessus des boutons.
- **État** — `StatusBadge` (pastille + mot) partout où la place le permet, `HealthDot` seul dans une ligne dense ; `LoadingLines` ou `Skeleton` pour une page qui charge, `Spinner` dans un bouton.
- **Navigation et dialogues** — `Tabs` entre les parties d'un objet, `Fold` pour ce qui sert rarement, `Sheet` pour un formulaire court, `ConfirmDialog` avant toute suppression (avec `phrase` si elle ne se défait pas).
- **Mise en page** — `PageTitle` (avec l'action principale à droite), `SectionTitle`, `Icon`.

## Plan d'un paquet ou d'une application

Un plan montre ce qui compose un paquet ou une application déployée et ce qui parle à quoi. Il sert aussi de navigation : choisir un élément ouvre ses réglages juste en dessous, sur la même page.

- **Trois bandes** de fond très clair, nommées en `caption` `color-faint` : **Entrée** (Internet, en pastille ronde, et le Proxy), **Services**, **Stockage** (disques et sauvegardes). Une case par élément : icône, nom, une ligne de détail en `small` `color-muted`, et la pastille d'état sur une application déployée.
- **Six liens**, toujours dans ces styles (filet sans échelle, `vector-effect: non-scaling-stroke`) :

| Lien | Trait |
|---|---|
| Par le proxy | `color-ink`, plein, 2px |
| Port ouvert directement | `color-warn`, plein, 2px |
| Réseau interne | `color-muted`, tirets 6/4, 1,5px |
| Disque monté | `color-muted`, pointillé 2/3, 2,5px |
| Redondance (réplication, exemplaires) | `color-ink`, tiret-point 10/3/2/3, 1,5px |
| Sauvegarde | `color-faint`, points 1/5, bouts ronds |

- **Sélection** : l'élément choisi prend une bordure 2px `color-ink` ; ses liens s'épaississent et **défilent** (tirets animés, 1,1s linéaire) ; le reste tombe à `opacity-dimmed`, les liens sans rapport à `opacity-link-dimmed`. Cette animation reste même quand le système demande moins de mouvement : c'est elle qui dit sur quoi on travaille. Seules les transitions de mise en page s'arrêtent alors.
- **Toujours sous les yeux** : le plan et la tête de l'élément choisi (type, nom, onglets) forment un bloc collant en haut de page. Au défilement (au-delà de 200px, retour sous 40px), le plan se **compacte** : cases réduites au nom, étiquettes des liens masquées, liens toujours tracés. Pas de bouton pour réduire ou agrandir.
- **Une seule page d'édition** : choisir un élément change les onglets en dessous (service : Général, Réseau, Stockage, Variables, Santé, Ressources, Connexions ; stockage : Taille et redondance, Sauvegardes, Connexions). Un onglet commun aux deux éléments reste ouvert.
- Le bouton **Ajouter** propose Service ou Stockage ; l'élément apparaît en pointillé dans sa bande tant qu'il n'est pas validé. **Interactions · N** déplie le tableau de tous les liens (De, Vers, Type, Détail, Ouvert à ou Serveur), replié par défaut.
- Sur une application déployée, le plan est **simplifié** par défaut (une case par service, avec ses copies et ses serveurs en une ligne) ; « Voir tous les pods » montre une colonne par serveur, un pod par case, la réplication et les exemplaires. Cliquer dans le plan hors d'un élément revient à « Toute l'application ».
- **Pas de plan sur téléphone** : il est masqué sous 720px ; les mêmes éléments s'y lisent en listes.

## Téléphone

Un écran de téléphone est conçu pour lui, pas réduit depuis l'ordinateur : l'essentiel, en listes, avec des icônes.

- **Barre du bas** de cinq onglets, `tab-bar-height` : Accueil, Apps, Serveurs, Activité, Plus (Templates, Mises à jour, Réglages, compte, langue, thème). Icône `icon-mobile` et mot en `micro` ; l'onglet actif en `color-ink`, les autres en `color-faint`. Un compteur `color-warn` sur Activité quand quelque chose tourne, un point `color-link` sur Plus quand une mise à jour attend.
- **En-tête** : retour « ‹ Réglages » en `color-link`, puis le titre en `page-title-mobile-large` ; une seule action, en icône (+, loupe, ⋯).
- **Lignes de liste** de `row-height-mobile` : tuile d'icône `icon-tile` sur `color-status`, nom en `body`, une sous-ligne en `small` `color-muted` (tronquée, jamais sur deux lignes), à droite un compteur, un mot ou une pastille d'état, puis un chevron. Les lignes se groupent dans une carte, titre de groupe en `small` `color-muted` au-dessus.
- **Textes courts** : un mot par réglage et un compteur (« Hébergeurs 2 », « Mon compte 2FA ») ; une version en flèche (« 0.14.2 → 0.15.0 ») ; les consignes en étapes numérotées.
- Filtres en **pastilles** qui défilent ; parties d'un objet en **contrôle segmenté** ; mesures en trois petites barres (CPU, RAM, Disque) ; actions d'une fiche en boutons ronds de 48px avec leur mot dessous.
- Bouton principal en pleine largeur, 48px de haut ; champs en pleine largeur, 44px.

## Iconographie

- **lucide-react**, à `icon` (16px) sur ordinateur et `icon-mobile` (20 à 22px, trait 1,75) sur téléphone, trait 1,5, couleur du texte (`currentColor`). Une icône seulement quand elle distingue une action (copier, révéler, supprimer) ou une rubrique de navigation ; un `IconButton` porte toujours un `aria-label`.
- Les applications de la bibliothèque montrent leur propre logo (simple-icons) dans une tuile, ou leur initiale quand il n'y en a pas.
- La marque est l'icône `Boxes` ; voir le groupe Logos.

## Non synchronisé

- Le plan, les écrans de téléphone et leurs mesures (bandes, liens, `row-height-mobile`, `tab-bar-height`, `icon-tile`) viennent des propositions UI/UX du 29 octobre 2026 (dossier `design/` du dépôt), pas encore du code de `web/src`.

- Synchronisé depuis `tainalo2/MyInfra` (`main@a1a9341`, dossier `web`) : variables de `web/src/index.css`, règles de `docs/cahier-des-charges.md`, comportements de `ui.tsx` et `kit.tsx`.
- Polices : seuls les sous-ensembles latin et latin-ext de Geist et Geist Mono (paquets `@fontsource-variable` 5.2.6) ; le cyrillique n'est pas repris.
- Les échelles d'espacement, de rayons, d'ombres et de tailles de texte viennent des classes Tailwind v4 utilisées dans le code, pas de variables déclarées : les valeurs sont celles de Tailwind 4.
- `color-line` sous un champ ne fait que 1,3:1 sur la surface (clair) et `color-faint` 4,4:1 sur `color-status` : valeurs du code, gardées telles quelles.
- Composants : 24 composants de `web/src/ui.tsx` et `web/src/kit.tsx` compilés depuis le code (Vite + esbuild, bundle IIFE). Non repris, car liés au routeur, à la session ou à l'API de l'application : `Header`, `Shell`, `AuthFrame`, `SectionTabs`, `AppsTabs`, `ListActions`, `EmptyList`, `ListModeButton`, `Steps`, `ToastHost`.
- `components/bundle.css` est la feuille Tailwind compilée de l'application, sans les couleurs, les polices ni `radius-md`, qui viennent de `tokens.css`. Le voile d'un `Sheet` reprend `color-ink` à 40 % : clair en thème sombre, comme dans l'application.
- Logos des applications tierces (simple-icons) non repris : ce ne sont pas des marques de MyInfra.
