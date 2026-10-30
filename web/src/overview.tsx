import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { AlertTriangle, ExternalLink, Map as MapIcon, Search, X } from "lucide-react";
import { api, peek, type Application, type Overview as OverviewData, type OverviewPod, type OverviewServer, type UpdateItem } from "./api";
import { AppLogo } from "./app-logo";
import { text } from "./i18n";
import { useI18n } from "./lang";
import { hourlyRate, money, rateCurrency } from "./measure";
import { hostingLabel } from "./hosting";
import { UsageMeters } from "./meters";
import { placeOf } from "./places";
import { Button, ErrorText, HealthDot, Icon, IconButton, LoadingLines, TextButton, focus, liveServer, podFailed, podHealth, serverState, statusLabel, useDeployments, useDestructions } from "./ui";
import { EventList } from "./world";
import { appHealth } from "./kit";
import { urgent, useUpdates } from "./updates";
import { WorldMap, healthOf, worst, type Inset, type MapSite, type Tone } from "./worldmap";
import { useSession } from "./session";
import { RedundancyDialog } from "./redundancy";

type Mode = "serveurs" | "apps" | "pods";
type Selection = { kind: "app" | "server" | "lieu" | "pod"; id: string } | null;

const modes: Mode[] = ["serveurs", "apps", "pods"];

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
  // The card of the selection sits over the top left of the map.
  const inset: Inset = wide ? { top: 56, right: 56, bottom: 16, left: selection ? 452 : 16 } : { top: 12, right: 12, bottom: 12, left: 12 };
  const fitKey = [wide, wide && selection ? 1 : 0, ...[...frame].sort()].join("|");

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

  const card = selection ? (
    <Card
      selection={selection}
      model={model}
      select={select}
      onClose={() => select(null)}
      onPause={canDeploy ? togglePause : undefined}
      onOpen={(to) => navigate(to)}
      error={actionError}
    />
  ) : null;

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
    </Panel>
  );

  return (
    <div className={`flex flex-col gap-4 px-4 pb-4 md:h-full md:gap-3 md:overflow-hidden md:px-6 md:py-4 ${selection && !wide ? "pb-[50vh]" : ""}`}>
      <PageTitleOnly />
      <Figures model={model} loading={data === null} />
      {empty ? (
        <section className="flex max-w-[560px] flex-col gap-3 rounded-md border border-line bg-surface p-4">
          <p>{text("overview.empty")}</p>
          <Missing />
          {canDeploy ? (
            <div className="flex flex-wrap gap-3">
              <Button type="button" onClick={() => navigate("/clusters")}>{text("overview.newServer")}</Button>
              <TextButton type="button" onClick={() => navigate("/nouveau")}>{text("home.new")}</TextButton>
            </div>
          ) : null}
        </section>
      ) : null}
      <div className="flex min-h-0 flex-1 flex-col gap-4 md:flex-row md:items-stretch">
        <div className="flex min-w-0 flex-col md:min-h-0 md:w-[340px] md:shrink-0 lg:w-[380px]">{panel}</div>
        <section aria-label={mapLabel} className="flex min-w-0 flex-1 flex-col gap-4 md:min-h-0 md:gap-3">
          <div className="md:hidden">
            <TextButton type="button" aria-expanded={mapOpen} onClick={() => setMapOpen((open) => !open)}>
              <Icon icon={MapIcon} />
              {mapOpen ? text("overview.hideMap") : text("overview.showMap")}
            </TextButton>
          </div>
          <div className={`relative h-[45vh] shrink-0 overflow-hidden rounded-md border border-line md:block md:h-auto md:min-h-0 md:flex-1 ${mapOpen ? "" : "hidden"}`}>
            <WorldMap sites={sites} frame={frame} inset={inset} fitKey={fitKey} label={mapLabel} onSelect={(key) => select(key ? `lieu:${key}` : null)} />
            {selection && wide ? null : <Legend mode={mode} />}
            {selection && wide ? <div className="absolute left-4 top-4 z-10 w-[420px] max-w-[calc(100%-5rem)]">{card}</div> : null}
          </div>
          <RecentActivity />
        </section>
      </div>
      {selection && !wide ? <div className="fixed bottom-[calc(4rem+env(safe-area-inset-bottom))] left-2 right-2 z-20">{card}</div> : null}
    </div>
  );
}

// PageTitleOnly names the page and its tab.
function PageTitleOnly() {
  useEffect(() => {
    document.title = `${text("overview.title")} · MyInfra`;
    return () => {
      document.title = "MyInfra";
    };
  }, []);
  return <h1 className="sr-only">{text("overview.title")}</h1>;
}

