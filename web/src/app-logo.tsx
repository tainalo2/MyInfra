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
  siRadarr,
  siSonarr,
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
  radarr: siRadarr,
  sonarr: siSonarr,
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

// Official logos of the projects simple-icons lacks, taken as published in
// their own repositories (see public/logos/SOURCES.md).
const files: Record<string, string> = {
  activepieces: "/logos/activepieces.svg",
  bazarr: "/logos/bazarr.png",
  cyberchef: "/logos/cyberchef.svg",
  dashy: "/logos/dashy.png",
  docmost: "/logos/docmost.png",
  docuseal: "/logos/docuseal.svg",
  dokuwiki: "/logos/dokuwiki.svg",
  etherpad: "/logos/etherpad.svg",
  filebrowser: "/logos/filebrowser.svg",
  freescout: "/logos/freescout.png",
  gatus: "/logos/gatus.svg",
  gotify: "/logos/gotify.png",
  healthchecks: "/logos/healthchecks.svg",
  homer: "/logos/homer.svg",
  huginn: "/logos/huginn.svg",
  "it-tools": "/logos/it-tools.png",
  jellyseerr: "/logos/jellyseerr.svg",
  kavita: "/logos/kavita.svg",
  komga: "/logos/komga.svg",
  lidarr: "/logos/lidarr.svg",
  linkding: "/logos/linkding.svg",
  linkwarden: "/logos/linkwarden.png",
  memos: "/logos/memos.svg",
  metube: "/logos/metube.png",
  miniflux: "/logos/miniflux.svg",
  navidrome: "/logos/navidrome.png",
  nocodb: "/logos/nocodb.png",
  openldap: "/logos/openldap.png",
  photoprism: "/logos/photoprism.svg",
  pinchflat: "/logos/pinchflat.png",
  prowlarr: "/logos/prowlarr.svg",
  readarr: "/logos/readarr.svg",
  shiori: "/logos/shiori.png",
  shlink: "/logos/shlink.svg",
  silverbullet: "/logos/silverbullet.svg",
  stirlingpdf: "/logos/stirlingpdf.svg",
  typesense: "/logos/typesense.png",
  wallos: "/logos/wallos.png",
  wekan: "/logos/wekan.svg",
  whisparr: "/logos/whisparr.svg",
  whoogle: "/logos/whoogle.png",
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
  const file = files[slug];
  if (!icon && file) {
    // Colour logos keep their own colours, on a light tile in both themes.
    return (
      <span
        className="inline-flex shrink-0 items-center justify-center rounded-md border border-line bg-white"
        style={{ width: size, height: size }}
        aria-hidden
      >
        <img src={file} alt="" width={Math.round(size * 0.72)} height={Math.round(size * 0.72)} className="object-contain" loading="lazy" decoding="async" />
      </span>
    );
  }
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
