import { useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ArrowDown, ArrowRight, Box, DatabaseBackup, Globe, HardDrive, LayoutGrid, Network, Plus, Server as ServerIcon, Shield, type LucideIcon } from "lucide-react";
import type { AppRuntime, Application, Package, TemplateApp, TemplateBody, TemplateServer } from "./api";
import { text, type MsgKey } from "./i18n";
import { useWide } from "./kit";
import { IconButton, focus } from "./ui";

// A plan shows what makes a package or an application and what talks to
// what: bands from the entry to the storage, and links of a few fixed kinds.

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
  // parent is the element that holds this one, such as the cluster that
  // gathers its servers: it is drawn inside it.
  parent?: string;
};

export type PlanLink = { from: string; to: string; kind: LinkKind; label?: string };

export type PlanModel = { nodes: PlanNode[]; links: PlanLink[] };

// PlanAdds offers to add an element to a band, shown even while it is empty.
export type PlanAdds = Partial<Record<Band, { label: string; onClick: () => void }>>;

// Flow is the direction of a plan: its bands one under the other (rows) or
// side by side (columns).
export type Flow = "auto" | "rows" | "columns";

const bands: { key: Band; label: MsgKey }[] = [
  { key: "entry", label: "plan.band.entry" },
  { key: "apps", label: "plan.band.apps" },
  { key: "services", label: "plan.band.services" },
  { key: "machines", label: "plan.band.machines" },
  { key: "storage", label: "plan.band.storage" },
];

const icons: Record<PlanNode["kind"], LucideIcon> = { internet: Globe, proxy: Shield, service: Box, disk: HardDrive, backup: DatabaseBackup, app: LayoutGrid, server: ServerIcon, cluster: Network };

// The strokes never change: a reader learns them once.
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
  // A container and what it holds go together.
  const node = model.nodes.find((item) => item.key === key);
  if (node?.parent) out.add(node.parent);
  for (const item of model.nodes) if (item.parent === key) out.add(item.key);
  return out;
}

