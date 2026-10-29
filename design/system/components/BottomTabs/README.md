# BottomTabs

La navigation d'un téléphone : cinq onglets en bas, à portée du pouce — Accueil, Apps, Serveurs, Activité, Plus.

- Hauteur `tab-bar-height` plus la zone sûre ; fond `color-surface`, filet `color-line` au-dessus.
- Icône `icon-mobile` et mot en `micro` ; onglet actif en `color-ink`, les autres en `color-faint`.
- Compteur `color-warn` sur Activité quand un déploiement tourne ; point `color-link` sur Plus quand une mise à jour attend.
- « Plus » ouvre une feuille : Templates, Mises à jour, Réglages, le compte, la langue et le thème.
- Source : `web/src/mobilenav.tsx` (`BottomTabs`, non compilé ici car lié au routeur).
