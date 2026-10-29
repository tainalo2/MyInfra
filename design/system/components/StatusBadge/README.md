# StatusBadge

Dit un statut en mots et en couleur : une pastille de 8px et le libellé, dans la teinte de l'état.

- `color-ok` : en cours, prêt, lié, terminé. `color-warn` : en préparation, en déploiement, en attente du DNS, en suppression. `color-bad` : échec, injoignable, disparu. `color-muted` : en pause, annulé, non publié, inconnu.
- Le consommateur fournit `status` (le code de l'API) ; le libellé suit la langue de l'interface.

Source : `web/src/kit.tsx`.
