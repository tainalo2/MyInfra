# ActionsMenu

Le menu « … » d'un objet : les actions moins courantes d'une application, d'un serveur ou d'un cluster.

- Le consommateur fournit `label` (nom accessible du bouton) et des `MenuItem` en enfants.
- Menu `color-surface`, filet `color-line`, `radius-md`, padding `space-1` ; il s'ouvre vers la gauche quand la place manque à droite, se ferme sur Échap ou un clic à l'extérieur.
- L'action destructrice vient en dernier, en `tone="danger"`, et ouvre un `ConfirmDialog`.

Source : `web/src/ui.tsx`.
