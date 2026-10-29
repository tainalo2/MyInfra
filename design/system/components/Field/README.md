# Field

Un libellé, un contrôle et une aide facultative, empilés.

- Libellé en `color-muted` au-dessus (`space-2`), aide en `color-faint` en dessous (`space-2`).
- Le consommateur fournit `label`, `hint` (texte ou nœud) et un seul contrôle en enfant : `Input`, `Select` ou `TextArea`. Le tout est un `<label>` : cliquer le libellé focalise le champ.
- Plusieurs contrôles sous un même nom : utilisez `Group`.

Source : `web/src/ui.tsx`.
