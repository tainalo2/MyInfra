import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { Box, DatabaseBackup, Globe, HardDrive, LayoutGrid, Network, Server as ServerIcon, Shield, type LucideIcon } from "lucide-react";
import type { AppRuntime, Application, Package, TemplateApp, TemplateBody, TemplateServer } from "./api";
import { text, type MsgKey } from "./i18n";
import { focus } from "./ui";

// A plan shows what makes a package or an application and what talks to
// what: three bands (entry, services, storage) and six kinds of links.

export type Band = "entry" | "apps" | "services" | "machines" | "storage";
export type LinkKind = "proxy" | "direct" | "internal" | "disk" | "redundancy" | "backup" | "hosts";

export type PlanNode = {
  key: string;
  band: Band;
  kind: "internet" | "proxy" | "service" | "disk" | "backup" | "app" | "server" | "cluster";
  label: string;
  detail?: string;
  // status is a word of statusLabel: running, failed, starting…
  status?: "ok" | "warn" | "bad";
  // draft is an element not saved yet: dashed in its band.
  draft?: boolean;
};

export type PlanLink = { from: string; to: string; kind: LinkKind; label?: string };

export type PlanModel = { nodes: PlanNode[]; links: PlanLink[] };

const bands: { key: Band; label: MsgKey }[] = [
  { key: "entry", label: "plan.band.entry" },
  { key: "apps", label: "plan.band.apps" },
  { key: "services", label: "plan.band.services" },
  { key: "machines", label: "plan.band.machines" },
  { key: "storage", label: "plan.band.storage" },
];

const icons: Record<PlanNode["kind"], LucideIcon> = { internet: Globe, proxy: Shield, service: Box, disk: HardDrive, backup: DatabaseBackup, app: LayoutGrid, server: ServerIcon, cluster: Network };

// The six strokes never change: a reader learns them once.
const strokes: Record<LinkKind, { color: string; width: number; dash?: string; cap?: "round" }> = {
  proxy: { color: "var(--color-ink)", width: 2 },
  direct: { color: "var(--color-warn)", width: 2 },
  internal: { color: "var(--color-muted)", width: 1.5, dash: "6 4" },
  disk: { color: "var(--color-muted)", width: 2.5, dash: "2 3" },
  redundancy: { color: "var(--color-ink)", width: 1.5, dash: "10 3 2 3" },
  backup: { color: "var(--color-faint)", width: 2, dash: "1 5", cap: "round" },
  hosts: { color: "var(--color-faint)", width: 1 },
};

export const linkLabels: Record<LinkKind, MsgKey> = {
  proxy: "plan.link.proxy",
  direct: "plan.link.direct",
  internal: "plan.link.internal",
  disk: "plan.link.disk",
  redundancy: "plan.link.redundancy",
  backup: "plan.link.backup",
  hosts: "plan.link.hosts",
};

type Rect = { x: number; y: number; w: number; h: number };

// related tells the elements a selection reaches by one link.
function related(model: PlanModel, key: string | null) {
  const out = new Set<string>();
  if (!key) return out;
  out.add(key);
  for (const link of model.links) {
    if (link.from === key) out.add(link.to);
    if (link.to === key) out.add(link.from);
  }
  return out;
}

