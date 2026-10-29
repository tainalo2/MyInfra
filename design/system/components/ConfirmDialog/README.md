# ConfirmDialog

Demande avant une action. Avec `phrase`, la personne la tape d'abord : pour ce qui ne se défait pas.

- Le texte dit ce qui disparaît, en `color-muted`. Le bouton de confirmation est `danger` si `danger`, sinon `primary` ; « Annuler » est un `TextButton`.
- Il affiche lui-même l'erreur d'une action qui échoue (`ErrorText`), et la page derrière ne bouge pas.
- Le consommateur fournit `title`, `confirmLabel`, `onConfirm` (peut renvoyer une promesse), `onClose`.

Source : `web/src/kit.tsx`.
