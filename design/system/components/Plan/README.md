# Plan

Le schéma d'un paquet, d'une application ou d'un modèle : ses éléments en bandes (Entrée, Applications, Services, Machines, Stockage, seulement celles qui ont du contenu) et les liens qui les unissent ; choisir un élément ouvre ses réglages dessous.

- Dans le code : `web/src/plan.tsx` (`Plan`, `PlanPanel`, et `packagePlan`, `appPlan`, `templatePlan` qui construisent le modèle). Maquettes : `design/canvas/Proposition-PaquetPlan.dc.html`, `Proposition-AppPlan.dc.html`.
- Cases : `color-surface`, filet `#d6d3d1`, `radius-md` (Internet en `radius-full`) ; stockage sur `color-canvas`, sauvegardes en filet pointillé. Élément choisi : bordure 2px `color-ink` ; le reste à `opacity-dimmed`.
- Liens : sept styles fixes (voir le brand book, section Plan) ; un port ouvert directement est toujours en `color-warn`. Un lien interne vient de ce que les services déclarent (`dependsOn`), jamais d'une déduction. Dans une bande, un lien passe sous les cases entre ses deux bouts.
- Direction : le plan prend celle qui le rend le moins haut, de haut en bas si les colonnes sont trop étroites pour les cases ; un bouton la force. De gauche à droite, les étiquettes des liens se lisent dans le tableau Interactions.
- Les liens de l'élément choisi défilent (tirets animés), même avec moins de mouvement demandé ; le plan se compacte au défilement (`plan-compact-after`) sur une seule ligne, bandes côte à côte et une case par bande, et reste collé avec la tête de l'élément et ses onglets.
- Masqué sous 768px : sur téléphone, les mêmes éléments en `ListRow`. Ce qui suit le plan n'est monté que du côté affiché.
- Le consommateur fournit les éléments (clé, type, nom, détail, état) et les liens (de, vers, type, détail).
