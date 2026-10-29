# Button

Le bouton d'action : `primary` pour la seule action principale d'un écran, `secondary` pour une action bordée à côté, `danger` pour une suppression confirmée.

- `primary` : fond `color-ink`, texte `color-canvas` ; au survol, opacité `opacity-primary-hover`.
- `secondary` : `color-surface`, filet `color-line`, bordure `color-ink` au survol.
- `danger` : fond `color-bad`, texte `color-canvas` — dans un `ConfirmDialog`, pas directement sur une page.
- Padding `space-2` × `space-4`, `radius-md`, `space-2` entre icône et texte, 44px de haut sur écran tactile.
- Le consommateur fournit le libellé (un verbe : « Déployer », « Nouvelle application ») et, au besoin, un `Icon` en tête. Tous les attributs de `<button>` passent.
- Désactivé : `opacity-disabled`. Une action indisponible pendant un transfert garde son bouton désactivé avec un `title` qui dit pourquoi.

À ne pas faire : deux boutons `primary` sur un écran ; un bouton coloré pour une action ordinaire (utilisez `TextButton`).

Source : `web/src/ui.tsx`.