export function Plan({
  model,
  selected,
  onSelect,
  label,
  compact = false,
  toolbar,
}: {
  model: PlanModel;
  selected: string | null;
  onSelect: (key: string | null) => void;
  label: string;
  compact?: boolean;
  toolbar?: ReactNode;
}) {
  const frame = useRef<HTMLDivElement>(null);
  const refs = useRef(new Map<string, HTMLButtonElement>());
  const [boxes, setBoxes] = useState<Map<string, Rect>>(new Map());
  const [size, setSize] = useState({ w: 0, h: 0 });
  const near = related(model, selected);
  const shape = model.nodes.map((node) => node.key).join("|") + (compact ? "c" : "");

  // The links follow the boxes: they are measured after each layout.
  useLayoutEffect(() => {
    const node = frame.current;
    if (!node) return;
    const measure = () => {
      const base = node.getBoundingClientRect();
      const next = new Map<string, Rect>();
      for (const [key, element] of refs.current) {
        const rect = element.getBoundingClientRect();
        next.set(key, { x: rect.left - base.left, y: rect.top - base.top, w: rect.width, h: rect.height });
      }
      setBoxes(next);
      setSize({ w: base.width, h: base.height });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [shape]);

  const used = new Set(model.links.map((link) => link.kind));

  return (
    <section aria-label={label} className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-muted" aria-label={text("plan.legend")}>
          {(Object.keys(strokes) as LinkKind[])
            .filter((kind) => used.has(kind))
            .map((kind) => (
              <li key={kind} className="inline-flex items-center gap-1.5">
                <svg width="22" height="6" aria-hidden>
                  <line x1="1" y1="3" x2="21" y2="3" stroke={strokes[kind].color} strokeWidth={strokes[kind].width} strokeDasharray={strokes[kind].dash} strokeLinecap={strokes[kind].cap} />
                </svg>
                {text(linkLabels[kind])}
              </li>
            ))}
        </ul>
        {toolbar}
      </div>
      <div
        ref={frame}
        role="group"
        aria-label={label}
        className="relative flex flex-col gap-2 rounded-md border border-line bg-surface p-2"
        onClick={(event) => {
          if (event.target === event.currentTarget || (event.target as HTMLElement).dataset.band) onSelect(null);
        }}
      >
        <svg className="pointer-events-none absolute inset-0" width={size.w} height={size.h} aria-hidden>
          {model.links.map((link, index) => {
            const a = boxes.get(link.from);
            const b = boxes.get(link.to);
            if (!a || !b) return null;
            const style = strokes[link.kind];
            const lit = selected !== null && (link.from === selected || link.to === selected);
            const dim = selected !== null && !lit;
            const path = curve(a, b);
            return (
              <g key={`${link.from}-${link.to}-${link.kind}-${index}`} opacity={dim ? 0.25 : 1}>
                <path
                  d={path}
                  fill="none"
                  stroke={style.color}
                  strokeWidth={lit ? style.width + 1 : style.width}
                  strokeDasharray={lit ? style.dash ?? "8 4" : style.dash}
                  strokeLinecap={style.cap}
                  vectorEffect="non-scaling-stroke"
                  className={lit ? "plan-flow" : undefined}
                />
                {link.label && !compact ? <LinkLabel a={a} b={b} label={link.label} warn={link.kind === "direct"} /> : null}
              </g>
            );
          })}
        </svg>
        {bands.map((band) => {
          const nodes = model.nodes.filter((node) => node.band === band.key);
          if (nodes.length === 0) return null;
          return (
            <div key={band.key} data-band={band.key} className={`relative flex flex-col gap-2 rounded-md px-3 ${compact ? "py-2" : "pb-4 pt-2"} ${band.key === "storage" || band.key === "machines" ? "bg-canvas" : "bg-status/40"}`}>
              {compact ? null : (
                <span data-band={band.key} className="text-[12px] text-faint">
                  {text(band.label)}
                </span>
              )}
              <div data-band={band.key} className={`flex flex-wrap justify-center ${compact ? "gap-3" : "gap-x-10 gap-y-4"}`}>
                {nodes.map((node) => {
                  const Glyph = icons[node.kind];
                  const on = node.key === selected;
                  const dim = selected !== null && !near.has(node.key);
                  return (
                    <button
                      key={node.key}
                      ref={(element) => {
                        if (element) refs.current.set(node.key, element);
                        else refs.current.delete(node.key);
                      }}
                      type="button"
                      aria-current={on ? "true" : undefined}
                      aria-label={[text(`plan.kind.${node.kind}` as MsgKey), node.label, node.detail].filter(Boolean).join(" · ")}
                      onClick={(event) => {
                        event.stopPropagation();
                        onSelect(on ? null : node.key);
                      }}
                      className={`relative z-[1] flex min-w-0 max-w-[220px] items-center gap-2 bg-surface text-left transition-opacity ${node.kind === "internet" ? "rounded-full" : "rounded-md"} ${
                        on ? "border-2 border-ink" : node.draft ? "border border-dashed border-faint" : node.kind === "backup" ? "border border-dashed border-[#d6d3d1]" : "border border-[#d6d3d1] dark:border-line"
                      } ${node.band === "storage" || node.band === "machines" ? "bg-canvas" : ""} ${compact ? "px-2.5 py-1" : on ? "px-[11px] py-[7px]" : "px-3 py-2"} ${dim ? "opacity-40" : ""} ${focus}`}
                    >
                      <Glyph size={16} strokeWidth={1.5} aria-hidden className="shrink-0 text-muted" />
                      <span className="flex min-w-0 flex-col">
                        <span className="flex items-center gap-1.5 truncate">
                          {node.status ? <span aria-hidden className={`size-2 shrink-0 rounded-full ${node.status === "ok" ? "bg-ok" : node.status === "warn" ? "bg-warn" : "bg-bad"}`} /> : null}
                          <span className="truncate">{node.label}</span>
                        </span>
                        {node.detail && !compact ? <span className="truncate text-[12px] text-muted">{node.detail}</span> : null}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}

// curve joins two boxes: from the bottom of the upper one to the top of the
// lower one, or side to side within a band.
function curve(a: Rect, b: Rect) {
  const ax = a.x + a.w / 2;
  const bx = b.x + b.w / 2;
  if (Math.abs(a.y - b.y) < 8) {
    const left = a.x < b.x ? a : b;
    const right = a.x < b.x ? b : a;
    const y1 = left.y + left.h / 2;
    const y2 = right.y + right.h / 2;
    const x1 = left.x + left.w;
    const x2 = right.x;
    if (x2 > x1) return `M ${x1} ${y1} C ${x1 + 20} ${y1}, ${x2 - 20} ${y2}, ${x2} ${y2}`;
    const y = Math.max(left.y + left.h, right.y + right.h);
    return `M ${ax} ${a.y + a.h} C ${ax} ${y + 24}, ${bx} ${y + 24}, ${bx} ${b.y + b.h}`;
  }
  const up = a.y < b.y ? a : b;
  const down = a.y < b.y ? b : a;
  const ux = up.x + up.w / 2;
  const dx = down.x + down.w / 2;
  const y1 = up.y + up.h;
  const y2 = down.y;
  const mid = (y1 + y2) / 2;
  return `M ${ux} ${y1} C ${ux} ${mid}, ${dx} ${mid}, ${dx} ${y2}`;
}

function LinkLabel({ a, b, label, warn }: { a: Rect; b: Rect; label: string; warn: boolean }) {
  const row = Math.abs(a.y - b.y) < 8;
  const x = row ? (Math.min(a.x + a.w, b.x + b.w) + Math.max(a.x, b.x)) / 2 : (a.x + a.w / 2 + b.x + b.w / 2) / 2;
  const y = row ? a.y - 4 : (Math.min(a.y + a.h, b.y + b.h) + Math.max(a.y, b.y)) / 2;
  const width = Math.min(200, label.length * 6.4 + 12);
  return (
    <g>
      <rect x={x - width / 2} y={y - 9} width={width} height={18} rx={4} fill="var(--color-surface)" />
      <text x={x} y={y + 4} textAnchor="middle" fontSize="11" fill={warn ? "var(--color-warn)" : "var(--color-muted)"} fontFamily="var(--font-mono)">
        {label.length > 30 ? `${label.slice(0, 29)}…` : label}
      </text>
    </g>
  );
}

// packagePlan draws a package from its definition: what is published, what
// is opened directly, who depends on whom and which disks are mounted.
export function packagePlan(pkg: Package): PlanModel {
  const nodes: PlanNode[] = [];
  const links: PlanLink[] = [];
  const services = pkg.services ?? [];
  const published = services.filter((svc) => svc.public);
  const exposed = services.filter((svc) => (svc.ports ?? []).some((port) => port.expose));
  if (published.length > 0 || exposed.length > 0) nodes.push({ key: "internet", band: "entry", kind: "internet", label: text("plan.internet") });
  if (published.length > 0) {
    nodes.push({ key: "proxy", band: "entry", kind: "proxy", label: text("plan.proxy"), detail: text("plan.proxyDetail") });
    links.push({ from: "internet", to: "proxy", kind: "proxy", label: "HTTPS" });
  }
  const disks = new Map<string, number>();
  for (const svc of services) {
    const copies = svc.replicas && svc.replicas > 1 ? text("apps.copyMany", { count: svc.replicas }) : "";
    nodes.push({ key: `svc:${svc.name}`, band: "services", kind: "service", label: svc.name, detail: [shortImage(svc.image), copies].filter(Boolean).join(" · ") });
    if (svc.public) links.push({ from: "proxy", to: `svc:${svc.name}`, kind: "proxy", label: svc.port ? `:${svc.port}` : undefined });
    for (const port of svc.ports ?? []) {
      if (port.expose) links.push({ from: "internet", to: `svc:${svc.name}`, kind: "direct", label: `${port.expose}/${port.protocol ?? "tcp"}` });
    }
    for (const dep of svc.dependsOn ?? []) {
      if (services.some((other) => other.name === dep)) links.push({ from: `svc:${svc.name}`, to: `svc:${dep}`, kind: "internal" });
    }
    for (const volume of svc.volumes ?? []) {
      const name = volume.disk || `${svc.name}${volume.path.replace(/\//g, "-")}`;
      if (!disks.has(name)) disks.set(name, volume.sizeGi ?? pkg.disks?.find((disk) => disk.name === name)?.sizeGi ?? 0);
      links.push({ from: `svc:${svc.name}`, to: `disk:${name}`, kind: "disk", label: volume.path });
    }
  }
  for (const [name, sizeGi] of disks) nodes.push({ key: `disk:${name}`, band: "storage", kind: "disk", label: name, detail: sizeGi ? `${sizeGi} Gio` : undefined });
  return { nodes, links };
}

// appPlan draws a running application: one box per service with its copies
// and servers, its addresses and its disks.
export function appPlan(app: Application, runtime: AppRuntime | null): PlanModel {
  const nodes: PlanNode[] = [];
  const links: PlanLink[] = [];
  const services = app.services ?? [];
  const published = services.filter((svc) => svc.published);
  const exposed = services.filter((svc) => (svc.exposed ?? []).length > 0);
  const pods = runtime?.pods ?? [];
  const owner = (pod: string) => serviceOfPod(services, pod);
  if (published.length > 0 || exposed.length > 0) nodes.push({ key: "internet", band: "entry", kind: "internet", label: text("plan.internet") });
  if (published.length > 0) {
    nodes.push({ key: "proxy", band: "entry", kind: "proxy", label: text("plan.proxy"), detail: text("plan.proxyDetail") });
    links.push({ from: "internet", to: "proxy", kind: "proxy", label: "HTTPS" });
  }
  for (const svc of services) {
    const mine = pods.filter((pod) => owner(pod.name) === svc.name);
    const servers = [...new Set(mine.map((pod) => pod.server).filter(Boolean))];
    const copies = svc.replicas || 1;
    const bad = mine.some((pod) => pod.phase === "Failed" || /CrashLoop|Error|BackOff/.test(pod.reason));
    const ready = mine.length > 0 && mine.every((pod) => pod.ready);
    const status = app.paused ? undefined : bad ? "bad" : ready ? "ok" : mine.length > 0 ? "warn" : undefined;
    const detail = [copies > 1 ? text("apps.copyMany", { count: copies }) : text("apps.copyOne", { count: 1 }), servers.join(", ")].filter(Boolean).join(" · ");
    nodes.push({ key: `svc:${svc.name}`, band: "services", kind: "service", label: svc.name, detail, status });
    if (svc.published) {
      const host = app.domains?.find((domain) => (domain.service || "") === (published[0]?.name === svc.name ? "" : svc.name) && domain.kind === "custom")?.hostname;
      links.push({ from: "proxy", to: `svc:${svc.name}`, kind: "proxy", label: host });
    }
    for (const port of svc.exposed ?? []) links.push({ from: "internet", to: `svc:${svc.name}`, kind: "direct", label: port });
    if (svc.disk) {
      nodes.push({ key: `disk:${svc.name}`, band: "storage", kind: "disk", label: text("plan.diskOf", { name: svc.name }), detail: copies > 1 ? text("plan.replicated") : undefined });
      links.push({ from: `svc:${svc.name}`, to: `disk:${svc.name}`, kind: "disk" });
    }
  }
  // Services that are not published are reached inside the application.
  for (const inner of services.filter((svc) => !svc.published && !(svc.exposed ?? []).length)) {
    for (const front of published) links.push({ from: `svc:${front.name}`, to: `svc:${inner.name}`, kind: "internal" });
  }
  if (app.failover) {
    nodes.push({ key: "backup", band: "storage", kind: "backup", label: text("plan.backup"), detail: text("plan.backupDetail") });
    for (const svc of services.filter((item) => item.disk)) links.push({ from: `disk:${svc.name}`, to: "backup", kind: "backup" });
  }
  return { nodes, links };
}

// templatePlan draws a template before it is deployed: its applications,
// the cluster they run on and the servers that make it. An element still
// missing a setting is marked.
export function templatePlan(
  body: TemplateBody,
  options: { cluster: string; serverReady: (server: TemplateServer) => boolean; appReady: (app: TemplateApp) => boolean },
): PlanModel {
  const nodes: PlanNode[] = [];
  const links: PlanLink[] = [];
  const apps = body.applications ?? [];
  const servers = body.servers ?? [];
  if (apps.length > 0) {
    nodes.push({ key: "internet", band: "entry", kind: "internet", label: text("plan.internet") });
    nodes.push({ key: "proxy", band: "entry", kind: "proxy", label: text("plan.proxy"), detail: text("plan.proxyDetail") });
    links.push({ from: "internet", to: "proxy", kind: "proxy", label: "HTTPS" });
  }
  if (options.cluster || servers.length > 0) {
    nodes.push({ key: "cluster", band: "machines", kind: "cluster", label: options.cluster || text("plan.clusterToChoose"), status: options.cluster ? undefined : "warn" });
  }
  apps.forEach((app, index) => {
    const key = `app:${index}`;
    const copies = (app.replicas ?? 1) > 1 ? text("apps.copyMany", { count: app.replicas ?? 1 }) : "";
    const what = app.package ? app.package.name : shortImage(app.source || "");
    nodes.push({ key, band: "apps", kind: "app", label: app.name || "…", detail: [what, copies].filter(Boolean).join(" · "), status: options.appReady(app) ? undefined : "warn" });
    const name = (app.names ?? []).find((item) => item.subdomain.trim());
    links.push({ from: "proxy", to: key, kind: "proxy", label: name ? `${name.subdomain}.${name.zone}` : undefined });
    if (options.cluster || servers.length > 0) links.push({ from: key, to: "cluster", kind: "hosts" });
  });
  const marked = servers.some((server) => server.controlPlane);
  servers.forEach((server, index) => {
    const key = `server:${index}`;
    const pilot = server.controlPlane || (!marked && index === 0 && Boolean(body.clusterName));
    nodes.push({
      key,
      band: "machines",
      kind: "server",
      label: server.name || "…",
      detail: [pilot ? text("server.pilot") : "", server.location ?? ""].filter(Boolean).join(" · "),
      status: options.serverReady(server) ? undefined : "warn",
    });
    links.push({ from: "cluster", to: key, kind: "hosts" });
  });
  return { nodes, links };
}

// serviceOfPod finds the service a pod runs: the one whose name the pod's
// starts with, longest first.
export function serviceOfPod(services: { name: string }[], pod: string) {
  const names = services.map((svc) => svc.name).sort((a, b) => b.length - a.length);
  return names.find((name) => pod === name || pod.startsWith(`${name}-`) || pod.includes(`-${name}-`)) ?? (services.length === 1 ? services[0].name : "");
}

function shortImage(image: string) {
  const bare = image.replace(/@sha256:[a-f0-9]+$/, "");
  const slash = bare.lastIndexOf("/");
  return slash >= 0 ? bare.slice(slash + 1) : bare;
}


// PlanPanel holds a plan with what goes around it: the whole object, the
// table of every link, and the head of the chosen element. It stays in sight
// while the page scrolls, and shrinks to names once it has scrolled away.
export function PlanPanel({
  model,
  label,
  whole,
  selected,
  onSelect,
  children,
  summary,
}: {
  model: PlanModel;
  label: string;
  whole: string;
  selected: string | null;
  onSelect: (key: string | null) => void;
  children?: ReactNode;
  // summary follows the word Plan: what the plan holds.
  summary?: string;
}) {
  const [flows, setFlows] = useState(false);
  const [compact, setCompact] = useState(false);
  const box = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const scroller = box.current?.closest("main");
    if (!scroller) return;
    const onScroll = () => {
      const top = scroller.scrollTop;
      setCompact((current) => (current ? top > 40 : top > 200));
    };
    scroller.addEventListener("scroll", onScroll, { passive: true });
    return () => scroller.removeEventListener("scroll", onScroll);
  }, []);

  const name = (key: string) => model.nodes.find((node) => node.key === key)?.label ?? key;
  const chosen = model.nodes.find((node) => node.key === selected) ?? null;
  return (
    <>
    <PhoneList model={model} selected={selected} onSelect={onSelect}>
      {children}
    </PhoneList>
    <div className="hidden md:block">
      <div ref={box} className="sticky top-0 z-10 -mx-2 flex flex-col gap-3 bg-canvas px-2 pb-3 pt-2">
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <h2 className="flex items-baseline gap-2">
            {text("plan.title")}
            <span className="text-[13px] text-muted">
              {summary ?? `${model.nodes.filter((node) => node.kind === "service").length} ${text("plan.band.services").toLowerCase()}`}
            </span>
          </h2>
          <div className="flex flex-wrap items-center gap-2">
            <button type="button" aria-pressed={selected === null} onClick={() => onSelect(null)} className={`rounded-md border px-3 py-1 text-[13px] ${selected === null ? "border-ink text-ink" : "border-line text-muted"} ${focus}`}>
              {whole}
            </button>
            <button type="button" aria-expanded={flows} onClick={() => setFlows((value) => !value)} className={`rounded-md border border-line px-3 py-1 text-[13px] text-muted ${focus}`}>
              {text("plan.flows", { count: model.links.length })}
            </button>
          </div>
        </div>
        <Plan model={model} selected={selected} onSelect={onSelect} label={label} compact={compact} />
        {chosen ? (
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-line pb-2">
            <span className="text-[13px] text-muted">{text(`plan.kind.${chosen.kind}` as MsgKey)}</span>
            <span className="text-[16px]">{chosen.label}</span>
            {chosen.detail ? <span className="text-[13px] text-muted">{chosen.detail}</span> : null}
          </div>
        ) : null}
      </div>
      {flows ? (
        <section aria-label={text("plan.flows", { count: model.links.length })} className="mb-6 mt-2 overflow-hidden rounded-md border border-line bg-surface">
          <div className="grid grid-cols-[1fr_1fr_1fr_1fr] gap-4 border-b border-line px-4 py-2 text-[12px] text-muted">
            <span>{text("plan.col.from")}</span>
            <span>{text("plan.col.to")}</span>
            <span>{text("plan.col.type")}</span>
            <span>{text("plan.col.detail")}</span>
          </div>
          <ul>
            {model.links.map((link, index) => (
              <li key={`${link.from}-${link.to}-${index}`}>
                <button
                  type="button"
                  onClick={() => onSelect(link.to)}
                  className={`grid w-full grid-cols-[1fr_1fr_1fr_1fr] gap-4 border-t border-line px-4 py-2 text-left text-[13px] first:border-t-0 ${focus}`}
                >
                  <span className="truncate">{name(link.from)}</span>
                  <span className="truncate">{name(link.to)}</span>
                  <span className={link.kind === "direct" ? "text-warn" : "text-muted"}>{text(linkLabels[link.kind])}</span>
                  <span className="truncate font-mono text-muted">{link.label ?? ""}</span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      {children}
    </div>
    </>
  );
}

// PhoneList shows the elements of a plan on a phone: a list per band, the
// chosen element's details right under its row.
function PhoneList({ model, selected, onSelect, children }: { model: PlanModel; selected: string | null; onSelect: (key: string | null) => void; children?: ReactNode }) {
  return (
    <div className="flex flex-col gap-6 md:hidden">
      {bands.map((band) => {
        const nodes = model.nodes.filter((node) => node.band === band.key);
        if (nodes.length === 0) return null;
        return (
          <section key={band.key}>
            <h2 className="mb-2 px-1 text-[13px] text-muted">{text(band.label)}</h2>
            <div className="row-group overflow-hidden rounded-md border border-line bg-surface">
              {nodes.map((node) => {
                const Glyph = icons[node.kind];
                const on = node.key === selected;
                const links = model.links.filter((link) => link.from === node.key || link.to === node.key);
                const direct = node.kind === "service" && links.some((link) => link.kind === "direct");
                return (
                  <div key={node.key} className="row-tiled">
                    <button
                      type="button"
                      aria-expanded={on}
                      onClick={() => onSelect(on ? null : node.key)}
                      className={`row flex min-h-14 w-full items-center gap-3 px-4 py-2 text-left ${focus}`}
                    >
                      <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-md bg-status text-muted">
                        <Glyph size={20} strokeWidth={1.75} />
                      </span>
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate">{node.label}</span>
                        {node.detail ? <span className="truncate text-[13px] text-muted">{node.detail}</span> : null}
                      </span>
                      {direct ? <span className="text-[12px] text-warn">{text("plan.directShort")}</span> : null}
                      {node.status ? <span aria-hidden className={`size-2 shrink-0 rounded-full ${node.status === "ok" ? "bg-ok" : node.status === "warn" ? "bg-warn" : "bg-bad"}`} /> : null}
                    </button>
                    {on ? <div className="px-4 pb-3 [&>*]:mb-0">{children}</div> : null}
                  </div>
                );
              })}
            </div>
          </section>
        );
      })}
    </div>
  );
}