export function Plan({
  model,
  selected,
  onSelect,
  label,
  compact = false,
  header,
  flow = "auto",
  adds,
}: {
  model: PlanModel;
  selected: string | null;
  onSelect: (key: string | null) => void;
  label: string;
  compact?: boolean;
  // header heads the frame: the name of the plan and its buttons. It is told
  // the direction shown, since auto settles it here.
  header?: (columns: boolean) => ReactNode;
  // flow sets the direction of the bands; auto lets leftToRight choose.
  flow?: Flow;
  adds?: PlanAdds;
}) {
  const frame = useRef<HTMLDivElement>(null);
  const refs = useRef(new Map<string, HTMLElement>());
  const [boxes, setBoxes] = useState<Map<string, Rect>>(new Map());
  const [size, setSize] = useState({ w: 0, h: 0 });
  const near = related(model, selected);
  const [direction, setDirection] = useState("rows");

  // Folded, a band keeps one element in front, so that the plan stays one
  // line high: the chosen one, or else one it reaches by a link. The others
  // are stacked behind it, and their links start from the stack.
  const stacks = new Map<string, PlanNode[]>();
  const shownAs = new Map<string, string>();
  if (compact) {
    const fold = (nodes: PlanNode[]) => {
      if (nodes.length < 2) return;
      const front = nodes.find((node) => node.key === selected) ?? nodes.find((node) => near.has(node.key)) ?? nodes[0];
      const behind = nodes.filter((node) => node !== front);
      const top = front.key;
      stacks.set(top, behind);
      for (const node of behind) {
        shownAs.set(node.key, top);
        for (const member of model.nodes) if (member.parent === node.key) shownAs.set(member.key, top);
      }
    };
    for (const band of bands) fold(model.nodes.filter((node) => node.band === band.key && !node.parent));
    for (const node of model.nodes) if (!shownAs.has(node.key)) fold(model.nodes.filter((item) => item.parent === node.key));
  }
  const front = (node: PlanNode) => !shownAs.has(node.key);
  const at = (key: string) => shownAs.get(key) ?? key;

  // What a box says sets its size: the links follow it as they follow its key.
  const shape = model.nodes.filter(front).map((node) => [node.key, node.label, node.detail, node.status].join(":")).join("|") + (compact ? "c" : "") + direction;

  // The last layout, to move from it to the next one.
  const before = useRef<{ boxes: Map<string, Rect>; height: number; shownAs: Map<string, string> } | null>(null);
  const shownNow = useRef(shownAs);
  shownNow.current = shownAs;

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
      return { next, height: base.height };
    };
    const { next, height } = measure();
    const last = before.current;
    before.current = { boxes: next, height, shownAs: shownNow.current };
    const animations = last ? glide(node, refs.current, last, next, height, model) : [];
    // The links are measured as the boxes move, then once they have arrived.
    if (animations.length > 0) void Promise.allSettled(animations.map((item) => item.finished)).then(() => frame.current && measure());
    const observer = new ResizeObserver(() => {
      const { next: moved, height: now } = measure();
      if (before.current) before.current = { ...before.current, boxes: moved, height: now };
    });
    observer.observe(node);
    return () => {
      observer.disconnect();
      for (const item of animations) item.cancel();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shape]);

  const shownBands = bands.filter((band) => adds?.[band.key] || model.nodes.some((node) => node.band === band.key));
  const shownRef = useRef(false);
  const columns = useMemo(
    // Folded, the bands always go side by side: it is the lowest a plan gets.
    () => shownBands.length > 1 && (compact || (flow === "auto" ? leftToRight(model, size.w, shownRef.current, shownBands) : flow === "columns")),
    [model, size.w, compact, flow, shownBands.length],
  );
  // The direction to keep is the open plan's, to find it again once unfolded.
  if (!compact) shownRef.current = columns;
  // A change of direction moves every box: the links are measured again.
  useLayoutEffect(() => setDirection(columns ? "columns" : "rows"), [columns]);

  // box draws one element of the plan, and the stack behind it when folded.
  const box = (node: PlanNode) => {
    const Glyph = icons[node.kind];
    const on = node.key === selected;
    const dim = selected !== null && !near.has(node.key);
    const behind = stacks.get(node.key);
    const width = columns ? "w-full max-w-[280px] self-center" : "max-w-[220px]";
    const button = (
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
        className={`relative z-[2] flex min-w-0 items-center gap-2 bg-surface text-left transition-opacity ${behind ? "w-full" : width} ${node.kind === "internet" ? "rounded-full" : "rounded-md"} ${
          on ? "border-2 border-ink" : node.draft ? "border border-dashed border-faint" : node.kind === "backup" ? "border border-dashed border-[#d6d3d1]" : "border border-[#d6d3d1] dark:border-line"
        } ${node.band === "storage" || node.band === "machines" ? "bg-canvas" : ""} ${compact ? "px-2 py-0.5" : on ? "px-[9px] py-[5px]" : "px-2.5 py-1.5"} ${dim && !behind ? "opacity-40" : ""} ${focus}`}
      >
        <Glyph size={16} strokeWidth={1.5} aria-hidden className="shrink-0 text-muted" />
        <span className="flex min-w-0 flex-col">
          <span className="flex items-center gap-1.5 truncate">
            {node.status ? <span aria-hidden className={`size-2 shrink-0 rounded-full ${node.status === "ok" ? "bg-ok" : node.status === "warn" ? "bg-warn" : "bg-bad"}`} /> : null}
            <span className="truncate">{node.label}</span>
          </span>
          {node.detail && !compact ? <span className="truncate text-[12px] text-muted">{node.detail}</span> : null}
        </span>
        {behind ? (
          <span className="ml-auto shrink-0 rounded-full border border-line px-1.5 text-[11px] text-muted" title={behind.map((item) => item.label).join(", ")}>
            <span aria-hidden>+{behind.length}</span>
            <span className="sr-only">{text("plan.behind", { count: behind.length, names: behind.map((item) => item.label).join(", ") })}</span>
          </span>
        ) : null}
      </button>
    );
    if (!behind) return button;
    // Two edges peek out under the box: what is stacked behind it.
    return (
      <span key={node.key} data-stack className={`relative z-[2] flex ${width} ${dim ? "opacity-40" : ""}`}>
        <span aria-hidden className={`absolute inset-0 translate-x-[6px] translate-y-[6px] border border-line bg-surface ${node.kind === "internet" ? "rounded-full" : "rounded-md"}`} />
        <span aria-hidden className={`absolute inset-0 translate-x-[3px] translate-y-[3px] border border-line bg-surface ${node.kind === "internet" ? "rounded-full" : "rounded-md"}`} />
        {button}
      </span>
    );
  };

  const used = new Set(model.links.map((link) => link.kind));

  const legend = (Object.keys(strokes) as LinkKind[]).filter((kind) => used.has(kind));

  return (
    <section aria-label={label}>
      <div
        ref={frame}
        role="group"
        aria-label={label}
        className={`relative flex flex-col rounded-md border border-line bg-surface ${compact ? "gap-1.5 p-1.5" : "gap-2.5 p-2"}`}
        onClick={(event) => {
          if (event.target === event.currentTarget || (event.target as HTMLElement).dataset.band) onSelect(null);
        }}
      >
        {/* Folded, the plan is only its line of boxes. */}
        {header && !compact ? <div className="relative z-[2] px-1">{header(columns)}</div> : null}
        {/* The links run above the bands' backgrounds and under the boxes. */}
        <svg className="pointer-events-none absolute inset-0 z-[1]" width={size.w} height={size.h} aria-hidden>
          {model.links.map((link, index) => {
            // A link inside one stack has nowhere to go.
            if (at(link.from) === at(link.to)) return null;
            const a = boxes.get(at(link.from));
            const b = boxes.get(at(link.to));
            if (!a || !b) return null;
            const style = strokes[link.kind];
            const lit = selected !== null && (link.from === selected || link.to === selected);
            const dim = selected !== null && !lit;
            const others = [...boxes].filter(([key]) => !key.startsWith("band:") && key !== at(link.from) && key !== at(link.to)).map(([, rect]) => rect);
            const path = curve(a, b, columns, others);
            return (
              <g key={`${link.from}-${link.to}-${link.kind}-${index}`} opacity={dim ? (compact ? 0.35 : 0.25) : 1}>
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
                {link.label && !compact && !columns ? <LinkLabel a={a} b={b} label={link.label} warn={link.kind === "direct"} /> : null}
              </g>
            );
          })}
        </svg>
        <div data-band="all" className={columns ? `flex items-stretch ${compact ? "gap-1.5" : "gap-2.5"}` : `flex flex-col ${compact ? "gap-1.5" : "gap-2.5"}`}>
          {shownBands.map((band) => {
            const nodes = model.nodes.filter((node) => node.band === band.key && !node.parent && front(node));
            const tint = band.key === "storage" || band.key === "machines" ? "bg-line/25" : "bg-status/40";
            // The tint of a band is a layer of its own, so that it can follow
            // the band from one layout to the next without moving its content.
            const layer = (
              <span
                aria-hidden
                data-band={band.key}
                ref={(element) => {
                  if (element) refs.current.set(`band:${band.key}`, element);
                  else refs.current.delete(`band:${band.key}`);
                }}
                className={`absolute inset-0 rounded-md ${tint}`}
              />
            );
            const add = adds?.[band.key];
            const plus = add ? <AddToBand label={add.label} onClick={add.onClick} compact={compact} /> : null;
            const content = nodes.map((node) => {
              const members = model.nodes.filter((item) => item.parent === node.key && front(item));
              if (members.length === 0) return box(node);
              const on = node.key === selected;
              const dim = selected !== null && !near.has(node.key);
              const Glyph = icons[node.kind];
              return (
                // A container: its name heads a frame around what it holds.
                <div
                  key={node.key}
                  ref={(element) => {
                    if (element) refs.current.set(node.key, element);
                    else refs.current.delete(node.key);
                  }}
                  className={`relative z-[2] flex gap-2 rounded-md bg-surface/70 transition-opacity ${compact ? "flex-row items-center" : "flex-col"} ${on ? "border-2 border-ink" : "border border-dashed border-muted"} ${compact ? "p-1.5" : on ? "px-[9px] pb-[9px] pt-[5px]" : "px-2.5 pb-2.5 pt-1.5"} ${dim ? "opacity-40" : ""}`}
                >
                  <button
                    type="button"
                    aria-current={on ? "true" : undefined}
                    aria-label={[text(`plan.kind.${node.kind}` as MsgKey), node.label, node.detail].filter(Boolean).join(" · ")}
                    onClick={(event) => {
                      event.stopPropagation();
                      onSelect(on ? null : node.key);
                    }}
                    className={`-mx-1 flex items-center gap-2 self-start rounded-md px-1 text-left ${focus}`}
                  >
                    <Glyph size={16} strokeWidth={1.5} aria-hidden className="shrink-0 text-muted" />
                    {node.status ? <span aria-hidden className={`size-2 shrink-0 rounded-full ${node.status === "ok" ? "bg-ok" : node.status === "warn" ? "bg-warn" : "bg-bad"}`} /> : null}
                    <span className="truncate">{node.label}</span>
                    {node.detail && !compact ? <span className="truncate text-[12px] text-muted">{node.detail}</span> : null}
                  </button>
                  <div className={`flex ${columns ? "flex-col" : "flex-wrap"} ${compact ? "gap-2" : "gap-3"}`}>{members.map((member) => box(member))}</div>
                </div>
              );
            });
            if (columns) {
              // Left to right: a column per band, its elements one under the other.
              return (
                <div key={band.key} data-band={band.key} className={`relative flex min-h-8 min-w-0 flex-1 flex-col gap-2 rounded-md px-2 ${compact ? "py-1" : "py-2"} ${add && compact ? "pr-8" : ""}`}>
                  {layer}
                  {plus}
                  {compact ? null : (
                    <span data-band={band.key} className="relative text-[11px] text-faint">
                      {text(band.label)}
                    </span>
                  )}
                  <div data-band={band.key} className={`flex flex-1 flex-col justify-center ${compact ? "gap-1.5" : "gap-2.5"}`}>
                    {content}
                  </div>
                </div>
              );
            }
            return (
              <div key={band.key} data-band={band.key} className={`relative flex min-h-8 items-center gap-2 rounded-md px-2 ${compact ? "py-1" : "py-2"} ${add ? "pr-8" : ""}`}>
                  {layer}
                  {plus}
                {compact ? null : (
                  <span data-band={band.key} className="relative w-[72px] shrink-0 self-start pt-0.5 text-[11px] text-faint">
                    {text(band.label)}
                  </span>
                )}
                <div data-band={band.key} className={`flex min-w-0 flex-1 flex-wrap items-center justify-center ${compact ? "gap-2" : "gap-x-8 gap-y-3"}`}>
                  {content}
                </div>
              </div>
            );
          })}
        </div>
        {compact || legend.length === 0 ? null : (
          <ul className="relative z-[2] flex flex-wrap gap-x-4 gap-y-1 px-1 text-[11px] text-muted" aria-label={text("plan.legend")}>
            {legend.map((kind) => (
              <li key={kind} className="inline-flex items-center gap-1.5">
                <svg width="20" height="6" aria-hidden>
                  <line x1="1" y1="3" x2="19" y2="3" stroke={strokes[kind].color} strokeWidth={strokes[kind].width} strokeDasharray={strokes[kind].dash} strokeLinecap={strokes[kind].cap} />
                </svg>
                {text(linkLabels[kind])}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}

// AddToBand is the + in the corner of a band; its words show on hover, at once.
function AddToBand({ label, onClick, compact }: { label: string; onClick: () => void; compact: boolean }) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      className={`group absolute right-1 z-[3] flex size-6 items-center justify-center rounded-md border border-line bg-surface text-muted hover:text-ink ${compact ? "top-1/2 -translate-y-1/2" : "top-1"} ${focus}`}
    >
      <Plus size={14} aria-hidden />
      <span aria-hidden className="pointer-events-none absolute right-0 top-full mt-1 hidden whitespace-nowrap rounded-md border border-line bg-surface px-2 py-0.5 text-[12px] text-ink group-hover:block group-focus-visible:block">
        {label}
      </span>
    </button>
  );
}

// curve joins two boxes: from the bottom of the upper one to the top of the
// lower one, or side to side within a band. Left to right, boxes of two
// columns join side to side, and two boxes of one column by an arc beside it.
// Within a band, a link goes under the boxes between its two ends: running
// behind them, it would read as two links through them.
function curve(a: Rect, b: Rect, columns = false, others: Rect[] = []) {
  const ax = a.x + a.w / 2;
  const bx = b.x + b.w / 2;
  if (columns) {
    const left = a.x <= b.x ? a : b;
    const right = a.x <= b.x ? b : a;
    const y1 = left.y + left.h / 2;
    const y2 = right.y + right.h / 2;
    const x1 = left.x + left.w;
    const x2 = right.x;
    if (x2 > x1 + 4) {
      const bend = Math.max(12, (x2 - x1) / 2);
      return `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`;
    }
    const edge = Math.max(a.x + a.w, b.x + b.w);
    return `M ${a.x + a.w} ${a.y + a.h / 2} C ${edge + 28} ${a.y + a.h / 2}, ${edge + 28} ${b.y + b.h / 2}, ${b.x + b.w} ${b.y + b.h / 2}`;
  }
  if (Math.abs(a.y + a.h / 2 - (b.y + b.h / 2)) < 8) {
    const left = a.x < b.x ? a : b;
    const right = a.x < b.x ? b : a;
    const y1 = left.y + left.h / 2;
    const y2 = right.y + right.h / 2;
    const x1 = left.x + left.w;
    const x2 = right.x;
    const between = others.some((rect) => Math.abs(rect.y + rect.h / 2 - y1) < 8 && rect.x < x2 && rect.x + rect.w > x1);
    if (x2 > x1 && !between) return `M ${x1} ${y1} C ${x1 + 20} ${y1}, ${x2 - 20} ${y2}, ${x2} ${y2}`;
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

// glide moves a plan from its last layout to the new one, in a short time:
// the frame changes height, each box slides from where it was, and a box
// that comes out of a stack starts from it, fading in. Nothing moves when
// the system asks for less motion.
function glide(
  frame: HTMLElement,
  elements: Map<string, HTMLElement>,
  last: { boxes: Map<string, Rect>; height: number; shownAs: Map<string, string> },
  next: Map<string, Rect>,
  height: number,
  model: PlanModel,
) {
  if (typeof frame.animate !== "function" || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return [];
  const timing: KeyframeAnimationOptions = { duration: 180, easing: "cubic-bezier(0.2, 0, 0, 1)" };
  const out: Animation[] = [];
  if (Math.abs(last.height - height) > 1) {
    // What does not fit yet stays inside the frame while it grows.
    frame.style.overflow = "hidden";
    const grow = frame.animate([{ height: `${last.height}px` }, { height: `${height}px` }], timing);
    void grow.finished.catch(() => undefined).finally(() => (frame.style.overflow = ""));
    out.push(grow);
  }
  const centre = (rect: Rect) => ({ x: rect.x + rect.w / 2, y: rect.y + rect.h / 2 });
  // A box held in a container moves with it: only its own move is added.
  const moved = new Map<string, { x: number; y: number }>();
  const parents = new Map(model.nodes.map((node) => [node.key, node.parent]));
  const keys = [...next.keys()].sort((a, b) => Number(Boolean(parents.get(a))) - Number(Boolean(parents.get(b))));
  for (const key of keys) {
    const now = next.get(key)!;
    if (key.startsWith("band:")) {
      // A tint stretches from the band it was to the band it is.
      const was = last.boxes.get(key);
      const layer = elements.get(key);
      if (!was || !layer || !now.w || !now.h) continue;
      const still = Math.abs(was.x - now.x) < 1 && Math.abs(was.y - now.y) < 1 && Math.abs(was.w - now.w) < 1 && Math.abs(was.h - now.h) < 1;
      if (still) continue;
      out.push(
        layer.animate(
          [
            { transformOrigin: "0 0", transform: `translate(${was.x - now.x}px, ${was.y - now.y}px) scale(${was.w / now.w}, ${was.h / now.h})` },
            { transformOrigin: "0 0", transform: "none" },
          ],
          timing,
        ),
      );
      continue;
    }
    const from = last.boxes.get(key) ?? last.boxes.get(last.shownAs.get(key) ?? "");
    const element = elements.get(key);
    if (!element) continue;
    const target = element.closest<HTMLElement>("[data-stack]") ?? element;
    if (!from) {
      out.push(target.animate([{ opacity: 0 }, { opacity: 1 }], timing));
      continue;
    }
    const a = centre(from);
    const b = centre(now);
    const parent = moved.get(parents.get(key) ?? "") ?? { x: 0, y: 0 };
    const dx = a.x - b.x - parent.x;
    const dy = a.y - b.y - parent.y;
    moved.set(key, { x: a.x - b.x, y: a.y - b.y });
    const fresh = !last.boxes.has(key);
    if (Math.abs(dx) < 1 && Math.abs(dy) < 1 && !fresh) continue;
    // A box coming out of a stack shows once it has left it a little, so
    // that the boxes leaving one stack do not mix their words.
    out.push(
      target.animate(
        fresh
          ? [
              { transform: `translate(${dx}px, ${dy}px)`, opacity: 0 },
              { opacity: 0, offset: 0.35 },
              { transform: "none", opacity: 1 },
            ]
          : [{ transform: `translate(${dx}px, ${dy}px)` }, { transform: "none" }],
        timing,
      ),
    );
  }
  return out;
}

// leftToRight chooses the direction of a plan: the one that makes it lower,
// since what follows the plan needs the room. Each direction is laid out on
// paper from the size of every box, guessed from its words so that the guess
// does not depend on the direction shown. Top to bottom wins a tie, and the
// shown direction is kept unless the other saves a tenth of the height, so
// that resizing the window does not make the plan swing.
function leftToRight(model: PlanModel, width: number, current: boolean, shown: typeof bands) {
  if (shown.length < 2 || width <= 0) return false;
  const gap = 12;
  const bandGap = 10;
  const bandPad = 16;
  const frame = 16;
  const head = 30; // the plan's own head and legend lines are the same both ways
  const boxW = (node: PlanNode) => Math.min(220, Math.max(100, Math.max(node.label.length * 7.2, (node.detail ?? "").length * 5.4) + 44));
  const boxH = (node: PlanNode) => (node.detail ? 46 : 36);
  // Each top element of a band, as a block: a box, or a container and what it holds.
  const blocks = (band: Band, columns: boolean) =>
    model.nodes
      .filter((node) => node.band === band && !node.parent)
      .map((node) => {
        const members = model.nodes.filter((item) => item.parent === node.key);
        if (members.length === 0) return { w: boxW(node), h: boxH(node) };
        const top = 28;
        if (columns) return { w: Math.max(...members.map(boxW)) + 20, h: top + members.reduce((sum, item) => sum + boxH(item) + gap, 0) + 8 };
        return { w: members.reduce((sum, item) => sum + boxW(item) + gap, 0) + 20, h: top + Math.max(...members.map(boxH)) + 12 };
      });

  // Top to bottom: each band fills lines across the width beside its label.
  const lineWidth = width - frame - bandPad - 80;
  let rowsH = frame + head;
  for (const band of shown) {
    let lines = 0;
    let lineH = 0;
    let used = Infinity;
    let total = 0;
    for (const block of blocks(band.key, false)) {
      if (used + block.w > lineWidth) {
        total += lineH;
        lines += 1;
        used = 0;
        lineH = 0;
      }
      used += block.w + 32;
      lineH = Math.max(lineH, block.h);
    }
    total += lineH + Math.max(0, lines - 1) * gap;
    rowsH += total + bandPad + bandGap;
  }

  // Left to right: a column per band, which must hold its widest box.
  const columnW = (width - frame - (shown.length - 1) * bandGap) / shown.length - bandPad;
  const columnBlocks = shown.map((band) => blocks(band.key, true));
  if (columnBlocks.some((list) => list.some((block) => block.w > columnW))) return false;
  const columnsH = frame + head + 20 + bandPad + Math.max(...columnBlocks.map((list) => list.reduce((sum, block) => sum + block.h + gap, -gap)));

  return current ? columnsH <= rowsH * 1.1 : columnsH < rowsH * 0.9;
}

// LinkLabel names a link of a plan from top to bottom; side by side, columns
// leave no room for it, and the table of interactions says it.
function LinkLabel({ a, b, label, warn }: { a: Rect; b: Rect; label: string; warn: boolean }) {
  const row = Math.abs(a.y + a.h / 2 - (b.y + b.h / 2)) < 8;
  const x = row ? (Math.min(a.x + a.w, b.x + b.w) + Math.max(a.x, b.x)) / 2 : (a.x + a.w / 2 + b.x + b.w / 2) / 2;
  const y = row ? Math.min(a.y, b.y) - 10 : (Math.min(a.y + a.h, b.y + b.h) + Math.max(a.y, b.y)) / 2;
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
  // A shared disk no service mounts yet waits, dashed, in its band.
  for (const disk of pkg.disks ?? []) {
    if (disk.name && !disks.has(disk.name)) nodes.push({ key: `disk:${disk.name}`, band: "storage", kind: "disk", label: disk.name, detail: disk.sizeGi ? `${disk.sizeGi} Gio` : undefined, draft: true });
  }
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
  // Who talks to whom inside is what the services declare, as in a package.
  for (const svc of services) {
    for (const dep of svc.dependsOn ?? []) {
      if (services.some((other) => other.name === dep)) links.push({ from: `svc:${svc.name}`, to: `svc:${dep}`, kind: "internal" });
    }
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
  // clusterLater says that a cluster left to choose is no gap: it is chosen at deployment.
  options: { cluster: string; clusterLater?: boolean; serverReady: (server: TemplateServer) => boolean; appReady: (app: TemplateApp) => boolean },
): PlanModel {
  const nodes: PlanNode[] = [];
  const links: PlanLink[] = [];
  const apps = body.applications ?? [];
  const servers = body.servers ?? [];
  if (apps.some(isPublic)) {
    nodes.push({ key: "internet", band: "entry", kind: "internet", label: text("plan.internet") });
    nodes.push({ key: "proxy", band: "entry", kind: "proxy", label: text("plan.proxy"), detail: text("plan.proxyDetail") });
    links.push({ from: "internet", to: "proxy", kind: "proxy", label: "HTTPS" });
  }
  if (options.cluster || servers.length > 0) {
    nodes.push({ key: "cluster", band: "machines", kind: "cluster", label: options.cluster || text("plan.clusterToChoose"), status: options.cluster || options.clusterLater ? undefined : "warn" });
  }
  apps.forEach((app, index) => {
    const key = `app:${index}`;
    const copies = (app.replicas ?? 1) > 1 ? text("apps.copyMany", { count: app.replicas ?? 1 }) : "";
    const what = app.package ? app.package.name : shortImage(app.source || "");
    nodes.push({ key, band: "apps", kind: "app", label: app.name || "…", detail: [what, copies].filter(Boolean).join(" · "), status: options.appReady(app) ? undefined : "warn" });
    const name = (app.names ?? []).find((item) => item.subdomain.trim());
    if (isPublic(app)) links.push({ from: "proxy", to: key, kind: "proxy", label: name ? `${name.subdomain}.${name.zone}` : undefined });
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
      parent: options.cluster || servers.length > 0 ? "cluster" : undefined,
    });
  });
  return { nodes, links };
}

// isPublic tells whether a template's application answers on the Internet:
// one of its services is published, and an application of one image is.
function isPublic(app: TemplateApp) {
  if (app.package) return app.package.services.some((svc) => svc.public);
  const services = app.services ?? [];
  return services.length === 0 || services.some((svc) => svc.published);
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


// underPlan keeps a line of the page, such as its tabs, in sight right
// under the plan while the page scrolls; it follows the plan's height.
export const underPlan = "md:sticky md:top-[var(--plan-bottom,0px)] md:z-[9] md:bg-canvas md:pt-1";

// ChosenName says which element of a plan is chosen: its kind, name and detail.
export function ChosenName({ node }: { node: PlanNode }) {
  return (
    <span className="flex min-w-0 items-baseline gap-2">
      <span className="shrink-0 text-[12px] text-muted">{text(`plan.kind.${node.kind}` as MsgKey)}</span>
      <span className="truncate">{node.label}</span>
      {node.detail ? <span className="truncate text-[12px] text-muted">{node.detail}</span> : null}
    </span>
  );
}

// PlanLinks lists what the chosen element is linked to; a row chooses the
// element at its other end.
export function PlanLinks({ model, chosen, onSelect }: { model: PlanModel; chosen: string; onSelect: (key: string) => void }) {
  const links = model.links.filter((link) => link.from === chosen || link.to === chosen);
  if (links.length === 0) return <p className="text-muted">{text("plan.noLinks")}</p>;
  return (
    <ul className="flex flex-col">
      {links.map((link, index) => {
        const other = link.from === chosen ? link.to : link.from;
        return (
          <li key={`${other}-${link.kind}-${index}`} className="border-t border-line first:border-t-0">
            <button type="button" onClick={() => onSelect(other)} className={`flex w-full flex-wrap items-baseline justify-between gap-x-6 gap-y-1 py-3 text-left ${focus}`}>
              <span className="min-w-0 truncate">{model.nodes.find((node) => node.key === other)?.label ?? other}</span>
              <span className="flex min-w-0 flex-wrap items-baseline gap-x-3 text-[13px]">
                {link.label ? <span className="break-all font-mono text-muted">{link.label}</span> : null}
                <span className={link.kind === "direct" ? "text-warn" : "text-muted"}>{text(linkLabels[link.kind])}</span>
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
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
  named = true,
  adds,
}: {
  model: PlanModel;
  label: string;
  whole: string;
  selected: string | null;
  onSelect: (key: string | null) => void;
  children?: ReactNode;
  // summary follows the word Plan: what the plan holds.
  summary?: string;
  // named heads what follows with the chosen element; a page that says it on
  // its own line, with ChosenName, leaves it out.
  named?: boolean;
  adds?: PlanAdds;
}) {
  const [flows, setFlows] = useState(false);
  const [compact, setCompact] = useState(false);
  // The plan chooses its direction until a person turns it with the button.
  const [flow, setFlow] = useState<Flow>("auto");
  // What follows the plan is mounted once, on the side that shows it: hidden
  // by CSS only, a second copy would keep its own state and calls.
  const wide = useWide();
  const box = useRef<HTMLDivElement>(null);
  const mark = useRef<HTMLDivElement>(null);
  const folded = useRef(false);
  folded.current = compact;

  // The plan shrinks once it is stuck under the trail, and opens again when
  // the page comes back to its top. The mark sits above the plan: shrinking
  // moves what is below, never the mark, so the plan does not flicker.
  //
  // What follows the plan rises into the room it gives up. A short page would
  // then lose its scroll and the plan would open again at once: its end gets
  // just what keeps it scrolled past that point, and a long page gets nothing.
  useLayoutEffect(() => {
    const sticky = box.current;
    const above = mark.current;
    const scroller = above?.closest("main");
    if (!sticky || !above || !scroller) return;
    let frame = 0;
    let live = true;
    const padding = parseFloat(getComputedStyle(scroller).paddingBottom) || 0;
    const keep = (room: number) => {
      if (room >= 1) scroller.style.paddingBottom = `${padding + room}px`;
      else scroller.style.removeProperty("padding-bottom");
    };
    // spare is what the page lacks to stay scrolled just past where the plan
    // opens, once it is shorter by shrink. Never where it is now: a fast
    // scroll would keep whatever empty room it reached.
    const spare = (shrink = 0) => {
      const end = scroller.lastElementChild?.getBoundingClientRect().bottom ?? 0;
      const content = end - scroller.getBoundingClientRect().top + scroller.scrollTop + padding - shrink;
      const past = scroller.scrollTop + above.getBoundingClientRect().top - line() + 5;
      return Math.max(0, past + scroller.clientHeight - content);
    };
    const line = () => scroller.getBoundingClientRect().top + (parseFloat(getComputedStyle(sticky).top) || 0);
    // settle gives back the room once the plan has its final height: while
    // it slides, the page looks longer than it will be.
    const settle = () => {
      if (!folded.current) return keep(0);
      const moving = sticky.querySelector('[role="group"]')?.getAnimations() ?? [];
      if (moving.length === 0) return keep(spare());
      void Promise.allSettled(moving.map((item) => item.finished)).then(() => {
        if (live) settle();
      });
    };
    // The end of the page and the line under the plan follow its height,
    // frame by frame while it changes.
    const fit = () => {
      settle();
      // Rounded down, on the exact height: the line slips under the plan, and
      // no sliver of the page shows between them.
      const bottom = (parseFloat(getComputedStyle(sticky).top) || 0) + sticky.getBoundingClientRect().height;
      scroller.style.setProperty("--plan-bottom", `${Math.floor(bottom)}px`);
    };
    const check = () => {
      frame = 0;
      if (sticky.offsetParent === null) return;
      const at = line();
      const top = above.getBoundingClientRect().top;
      const current = folded.current;
      // Going back up, the page gives back what it no longer needs.
      if (current) settle();
      const next = current ? top < at - 4 : top < at - 24;
      if (next === current) return;
      // The room is taken before the plan gives it up: a frame later, the
      // page would already be shorter.
      if (next) keep(spare(sticky.offsetHeight));
      setCompact(next);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(check);
    };
    const observer = new ResizeObserver(fit);
    observer.observe(sticky);
    scroller.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    fit();
    check();
    return () => {
      live = false;
      observer.disconnect();
      scroller.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      scroller.style.removeProperty("--plan-bottom");
      scroller.style.removeProperty("padding-bottom");
      if (frame) cancelAnimationFrame(frame);
    };
  }, []);

  // The plan takes the whole width of the page's area, past the narrower
  // column of the page it sits in: a wide plan is a lower one.
  const [span, setSpan] = useState<{ left: number; width: number } | null>(null);
  useLayoutEffect(() => {
    const sticky = box.current;
    const parent = sticky?.parentElement;
    const scroller = sticky?.closest("main");
    if (!sticky || !parent || !scroller) return;
    const measure = () => {
      if (sticky.offsetParent === null) return setSpan(null);
      const area = scroller.getBoundingClientRect();
      const style = getComputedStyle(scroller);
      const left = area.left + (parseFloat(style.paddingLeft) || 0);
      const width = scroller.clientWidth - (parseFloat(style.paddingLeft) || 0) - (parseFloat(style.paddingRight) || 0);
      const next = { left: Math.round(left - parent.getBoundingClientRect().left), width: Math.round(width) };
      setSpan((current) => (current && current.left === next.left && current.width === next.width ? current : next));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(scroller);
    observer.observe(parent);
    return () => observer.disconnect();
  }, []);

  const name = (key: string) => model.nodes.find((node) => node.key === key)?.label ?? key;
  const chosen = model.nodes.find((node) => node.key === selected) ?? null;
  const header = (columns: boolean) => (
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
      <h2 className="flex items-baseline gap-2 text-[14px]">
        {text("plan.title")}
        <span className="text-[12px] text-muted">
          {summary ?? `${model.nodes.filter((node) => node.kind === "service").length} ${text("plan.band.services").toLowerCase()}`}
        </span>
      </h2>
      <div className="flex flex-wrap items-center gap-1.5">
        <button type="button" aria-pressed={selected === null} onClick={() => onSelect(null)} className={`rounded-md border px-2 py-0.5 text-[12px] ${selected === null ? "border-ink text-ink" : "border-line text-muted"} ${focus}`}>
          {whole}
        </button>
        <button type="button" aria-expanded={flows} onClick={() => setFlows((value) => !value)} className={`rounded-md border border-line px-2 py-0.5 text-[12px] text-muted ${focus}`}>
          {text("plan.flows", { count: model.links.length })}
        </button>
        <button
          type="button"
          onClick={() => setFlow(columns ? "rows" : "columns")}
          className={`inline-flex items-center gap-1 rounded-md border border-line px-2 py-0.5 text-[12px] text-muted ${focus}`}
        >
          {columns ? <ArrowDown size={12} aria-hidden /> : <ArrowRight size={12} aria-hidden />}
          {text(columns ? "plan.rows" : "plan.columns")}
        </button>
      </div>
    </div>
  );
  // The parts are siblings in the page, so that the plan stays stuck while
  // all that follows it scrolls.
  return (
    <>
      <PhoneList model={model} selected={selected} onSelect={onSelect} adds={adds}>
        {wide ? null : children}
      </PhoneList>
      <div ref={mark} aria-hidden className="hidden h-0 md:block" />
      <div
        ref={box}
        className="sticky top-0 z-10 hidden flex-col gap-2 bg-canvas pb-2 pt-1 md:flex"
        style={span ? { marginLeft: span.left, width: span.width } : undefined}
      >
        <Plan model={model} selected={selected} onSelect={onSelect} label={label} compact={compact} header={header} flow={flow} adds={adds} />
        {chosen && named ? (
          <div className="border-b border-line pb-1.5">
            <ChosenName node={chosen} />
          </div>
        ) : null}
      </div>
      {flows ? (
        <section aria-label={text("plan.flows", { count: model.links.length })} className="mb-4 mt-2 hidden overflow-hidden rounded-md border border-line bg-surface md:block">
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
      {children && wide ? <div className="mt-3">{children}</div> : null}
    </>
  );
}

// PhoneList shows the elements of a plan on a phone: a list per band, the
// chosen element's details right under its row.
function PhoneList({ model, selected, onSelect, adds, children }: { model: PlanModel; selected: string | null; onSelect: (key: string | null) => void; adds?: PlanAdds; children?: ReactNode }) {
  return (
    <div className="flex flex-col gap-6 md:hidden">
      {bands.map((band) => {
        const nodes = model.nodes.filter((node) => node.band === band.key);
        const add = adds?.[band.key];
        if (nodes.length === 0 && !add) return null;
        return (
          <section key={band.key}>
            <div className="mb-2 flex items-center justify-between gap-3 px-1">
              <h2 className="text-[13px] text-muted">{text(band.label)}</h2>
              {add ? <IconButton type="button" icon={Plus} label={add.label} onClick={add.onClick} /> : null}
            </div>
            <div className={`row-group overflow-hidden rounded-md border border-line bg-surface ${nodes.length === 0 ? "hidden" : ""}`}>
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
                        {node.detail || node.parent ? (
                          <span className="truncate text-[13px] text-muted">
                            {[node.parent ? model.nodes.find((item) => item.key === node.parent)?.label : "", node.detail].filter(Boolean).join(" · ")}
                          </span>
                        ) : null}
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
