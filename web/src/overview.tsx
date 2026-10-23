import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { AlertTriangle, ChevronDown, ChevronUp, ExternalLink, Map as MapIcon, Search, X } from "lucide-react";
import { api, peek, type Application, type Overview as OverviewData, type OverviewPod, type OverviewServer, type UpdateItem } from "./api";
import { AppLogo } from "./app-logo";
import { text } from "./i18n";
import { useI18n } from "./lang";
import { hourlyRate, money, rateCurrency } from "./measure";
import { hostingLabel } from "./hosting";
import { UsageMeters } from "./meters";
import { placeOf } from "./places";
import { Button, ErrorText, Field, HealthDot, Icon, IconButton, LoadingLines, Select, TextButton, focus, liveServer, podFailed, podHealth, serverState, statusLabel, useDeployments, useDestructions } from "./ui";
import { EventList } from "./world";
import { appHealth } from "./kit";
import { urgent, useUpdates } from "./updates";
import { WorldMap, healthOf, worst, type Inset, type MapSite, type Tone } from "./worldmap";
import { useSession } from "./session";

type Mode = "serveurs" | "apps" | "pods";
type Selection = { kind: "app" | "server" | "lieu" | "pod"; id: string } | null;

const modes: Mode[] = ["serveurs", "apps", "pods"];
const panelWidth = 340;
const gap = 16;

function readSelection(raw: string | null): Selection {
  if (!raw) return null;
  const at = raw.indexOf(":");
  const kind = raw.slice(0, at);
  const id = raw.slice(at + 1);
  if (!id || (kind !== "app" && kind !== "server" && kind !== "lieu" && kind !== "pod")) return null;
  return { kind, id };
}

function podKey(pod: OverviewPod) {
  return `${pod.appId}/${pod.name}`;
}

function podTone(pod: OverviewPod): Tone {
  if (podFailed({ phase: pod.phase, reason: pod.reason ?? "" })) return "bad";
  return pod.ready ? "ok" : "warn";
}

function appTone(app: Application): Tone {
  if (app.paused) return "ok";
  if (app.runStatus === "failed") return "bad";
  if (app.runStatus === "running") return "ok";
  return "warn";
}

function siteOf(server: OverviewServer) {
  return placeOf(server.plan?.location)?.label || placeOf(server.place)?.label || "";
}

function useWide() {
  const query = "(min-width: 768px)";
  const [wide, setWide] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const media = window.matchMedia(query);
    const onChange = () => setWide(media.matches);
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, []);
  return wide;
}

// useHeight follows the height of an element, so the map keeps its markers
// clear of the card laid over it.
function useHeight<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [height, setHeight] = useState(0);
  useEffect(() => {
    const node = ref.current;
    if (!node) {
      setHeight(0);
      return;
    }
    const observer = new ResizeObserver(() => setHeight(node.offsetHeight));
    observer.observe(node);
    setHeight(node.offsetHeight);
    return () => observer.disconnect();
  });
  return [ref, height] as const;
}

function useOverview() {
  const [data, setData] = useState<OverviewData | null>(() => peek<OverviewData>("/api/overview"));
  const [error, setError] = useState("");
  useEffect(() => {
    let stop = false;
    const load = () => {
      if (document.hidden) return;
      api<OverviewData>("/api/overview")
        .then((next) => {
          if (stop) return;
          setData(next);
          setError("");
        })
        .catch((err) => {
          if (!stop) setError(err instanceof Error ? err.message : text("common.error"));
        });
    };
    load();
    const timer = window.setInterval(load, 10000);
    document.addEventListener("visibilitychange", load);
    return () => {
      stop = true;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", load);
    };
  }, []);
  return { data, setData, error };
}

