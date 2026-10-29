# IconButton

Un bouton fait d'une seule icône, quand l'icône suffit à distinguer l'action : copier, révéler, supprimer.

- `label` est obligatoire : il devient `aria-label` et `title`.
- Icône lucide à 16px, trait 1,5, en `color-ink` ; zone de 44px sur écran tactile grâce à un padding compensé par une marge négative.
- N'utilisez pas d'icône seule pour une action qui n'a pas de symbole évident : prenez un `TextButton`.

Source : `web/src/ui.tsx`.
