# ListRow

La ligne d'une liste sur téléphone : une tuile d'icône, un nom, une sous-ligne, un état à droite et un chevron.

- Hauteur `row-height-mobile`, padding `space-2` × `space-4`, `space-3` entre les parties.
- Tuile `icon-tile` sur `color-status`, `radius-md`, icône `icon-mobile` (20px) en `color-muted` — ou l'initiale de l'application en `body-medium`.
- Nom en `body`, sous-ligne en `small` `color-muted`, chacun sur une ligne (tronqué, jamais replié). Une sous-ligne d'état prend sa couleur (`color-warn` : « Redémarre »).
- À droite : un compteur ou un mot en `color-muted`, ou une pastille d'état ; un chevron 18px `#a8a29e` quand la ligne mène ailleurs. Une ligne destructrice : texte et icône `color-bad`, sans chevron.
- Les lignes se rangent dans une carte `color-surface` bordée de `color-line`, séparées par un filet qui commence après la tuile.