export function Overview() {
  const { view: updatesView } = useUpdates();
  const { canDeploy } = useSession();
  useI18n();
  const wide = useWide();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const { data, setData, error } = useOverview();
  const deployments = useDeployments();
  const jobs = useDestructions();
  const mode: Mode = modes.includes(params.get("vue") as Mode) ? (params.get("vue") as Mode) : "serveurs";
  const selection = readSelection(params.get("sel"));
  const [query, setQuery] = useState("");
  const [actionError, setActionError] = useState("");
  const [cardRef, cardHeight] = useHeight<HTMLDivElement>();
  const [mapOpen, setMapOpen] = useState(false);

  function change(next: { vue?: Mode; sel?: string | null }, push = false) {
    const out = new URLSearchParams(params);
    if (next.vue) out.set("vue", next.vue);
    if (next.sel !== undefined) {
      if (next.sel) out.set("sel", next.sel);
      else out.delete("sel");
    }
    setParams(out, { replace: !push });
  }

  const select = (sel: string | null) => change({ sel }, true);

  const model = useMemo(() => {
    const servers = data?.servers ?? [];
    const apps = data?.apps ?? [];
    const pods = data?.pods ?? [];
    const serverById = new Map(servers.map((server) => [server.id, server]));
    const appById = new Map(apps.map((app) => [app.id, app]));
    const podsOfApp = new Map<string, OverviewPod[]>();
    const podsOfServer = new Map<string, OverviewPod[]>();
    for (const pod of pods) {
      podsOfApp.set(pod.appId, [...(podsOfApp.get(pod.appId) ?? []), pod]);
      const where = pod.serverId ?? "";
      podsOfServer.set(where, [...(podsOfServer.get(where) ?? []), pod]);
    }
    const hostsOf = (app: Application) => {
      const hosts = new Set((podsOfApp.get(app.id) ?? []).map((pod) => pod.serverId ?? "").filter(Boolean));
      if (!app.clusterId && app.serverId) hosts.add(app.serverId);
      return hosts;
    };
    // An app is a single point of failure when its cluster could spread it
    // and every pod still runs on the same server.
    const singlePoint = (app: Application) => {
      // A copy that restarts elsewhere from its replicated data is not one.
      if (!app.clusterId || app.paused || app.failover) return "";
      const nodes = servers.filter((server) => server.clusterId === app.clusterId && liveServer(server)).length;
      const hosts = hostsOf(app);
      if (nodes < 2 || hosts.size !== 1) return "";
      return [...hosts][0];
    };
    const places = new Map<string, OverviewServer[]>();
    for (const server of servers) {
      const label = siteOf(server);
      places.set(label, [...(places.get(label) ?? []), server]);
    }
    return { servers, apps, pods, serverById, appById, podsOfApp, podsOfServer, hostsOf, singlePoint, places };
  }, [data]);

  const lit = useMemo(() => {
    const sites = new Set<string>();
    if (!selection) return sites;
    const add = (serverId?: string) => {
      const server = serverId ? model.serverById.get(serverId) : undefined;
      if (server && siteOf(server)) sites.add(siteOf(server));
    };
    if (selection.kind === "lieu") sites.add(selection.id);
    if (selection.kind === "server") add(selection.id);
    if (selection.kind === "app") {
      const app = model.appById.get(selection.id);
      if (app) model.hostsOf(app).forEach(add);
    }
    if (selection.kind === "pod") add(model.pods.find((pod) => podKey(pod) === selection.id)?.serverId);
    return sites;
  }, [selection?.kind, selection?.id, model]);

  const sites: MapSite[] = useMemo(() => {
    const out: MapSite[] = [];
    for (const [label, servers] of model.places) {
      const place = servers[0] ? placeOf(servers[0].plan?.location) ?? placeOf(servers[0].place) : null;
      if (!label || !place) continue;
      const ids = new Set(servers.map((server) => server.id));
      const site: MapSite = { key: label, place, count: servers.length, tone: worst(servers.map((server) => healthOf(server, server.usage))) };
      if (mode === "apps") {
        const hosted = model.apps.filter((app) => [...model.hostsOf(app)].some((id) => ids.has(id)));
        site.count = hosted.length;
        site.tone = worst(hosted.map(appTone));
      }
      if (mode === "pods") {
        const pods = model.pods.filter((pod) => pod.serverId && ids.has(pod.serverId));
        site.count = pods.length;
        site.ring = pods.map(podTone);
        site.tone = worst(site.ring);
      }
      site.selected = lit.has(label);
      site.dim = lit.size > 0 && !site.selected;
      out.push(site);
    }
    return out;
  }, [model, mode, lit]);

  // The map frames the selected app, server or pod; a place picked on the map
  // keeps the current frame so the map does not jump under the pointer.
  const frame = selection && selection.kind !== "lieu" && lit.size > 0 ? [...lit] : sites.map((site) => site.key);
  const cardSpace = selection && cardHeight ? Math.round(cardHeight / 40) * 40 + gap : 0;
  const inset: Inset = wide
    ? { top: gap + 72, right: gap, bottom: gap + cardSpace, left: panelWidth + gap * 2 }
    : { top: 12, right: 12, bottom: 12, left: 12 };
  const fitKey = [wide, wide ? cardSpace : 0, ...[...frame].sort()].join("|");

  async function togglePause(app: Application) {
    if (app.guarded) return;
    try {
      const next = await api<Application>(`/api/applications/${app.id}/pause`, { method: "POST", body: JSON.stringify({ paused: !app.paused }) });
      setData((current) => (current ? { ...current, apps: current.apps.map((item) => (item.id === app.id ? { ...item, ...next } : item)) } : current));
      setActionError("");
    } catch (err) {
      setActionError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  if (error && !data) {
    return (
      <div className="px-6 py-12">
        <ErrorText>{error}</ErrorText>
      </div>
    );
  }

  const attention = attentionOf(model, deployments, jobs, select, updatesView?.updates ?? []);
  const empty = data !== null && model.servers.length === 0 && model.apps.length === 0;
  const mapLabel = text("overview.map", { sites: sites.length, servers: model.servers.length });

  const panel = (
    <Panel>
      {attention.length > 0 ? <Attention items={attention} /> : null}
      <div role="tablist" aria-label={text("overview.title")} className="grid grid-cols-3 gap-1 rounded-md bg-status p-1">
        {modes.map((item) => (
          <button
            key={item}
            type="button"
            role="tab"
            aria-selected={mode === item}
            onClick={() => change({ vue: item })}
            className={`rounded px-2 py-1 ${mode === item ? "bg-surface text-ink" : "text-muted"}`}
          >
            {text(item === "serveurs" ? "overview.servers" : item === "apps" ? "overview.apps" : "overview.pods")}
          </button>
        ))}
      </div>
      <label className="flex items-center gap-2 rounded-md border border-line bg-surface px-3 py-1.5 focus-within:border-focus">
        <Icon icon={Search} className="text-muted" />
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={text("overview.filter")}
          aria-label={text("overview.filter")}
          className="min-w-0 flex-1 bg-transparent outline-none"
        />
      </label>
      {selection?.kind === "lieu" ? (
        <div>
          <TextButton type="button" onClick={() => select(null)} aria-label={text("overview.clearPlace", { place: selection.id })}>
            <span className="rounded bg-status px-2 py-0.5">{selection.id}</span>
            <Icon icon={X} />
          </TextButton>
        </div>
      ) : null}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {data === null ? (
          <LoadingLines lines={4} />
        ) : (
          <Rows mode={mode} model={model} query={query} place={selection?.kind === "lieu" ? selection.id : ""} selection={selection} select={select} />
        )}
      </div>
      <Activity />
    </Panel>
  );

  return (
    <div className={`flex flex-col md:relative md:h-full ${selection ? "pb-[50vh] md:pb-0" : ""}`}>
      <h1 className="sr-only">{text("overview.title")}</h1>
      {/* On a phone the list comes first and the map opens on demand, so that
          scrolling the page never turns into moving the map. */}
      <div className={`relative order-2 h-[45vh] shrink-0 overflow-hidden border-y border-line md:order-none md:absolute md:inset-0 md:block md:h-auto md:border-0 ${mapOpen ? "" : "hidden"}`}>
        <WorldMap sites={sites} frame={frame} inset={inset} fitKey={fitKey} label={mapLabel} onSelect={(key) => select(key ? `lieu:${key}` : null)} />
      </div>
      <div className="order-1 flex flex-col gap-4 p-4 md:contents">
        <div className="md:absolute md:right-4 md:top-4 md:max-w-[calc(100%-388px)]">
          <Figures model={model} loading={data === null} />
        </div>
        <div className="md:hidden">
          <TextButton type="button" aria-expanded={mapOpen} onClick={() => setMapOpen((open) => !open)}>
            <Icon icon={MapIcon} />
            {mapOpen ? text("overview.hideMap") : text("overview.showMap")}
          </TextButton>
        </div>
        <div className="md:absolute md:bottom-4 md:left-4 md:top-4 md:flex md:w-[340px]">{panel}</div>
        {selection ? (
          <div
            ref={cardRef}
            className="fixed bottom-[calc(4rem+env(safe-area-inset-bottom))] left-2 right-2 z-20 md:absolute md:bottom-4 md:left-[372px] md:right-16 md:z-auto md:max-w-[620px]"
          >
            <Card
              selection={selection}
              model={model}
              select={select}
              onClose={() => select(null)}
              onPause={canDeploy ? togglePause : undefined}
              onOpen={(to) => navigate(to)}
              error={actionError}
            />
          </div>
        ) : null}
        {empty ? (
          <div className="md:absolute md:left-[372px] md:top-1/2 md:max-w-[420px] md:-translate-y-1/2">
            <Panel>
              <p>{text("overview.empty")}</p>
              <Missing />
              {canDeploy ? (
                <div className="flex flex-wrap gap-3">
                  <Button type="button" onClick={() => navigate("/clusters")}>{text("overview.newServer")}</Button>
                  <TextButton type="button" onClick={() => navigate("/nouveau")}>{text("home.new")}</TextButton>
                </div>
              ) : null}
            </Panel>
          </div>
        ) : null}
      </div>
    </div>
  );
}

// Missing lists what is still to set up before a first server, each with the
// page that does it: what the start guide left for later.
function Missing() {
  const [secrets, setSecrets] = useState<{ kind: string }[] | null>(null);
  useEffect(() => {
    api<{ kind: string }[]>("/api/secrets")
      .then(setSecrets)
      .catch(() => setSecrets(null));
  }, []);
  if (!secrets) return null;
  const todo = [
    { kind: "provider_token", to: "/providers", label: text("overview.needProvider") },
    { kind: "ssh_key", to: "/cles", label: text("overview.needKey") },
    { kind: "dns_token", to: "/dns", label: text("overview.needDns") },
  ].filter((item) => !secrets.some((secret) => secret.kind === item.kind));
  if (todo.length === 0) return null;
  return (
    <div>
      <p className="mb-2 text-muted">{text("overview.todo")}</p>
      <ul className="flex flex-col gap-1">
        {todo.map((item) => (
          <li key={item.kind}>
            <Link to={item.to} className="underline">
              {item.label}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}

type Model = {
  servers: OverviewServer[];
  apps: Application[];
  pods: OverviewPod[];
  serverById: Map<string, OverviewServer>;
  appById: Map<string, Application>;
  podsOfApp: Map<string, OverviewPod[]>;
  podsOfServer: Map<string, OverviewPod[]>;
  hostsOf: (app: Application) => Set<string>;
  singlePoint: (app: Application) => string;
  places: Map<string, OverviewServer[]>;
};

function Panel({ children }: { children: ReactNode }) {
  return <section className="flex w-full flex-col gap-3 rounded-md border border-line bg-surface p-4 shadow-sm">{children}</section>;
}

type Item = { key: string; tone: Tone; label: string; to?: string; onClick?: () => void };

function attentionOf(
  model: Model,
  deployments: { id: string; name: string; to: string }[],
  jobs: { id: string; name: string; status: string }[],
  select: (sel: string) => void,
  updates: UpdateItem[],
): Item[] {
  const items: Item[] = [];
  for (const app of model.apps) {
    if (!app.paused && (app.runStatus === "failed" || app.runStatus === "unreachable")) items.push({ key: `app:${app.id}`, tone: "bad", label: text(app.runStatus === "unreachable" ? "overview.appUnreachable" : "overview.appFailed", { name: app.name }), onClick: () => select(`app:${app.id}`) });
  }
  for (const server of model.servers) {
    if (server.reach) items.push({ key: `server:${server.id}`, tone: "bad", label: text(server.reach === "gone" ? "overview.serverGone" : "overview.serverLost", { name: server.name }), to: `/serveurs/${server.id}` });
    else if (server.status === "failed") items.push({ key: `server:${server.id}`, tone: "bad", label: text("overview.serverFailed", { name: server.name }), to: `/serveurs/${server.id}` });
  }
  for (const item of deployments) items.push({ key: `deploy:${item.id}`, tone: "warn", label: text("overview.deploying", { name: item.name }), to: item.to });
  for (const job of jobs) {
    if (job.status === "running") items.push({ key: `job:${job.id}`, tone: "warn", label: text("overview.removing", { name: job.name }), to: `/destructions/${job.id}` });
  }
  for (const app of model.apps) {
    const host = model.singlePoint(app);
    if (host) {
      items.push({
        key: `spof:${app.id}`,
        tone: "warn",
        label: text("overview.singlePoint", { name: app.name, server: model.serverById.get(host)?.name ?? "" }),
        onClick: () => select(`app:${app.id}`),
      });
    }
  }
  // One line for every update: red when one fixes security or waits for a restart.
  if (updates.length > 0) {
    items.push({ key: "updates", tone: updates.some(urgent) ? "bad" : "warn", label: text("upd.waiting", { count: updates.length }), to: "/mises-a-jour" });
  }
  return items;
}

function Attention({ items }: { items: Item[] }) {
  const tone = worst(items.map((item) => item.tone));
  return (
    <section aria-label={text("overview.attention")} className="flex flex-col gap-1 border-b border-line pb-3">
      <h2 className={`flex items-center gap-2 ${tone === "bad" ? "text-bad" : "text-warn"}`}>
        <Icon icon={AlertTriangle} />
        {text("overview.attention")} · {items.length}
      </h2>
      <ul className="flex max-h-40 flex-col gap-1 overflow-y-auto">
        {items.map((item) => (
          <li key={item.key} className="flex items-center gap-2">
            <Dot tone={item.tone} />
            {item.to ? (
              <Link to={item.to} className="truncate underline-offset-2 hover:underline">{item.label}</Link>
            ) : (
              <button type="button" onClick={item.onClick} className="truncate text-left underline-offset-2 hover:underline">
                {item.label}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

function Dot({ tone }: { tone: Tone }) {
  const color = tone === "ok" ? "bg-ok" : tone === "warn" ? "bg-warn" : "bg-bad";
  return <span className={`inline-block size-2.5 shrink-0 rounded-full ${color}`} aria-hidden />;
}

function matches(value: string, query: string) {
  const clean = (input: string) => input.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();
  return clean(value).includes(clean(query.trim()));
}

function Rows({
  mode,
  model,
  query,
  place,
  selection,
  select,
}: {
  mode: Mode;
  model: Model;
  query: string;
  place: string;
  selection: Selection;
  select: (sel: string) => void;
}) {
  const inPlace = (serverId?: string) => {
    if (!place) return true;
    const server = serverId ? model.serverById.get(serverId) : undefined;
    return server ? siteOf(server) === place : false;
  };
  const active = (kind: string, id: string) => selection?.kind === kind && selection.id === id;
  const rowClass = (on: boolean) => `flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left ${on ? "bg-status" : ""}`;

  if (mode === "serveurs") {
    const groups = [...model.places.entries()]
      .map(([label, servers]) => [label, servers.filter((server) => inPlace(server.id) && matches(server.name, query))] as const)
      .filter(([, servers]) => servers.length > 0)
      .sort(([a], [b]) => (a === "" ? 1 : b === "" ? -1 : a.localeCompare(b)));
    if (groups.length === 0) return <Nothing />;
    return (
      <div className="flex flex-col gap-3">
        {groups.map(([label, servers]) => (
          <section key={label}>
            <h3 className="mb-1 px-2 text-muted">{label || text("overview.unknownPlace")}</h3>
            <ul>
              {servers.map((server) => (
                <li key={server.id}>
                  <button type="button" className={rowClass(active("server", server.id))} aria-pressed={active("server", server.id)} onClick={() => select(`server:${server.id}`)}>
                    <HealthDot status={serverState(server)} />
                    <span className="min-w-0 flex-1 truncate">{server.name}</span>
                    <span className="shrink-0 text-muted">{rate(server)}</span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    );
  }

  if (mode === "apps") {
    const apps = model.apps.filter((app) => (!place || [...model.hostsOf(app)].some((id) => inPlace(id))) && matches(app.name, query));
    if (apps.length === 0) return <Nothing />;
    return (
      <ul>
        {apps.map((app) => {
          const pods = model.podsOfApp.get(app.id) ?? [];
          const hosts = [...model.hostsOf(app)].map((id) => model.serverById.get(id)?.name).filter(Boolean);
          return (
            <li key={app.id}>
              <button type="button" className={rowClass(active("app", app.id))} aria-pressed={active("app", app.id)} onClick={() => select(`app:${app.id}`)}>
                <AppLogo slug={app.logo ?? ""} name={app.name} size={24} />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-2">
                    <span className="truncate">{app.name}</span>
                    <HealthDot status={appHealth(app, undefined, model.podsOfApp.get(app.id))} />
                    {model.singlePoint(app) ? <Icon icon={AlertTriangle} className="text-warn" /> : null}
                  </span>
                  <span className="block truncate text-muted">
                    {podCount(pods.length)}
                    {hosts.length > 0 ? ` · ${hosts.join(", ")}` : ""}
                  </span>
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    );
  }

  const groups = [...model.podsOfServer.entries()]
    .map(([serverId, pods]) => [serverId, pods.filter((pod) => inPlace(pod.serverId) && (matches(pod.name, query) || matches(model.appById.get(pod.appId)?.name ?? "", query)))] as const)
    .filter(([, pods]) => pods.length > 0)
    .sort(([a], [b]) => (a === "" ? 1 : b === "" ? -1 : (model.serverById.get(a)?.name ?? "").localeCompare(model.serverById.get(b)?.name ?? "")));
  if (groups.length === 0) return <Nothing />;
  return (
    <div className="flex flex-col gap-3">
      {groups.map(([serverId, pods]) => (
        <section key={serverId}>
          <h3 className="mb-1 px-2 text-muted">{model.serverById.get(serverId)?.name || text("overview.noServer")}</h3>
          <ul>
            {pods.map((pod) => (
              <li key={podKey(pod)}>
                <button type="button" className={rowClass(active("pod", podKey(pod)))} aria-pressed={active("pod", podKey(pod))} onClick={() => select(`pod:${podKey(pod)}`)}>
                  <Dot tone={podTone(pod)} />
                  <span className="min-w-0 flex-1 truncate font-mono text-[13px]">{pod.name}</span>
                  <span className="shrink-0 truncate text-muted">{model.appById.get(pod.appId)?.name}</span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function Nothing() {
  return <p className="px-2 text-muted">{text("overview.noMatch")}</p>;
}

function podCount(count: number) {
  return count === 1 ? text("overview.pod1") : text("overview.podN", { count });
}

function unit(currency?: string) {
  return currency === "USD" ? "$" : "€";
}

function rate(server: OverviewServer) {
  const hourly = hourlyRate(server);
  if (!hourly) return "";
  return text("overview.perHour", { amount: `${money(hourly)} ${unit(rateCurrency(server))}` });
}

function Figures({ model, loading }: { model: Model; loading: boolean }) {
  if (loading) return null;
  const online = model.apps.filter((app) => !app.paused && app.runStatus === "running").length;
  const linked = model.servers.filter((server) => liveServer(server)).length;
  const ready = model.pods.filter((pod) => pod.ready).length;
  const totals = new Map<string, number>();
  const providers = new Map<string, number>();
  for (const server of model.servers) {
    const hourly = hourlyRate(server);
    if (!hourly) continue;
    const currency = unit(rateCurrency(server));
    totals.set(currency, (totals.get(currency) ?? 0) + hourly);
    const name = `${hostingLabel(server)} (${currency})`;
    providers.set(name, (providers.get(name) ?? 0) + hourly);
  }
  const cost = [...totals.entries()];
  const detail = [text("overview.costHint"), ...[...providers.entries()].map(([name, hourly]) => `${name} : ${money(hourly)}/h`)].join("\n");
  return (
    <section aria-label={text("overview.figures")} className="flex flex-wrap gap-x-6 gap-y-2 rounded-md border border-line bg-surface px-4 py-3 shadow-sm">
      <Figure label={text("overview.apps")} value={text("overview.online", { count: online, total: model.apps.length })} />
      <Figure label={text("overview.servers")} value={text("overview.linked", { count: linked, total: model.servers.length })} />
      <Figure label={text("overview.pods")} value={text("overview.ready", { count: ready, total: model.pods.length })} />
      {cost.length > 0 ? (
        <details title={detail} className="group">
          <summary className={`flex cursor-pointer list-none flex-col rounded-md ${focus}`}>
            <span className="text-muted">{text("overview.cost")}</span>
            {cost.map(([currency, hourly]) => (
              <span key={currency} className="whitespace-nowrap">
                {text("overview.perHour", { amount: `${money(hourly)} ${currency}` })}
                <span className="text-muted"> · {text("overview.perMonth", { amount: `${money(hourly * 730)} ${currency}` })}</span>
              </span>
            ))}
          </summary>
          <p className="mt-2 whitespace-pre-line text-[13px] text-muted">{detail}</p>
        </details>
      ) : null}
    </section>
  );
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col">
      <span className="text-muted">{label}</span>
      <span className="whitespace-nowrap">{value}</span>
    </div>
  );
}

function Activity() {
  const [open, setOpen] = useState(false);
  return (
    <section className="border-t border-line pt-3">
      <h2>
        <button type="button" aria-expanded={open} onClick={() => setOpen((value) => !value)} className="-mx-1.5 inline-flex items-center gap-2 rounded-md px-1.5 py-0.5">
          <Icon icon={open ? ChevronUp : ChevronDown} />
          {text("overview.activity")}
        </button>
      </h2>
      {open ? (
        <div className="mt-2 max-h-56 overflow-y-auto">
          <EventList limit={5} />
          <Link to="/historique" className="mt-2 inline-block text-link">{text("nav.history")}</Link>
        </div>
      ) : null}
    </section>
  );
}

function Card({
  selection,
  model,
  select,
  onClose,
  onPause,
  onOpen,
  error,
}: {
  selection: NonNullable<Selection>;
  model: Model;
  select: (sel: string) => void;
  onClose: () => void;
  onPause?: (app: Application) => void;
  onOpen: (to: string) => void;
  error: string;
}) {
  let body: ReactNode = <p className="text-muted">{text("overview.noMatch")}</p>;
  if (selection.kind === "app") {
    const app = model.appById.get(selection.id);
    if (app) body = <AppCard app={app} model={model} select={select} onPause={onPause} onOpen={onOpen} />;
  }
  if (selection.kind === "server") {
    const server = model.serverById.get(selection.id);
    if (server) body = <ServerCard server={server} model={model} select={select} onOpen={onOpen} />;
  }
  if (selection.kind === "lieu") {
    const servers = model.places.get(selection.id) ?? [];
    const hourly = servers.reduce((sum, server) => sum + hourlyRate(server), 0);
    body = (
      <>
        <CardTitle title={selection.id} note={hourly ? text("overview.perHour", { amount: `${money(hourly)} €` }) : undefined} />
        <ul className="flex flex-col">
          {servers.map((server) => (
            <li key={server.id}>
              <button type="button" onClick={() => select(`server:${server.id}`)} className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left">
                <HealthDot status={serverState(server)} />
                <span className="flex-1 truncate">{server.name}</span>
                <span className="text-muted">{server.clusterName || text("server.alone")}</span>
              </button>
            </li>
          ))}
        </ul>
      </>
    );
  }
  if (selection.kind === "pod") {
    const pod = model.pods.find((item) => podKey(item) === selection.id);
    if (pod) {
      const app = model.appById.get(pod.appId);
      const server = pod.serverId ? model.serverById.get(pod.serverId) : undefined;
      const health = podHealth({ ready: pod.ready, phase: pod.phase, reason: pod.reason ?? "" });
      body = (
        <>
          <CardTitle title={pod.name} note={health.text} tone={health.tone} />
          <p className="flex flex-wrap gap-x-4 text-muted">
            {app ? <TextButton type="button" onClick={() => select(`app:${app.id}`)}>{text("overview.podOf", { app: app.name })}</TextButton> : null}
            {server ? <TextButton type="button" onClick={() => select(`server:${server.id}`)}>{server.name}</TextButton> : null}
          </p>
          {app ? (
            <div>
              <Button type="button" onClick={() => onOpen(`/applications/${app.id}`)}>{text("overview.open")}</Button>
            </div>
          ) : null}
        </>
      );
    }
  }
  return (
    <section aria-label={text("overview.selection")} className="relative flex max-h-[50vh] flex-col gap-3 overflow-y-auto rounded-md border border-line bg-surface p-4 shadow-sm md:max-h-[45vh]">
      <div className="absolute right-3 top-3">
        <IconButton type="button" icon={X} label={text("overview.close")} onClick={onClose} />
      </div>
      {body}
      {error ? <ErrorText>{error}</ErrorText> : null}
    </section>
  );
}

function CardTitle({ title, note, tone = "text-muted", logo }: { title: string; note?: string; tone?: string; logo?: ReactNode }) {
  return (
    <h2 className="flex flex-wrap items-center gap-x-3 gap-y-1 pr-8">
      {logo}
      <span className="font-medium">{title}</span>
      {note ? <span className={tone}>{note}</span> : null}
    </h2>
  );
}

function AppCard({
  app,
  model,
  select,
  onPause,
  onOpen,
}: {
  app: Application;
  model: Model;
  select: (sel: string) => void;
  onPause?: (app: Application) => void;
  onOpen: (to: string) => void;
}) {
  const { canDeploy } = useSession();
  const status = statusLabel(appHealth(app, undefined, model.podsOfApp.get(app.id)));
  const pods = model.podsOfApp.get(app.id) ?? [];
  const byServer = new Map<string, OverviewPod[]>();
  for (const pod of pods) byServer.set(pod.serverId ?? "", [...(byServer.get(pod.serverId ?? "") ?? []), pod]);
  const host = model.singlePoint(app);
  const [asking, setAsking] = useState(false);
  const [spreading, setSpreading] = useState(false);
  // The notice stays until the copies are spread; then the warning goes too.
  useEffect(() => {
    if (!host) setSpreading(false);
  }, [host]);
  return (
    <>
      <CardTitle title={app.name} note={status.text} tone={status.tone} logo={<AppLogo slug={app.logo ?? ""} name={app.name} size={28} />} />
      {app.hostname ? (
        <a href={`https://${app.hostname}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 self-start text-link">
          {app.hostname}
          <Icon icon={ExternalLink} />
        </a>
      ) : null}
      {byServer.size > 0 ? (
        <ul className="flex flex-wrap gap-2">
          {[...byServer.entries()].map(([serverId, list]) => (
            <li key={serverId}>
              <button
                type="button"
                onClick={() => serverId && select(`server:${serverId}`)}
                className="inline-flex items-center gap-2 rounded-md border border-line px-2 py-1"
              >
                <span className="flex gap-0.5">
                  {list.map((pod) => (
                    <Dot key={pod.name} tone={podTone(pod)} />
                  ))}
                </span>
                {model.serverById.get(serverId)?.name || text("overview.noServer")}
                <span className="text-muted">{podCount(list.length)}</span>
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      {host ? (
        <p className="flex items-start gap-2 text-warn">
          <Icon icon={AlertTriangle} className="mt-1 shrink-0" />
          {text(pods.length > 1 ? "overview.singlePointPlaced" : "overview.singlePointLong", { server: model.serverById.get(host)?.name ?? "" })}
        </p>
      ) : null}
      {host && !app.guarded && !spreading && canDeploy ? (
        <div>
          <Button type="button" onClick={() => setAsking(true)}>{text("redundancy.action")}</Button>
        </div>
      ) : null}
      {spreading ? <p className="text-muted">{text("redundancy.deploying")}</p> : null}
      {asking ? (
        <RedundancyDialog
          app={app}
          nodes={model.servers.filter((server) => server.clusterId === app.clusterId && liveServer(server)).length}
          onClose={() => setAsking(false)}
          onDone={() => {
            setAsking(false);
            setSpreading(true);
          }}
        />
      ) : null}
      {!app.clusterId && app.serverId ? <p className="text-muted">{text("overview.aloneNote")}</p> : null}
      <div className="flex flex-wrap items-center gap-4">
        <Button type="button" onClick={() => onOpen(`/applications/${app.id}`)}>{text("overview.open")}</Button>
        {app.guarded || !onPause ? null : (
          <TextButton type="button" onClick={() => onPause(app)}>
            {app.paused ? text("home.resume") : "Pause"}
          </TextButton>
        )}
      </div>
    </>
  );
}

type RedundancyPlan = { scaled: string[]; failover: string[]; database: string[]; alone: string[] };
type RedundancyOptions = { plan: RedundancyPlan; nodes: number; storageNodes: number; backedUp: string[] };

function RedundancyDialog({ app, nodes, onClose, onDone }: { app: Application; nodes: number; onClose: () => void; onDone: () => void }) {
  const [copies, setCopies] = useState(Math.min(3, nodes));
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  // The server knows how each service keeps its data, and so what it can become.
  const [options, setOptions] = useState<RedundancyOptions | null>(null);
  useEffect(() => {
    let stop = false;
    api<RedundancyOptions>(`/api/applications/${app.id}/redundancy`)
      .then((next) => {
        if (!stop) setOptions(next);
      })
      .catch((err) => {
        if (!stop) setError(err instanceof Error ? err.message : text("common.error"));
      });
    return () => {
      stop = true;
    };
  }, [app.id]);
  const plan = options?.plan;
  const needsCopies = Boolean(plan && plan.scaled.length + plan.database.length > 0);
  const needsStorage = Boolean(plan && plan.failover.length + plan.database.length > 0);
  const storageShort = Boolean(options && needsStorage && options.nodes < options.storageNodes);
  const actionable = Boolean(plan && plan.scaled.length + plan.failover.length + plan.database.length > 0);
  const choices = Array.from({ length: Math.max(0, nodes - 1) }, (_, i) => i + 2);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSending(true);
    setError("");
    try {
      await api(`/api/applications/${app.id}/redundancy`, { method: "POST", body: JSON.stringify({ copies: needsCopies ? copies : 0 }) });
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setSending(false);
    }
  }

  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center bg-ink/40 px-6" role="dialog" aria-modal="true" aria-labelledby="redundancy-title">
      <form onSubmit={submit} className="flex max-h-[80vh] w-full max-w-md flex-col rounded-md border border-line bg-surface p-6">
        <h2 id="redundancy-title" className="mb-3 text-lg">{text("redundancy.title", { name: app.name })}</h2>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <p className="mb-6 text-muted">{text("redundancy.intro")}</p>
          {!options && !error ? <LoadingLines lines={2} label={text("field.services")} /> : null}
          {plan && !actionable ? <p className="mb-6 text-warn">{text("redundancy.none")}</p> : null}
          {storageShort ? <p className="mb-6 text-warn">{text("redundancy.storageShort", { count: options?.storageNodes ?? 3 })}</p> : null}
          {plan && actionable ? (
            <>
              {needsCopies ? (
                <div className="mb-6">
                  <Field label={text("redundancy.copies")}>
                    <Select value={copies} onChange={(e) => setCopies(Number(e.target.value))} autoFocus>
                      {choices.map((count) => (
                        <option key={count} value={count}>
                          {text("redundancy.option", { count })}
                        </option>
                      ))}
                    </Select>
                  </Field>
                </div>
              ) : null}
              <ul className="mb-6">
                {plan.scaled.map((name) => (
                  <li key={name} className="border-t border-line py-2">
                    {text("redundancy.scaled", { name, count: copies })}
                  </li>
                ))}
                {plan.database.map((name) => (
                  <li key={name} className="border-t border-line py-2">
                    {text("redundancy.database", { name, count: copies })}
                  </li>
                ))}
                {plan.failover.map((name) => (
                  <li key={name} className="border-t border-line py-2">
                    {options?.backedUp.includes(name) ? text("redundancy.failoverOn", { name }) : text("redundancy.failover", { name })}
                  </li>
                ))}
                {plan.alone.map((name) => (
                  <li key={name} className="border-t border-line py-2 text-muted">
                    {text("redundancy.alone", { name })}
                  </li>
                ))}
              </ul>
            </>
          ) : null}
          {error ? (
            <div className="mb-6">
              <ErrorText>{error}</ErrorText>
            </div>
          ) : null}
        </div>
        <div className="flex items-center gap-4">
          {actionable ? (
            <Button type="submit" disabled={sending || storageShort || (needsCopies && choices.length === 0)}>
              {sending ? <span className="size-4 animate-spin rounded-full border border-current border-t-transparent" /> : null}
              {text("redundancy.deploy")}
            </Button>
          ) : null}
          <TextButton type="button" onClick={onClose}>
            {text("common.cancel")}
          </TextButton>
        </div>
      </form>
    </div>
  );
}

function ServerCard({ server, model, select, onOpen }: { server: OverviewServer; model: Model; select: (sel: string) => void; onOpen: (to: string) => void }) {
  const status = statusLabel(serverState(server));
  const apps = model.apps.filter((app) => model.hostsOf(app).has(server.id));
  const pods = model.podsOfServer.get(server.id) ?? [];
  const facts = [hostingLabel(server), siteOf(server) || server.plan?.location, server.plan?.name, server.clusterName || text("server.alone"), rate(server)].filter(Boolean);
  return (
    <>
      <CardTitle title={server.name} note={status.text} tone={status.tone} />
      <p className="text-muted">{facts.join(" · ")}</p>
      {server.clusterId ? <UsageMeters sample={server.usage ?? { measured: false, cpu: zero, memory: zero, disk: zero }} /> : null}
      {apps.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-muted">{text("overview.hosted")} :</span>
          {apps.map((app) => (
            <button key={app.id} type="button" onClick={() => select(`app:${app.id}`)} className="inline-flex items-center gap-2 rounded-md border border-line px-2 py-0.5">
              <HealthDot status={appHealth(app, undefined, model.podsOfApp.get(app.id))} />
              {app.name}
            </button>
          ))}
          <span className="text-muted">· {podCount(pods.length)}</span>
        </div>
      ) : (
        <p className="text-muted">{text("overview.noApps")}</p>
      )}
      <div>
        <Button type="button" onClick={() => onOpen(`/serveurs/${server.id}`)}>{text("overview.open")}</Button>
      </div>
    </>
  );
}

const zero = { used: 0, total: 0 };