// Legend says what the rings and figures of the map mean.
function Legend({ mode }: { mode: Mode }) {
  return (
    <div className="absolute right-4 top-4 hidden max-w-[calc(100%-2rem)] flex-wrap justify-end gap-x-4 gap-y-1 rounded-md border border-line bg-surface px-3 py-1.5 text-[12px] text-muted md:flex">
      <span className="inline-flex items-center gap-1.5">
        <span aria-hidden className="size-2.5 rounded-full border-2 border-ok" />
        {text("overview.legendOk")}
      </span>
      <span className="inline-flex items-center gap-1.5">
        <span aria-hidden className="size-2.5 rounded-full border-2 border-warn" />
        {text("overview.legendWarn")}
      </span>
      <span>{text(mode === "apps" ? "overview.legendApps" : mode === "pods" ? "overview.legendPods" : "overview.legendServers")}</span>
    </div>
  );
}

function RecentActivity() {
  return (
    <section className="flex flex-col rounded-md border border-line bg-surface px-3 py-2 md:max-h-[34%] md:min-h-0 md:shrink-0">
      <div className="mb-2 flex shrink-0 items-center justify-between gap-4">
        <h2>{text("overview.activity")}</h2>
        <Link to="/historique" className={`text-link ${focus}`}>
          {text("overview.seeAll")}
        </Link>
      </div>
      <div className="min-h-0 overflow-y-auto">
        <EventList limit={3} />
      </div>
    </section>
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
  return <section className="flex w-full flex-1 flex-col gap-3 rounded-md border border-line bg-surface p-3 md:min-h-0">{children}</section>;
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
  if (loading) return <LoadingLines lines={2} label={text("overview.figures")} />;
  const online = model.apps.filter((app) => !app.paused && app.runStatus === "running").length;
  const paused = model.apps.filter((app) => app.paused).length;
  const preparing = model.apps.length - online - paused - model.apps.filter((app) => !app.paused && app.runStatus === "failed").length;
  const failed = model.apps.filter((app) => !app.paused && app.runStatus === "failed").length;
  const linked = model.servers.filter((server) => liveServer(server)).length;
  const ready = model.pods.filter((pod) => pod.ready).length;
  const clusters = new Set(model.servers.map((server) => server.clusterId).filter(Boolean)).size;
  const sites = new Set(model.servers.map(siteOf).filter(Boolean)).size;
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
    <section aria-label={text("overview.figures")} className="grid shrink-0 grid-cols-2 gap-2 md:grid-cols-4 md:gap-3">
      <Kpi
        to="/applications"
        label={text("nav.apps")}
        value={String(online)}
        unit={text("overview.kpiOnline", { total: model.apps.length })}
        note={failed ? text("overview.kpiFailed", { count: failed }) : preparing > 0 ? text("overview.kpiPreparing", { count: preparing }) : paused ? text("overview.kpiPaused", { count: paused }) : ""}
        tone={failed ? "text-bad" : preparing > 0 ? "text-warn" : "text-muted"}
      />
      <Kpi
        to="/clusters"
        label={text("overview.servers")}
        value={String(linked)}
        unit={text("overview.kpiLinked", { total: model.servers.length })}
        note={text("overview.kpiSites", { sites, clusters })}
      />
      <Kpi
        to="/?vue=pods"
        label={text("overview.pods")}
        value={String(ready)}
        unit={text("overview.kpiReady", { total: model.pods.length })}
        note={model.pods.length > ready ? text("overview.kpiStarting", { count: model.pods.length - ready }) : ""}
        tone="text-warn"
      />
      {cost.length > 0 ? (
        <Kpi
          label={text("overview.cost")}
          title={detail}
          value={cost.map(([currency, hourly]) => `≈ ${money(hourly * 730)} ${currency}`).join(" · ")}
          unit={text("overview.kpiMonth")}
          note={cost.map(([currency, hourly]) => text("overview.perHour", { amount: `${money(hourly)} ${currency}` })).join(" · ")}
        />
      ) : null}
    </section>
  );
}

function Kpi({ to, label, value, unit, note, tone = "text-muted", title }: { to?: string; label: string; value: string; unit: string; note?: string; tone?: string; title?: string }) {
  const body = (
    <>
      <span className="flex min-w-0 items-baseline justify-between gap-2 text-[12px]">
        <span className="text-muted">{label}</span>
        {note ? <span className={`truncate ${tone}`}>{note}</span> : null}
      </span>
      <span className="truncate text-[18px] font-medium leading-tight">
        {value}
        <span className="text-[13px] font-normal text-muted">{unit}</span>
      </span>
    </>
  );
  const cls = `flex min-w-0 flex-col gap-0.5 rounded-md border border-line bg-surface px-3 py-2 text-ink ${focus}`;
  return to ? (
    <Link to={to} className={`card-link ${cls}`} title={title}>
      {body}
    </Link>
  ) : (
    <div className={cls} title={title}>
      {body}
    </div>
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
