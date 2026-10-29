# Tabs

Les onglets qui passent d'une partie à l'autre d'un même objet ; les flèches gauche et droite s'y déplacent.

- Onglet actif : texte `color-ink`, soulignement 2px `color-ink` ; les autres en `color-muted`. Un filet `color-line` court dessous.
- `mark: true` ajoute une pastille `color-bad` : un problème dans cet onglet.
- Le consommateur fournit `tabs`, `active`, `onChange` et `label`. Pour passer d'une page à l'autre, l'application utilise `SectionTabs` (des liens).

Source : `web/src/kit.tsx`.
