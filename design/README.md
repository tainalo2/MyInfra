# Design

Le design system de MyInfra et les maquettes des propositions UI/UX. Rien ici n'est compilé dans l'application : c'est la référence pour faire évoluer `web/src`.

## `system/` — le design system

- `README.md` : le brand book (ton, couleur, typographie, espace, états, composants, plan, téléphone, iconographie). Commencez par lui.
- `tokens.json` : les tokens (couleurs claires et sombres, typographie, espacements, rayons, ombres, tailles, opacités).
- `components/<Nom>/README.md` et `preview.html` : les règles et l'aperçu de chaque composant. Les composants viennent de `web/src/ui.tsx`, `web/src/kit.tsx` et `web/src/mobilenav.tsx` ; `Plan`, `BottomTabs` et `ListRow` décrivent des motifs des propositions, pas encore du code.
- `design-system.json` : l'index du design system.

Non copiés ici, car générés ou tirés de paquets : `components/bundle.js` et `bundle.css` (compilés depuis `web/src`), les polices Geist (`@fontsource-variable/geist` et `geist-mono` 5.2.6) et les logos (`web/public/favicon.svg`, `deploy/macos/icon.png`).

## `canvas/` — les maquettes

Un canvas Claude Design : `canvas.json` place les écrans, chaque `*.dc.html` en est un.

- `Proposition-*.dc.html` : les écrans pour ordinateur (1440px), dont l'éditeur de paquet sur une seule page (`Proposition-PaquetPlan`) et la fiche d'une application déployée avec son plan (`Proposition-AppPlan`).
- `Mobile-*.dc.html` : les écrans de téléphone (390px), conçus à part : barre d'onglets en bas, listes à icônes, pas de plan.

Les écrans chargent le runtime du canvas (`support.js`) et les composants du design system (`ds/myinfra/components/bundle.js`), fournis par Claude Design : ils s'ouvrent dans le canvas, pas directement dans un navigateur. Les données affichées (serveurs, pods, adresses, mesures, prix « [prix] ») sont des exemples.

## `tools/`

- `gen-mobile.py` : régénère les écrans `Mobile-*.dc.html` à partir de blocs communs. `python3 design/tools/gen-mobile.py design/canvas`.
