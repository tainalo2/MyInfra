import type { SimpleIcon } from "simple-icons";
import {
  siActualbudget,
  siAffine,
  siAppsmith,
  siAudiobookshelf,
  siAuthentik,
  siBaserow,
  siBookstack,
  siCalibreweb,
  siCaldotcom,
  siChangedetection,
  siChatwoot,
  siCoder,
  siCryptpad,
  siDiagramsdotnet,
  siDirectus,
  siDocker,
  siExcalidraw,
  siFireflyiii,
  siForgejo,
  siFormbricks,
  siFreshrss,
  siGhost,
  siGhostfolio,
  siGrafana,
  siGrav,
  siHedgedoc,
  siHomepage,
  siImmich,
  siInvoiceninja,
  siJellyfin,
  siJoplin,
  siKeycloak,
  siListmonk,
  siMattermost,
  siMatomo,
  siMeilisearch,
  siMetabase,
  siN8n,
  siNextcloud,
  siNtfy,
  siOutline,
  siPaperlessngx,
  siPenpot,
  siPocketbase,
  siSearxng,
  siTrilium,
  siUmami,
  siUptimekuma,
  siVaultwarden,
  siVerdaccio,
  siVikunja,
  siWallabag,
  siWikidotjs,
  siWordpress,
} from "simple-icons";

const logos: Record<string, SimpleIcon> = {
  actualbudget: siActualbudget,
  affine: siAffine,
  appsmith: siAppsmith,
  audiobookshelf: siAudiobookshelf,
  authentik: siAuthentik,
  baserow: siBaserow,
  bookstack: siBookstack,
  calibreweb: siCalibreweb,
  caldotcom: siCaldotcom,
  changedetection: siChangedetection,
  chatwoot: siChatwoot,
  coder: siCoder,
  cryptpad: siCryptpad,
  diagramsdotnet: siDiagramsdotnet,
  directus: siDirectus,
  docker: siDocker,
  excalidraw: siExcalidraw,
  fireflyiii: siFireflyiii,
  forgejo: siForgejo,
  formbricks: siFormbricks,
  freshrss: siFreshrss,
  ghost: siGhost,
  ghostfolio: siGhostfolio,
  grafana: siGrafana,
  grav: siGrav,
  hedgedoc: siHedgedoc,
  homepage: siHomepage,
  immich: siImmich,
  invoiceninja: siInvoiceninja,
  jellyfin: siJellyfin,
  joplin: siJoplin,
  keycloak: siKeycloak,
  listmonk: siListmonk,
  mattermost: siMattermost,
  matomo: siMatomo,
  meilisearch: siMeilisearch,
  metabase: siMetabase,
  n8n: siN8n,
  nextcloud: siNextcloud,
  ntfy: siNtfy,
  outline: siOutline,
  paperlessngx: siPaperlessngx,
  penpot: siPenpot,
  pocketbase: siPocketbase,
  searxng: siSearxng,
  trilium: siTrilium,
  umami: siUmami,
  uptimekuma: siUptimekuma,
  vaultwarden: siVaultwarden,
  verdaccio: siVerdaccio,
  vikunja: siVikunja,
  wallabag: siWallabag,
  wikidotjs: siWikidotjs,
  wordpress: siWordpress,
};

function brandTile(hex: string) {
  const full = hex.length === 3 ? hex.split("").map((part) => part + part).join("") : hex;
  const red = Number.parseInt(full.slice(0, 2), 16);
  const green = Number.parseInt(full.slice(2, 4), 16);
  const blue = Number.parseInt(full.slice(4, 6), 16);
  const luminance = (0.2126 * red + 0.7152 * green + 0.0722 * blue) / 255;
  if (luminance > 0.85) return { background: "#292524", fill: "#ffffff" };
  if (luminance > 0.62) return { background: `#${full}`, fill: "#1c1917" };
  return { background: `#${full}`, fill: "#ffffff" };
}

export function AppLogo({ slug, name, size = 40 }: { slug: string; name: string; size?: number }) {
  const icon = logos[slug];
  const glyph = Math.round(size * 0.58);
  if (!icon) {
    return (
      <span
        className="inline-flex shrink-0 items-center justify-center rounded-md bg-ink font-medium text-canvas"
        style={{ width: size, height: size, fontSize: glyph }}
        aria-hidden
      >
        {name.slice(0, 1)}
      </span>
    );
  }
  const tile = brandTile(icon.hex.toLowerCase());
  return (
    <span
      className="inline-flex shrink-0 items-center justify-center rounded-md"
      style={{ width: size, height: size, background: tile.background }}
      aria-hidden
    >
      <svg width={glyph} height={glyph} viewBox="0 0 24 24">
        <path fill={tile.fill} d={icon.path} />
      </svg>
    </span>
  );
}
