# Plan

Le schéma d'un paquet ou d'une application : ses éléments en trois bandes (Entrée, Services, Stockage) et les liens qui les unissent ; choisir un élément ouvre ses réglages dessous.

- Pas encore un composant du code : un motif dessiné dans les propositions du 29 octobre 2026 (`design/canvas/Proposition-PaquetPlan.dc.html`, `Proposition-AppPlan.dc.html`).
- Cases : `color-surface`, filet `#d6d3d1`, `radius-md` (Internet en `radius-full`) ; stockage sur `color-canvas`, sauvegardes en filet pointillé. Élément choisi : bordure 2px `color-ink` ; le reste à `opacity-dimmed`.
- Liens : six styles fixes (voir le brand book, section Plan) ; un port ouvert directement est toujours en `color-warn`.
- Les liens de l'élément choisi défilent (tirets animés), même avec moins de mouvement demandé ; le plan se compacte au défilement (`plan-compact-after`) et reste collé avec la tête de l'élément et ses onglets.
- Masqué sous 720px : sur téléphone, les mêmes éléments en `ListRow`.
- Le consommateur fournit les éléments (clé, type, nom, détail, état) et les liens (de, vers, type, détail).
