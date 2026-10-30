import { Link, useNavigate } from "react-router-dom";
import { useMemo, useState, type ReactNode } from "react";
import { AlertTriangle, ExternalLink, List, LayoutGrid, Plus, Search } from "lucide-react";
import { api, type Application, type Overview } from "./api";
import { AppsTabs, ConfirmDialog, StatusBadge, appHealth, useToast, useWide } from "./kit";
import { UpdateBadge, useUpdate } from "./updates";
import { usePoll } from "./poll";
import { text, type MsgKey } from "./i18n";
import { AppLogo } from "./app-logo";
import { RemovalNotice } from "./destructions";
import { useSession } from "./session";
import { Pill } from "./dnsrecords";
import { RedundancyDialog, placements, type Placement } from "./redundancy";
import { ListRow, RowGroup } from "./rows";
import { ActionsMenu, Button, EmptyList, ErrorText, HealthDot, Icon, LoadingCards, MenuItem, PageTitle, TextButton, focus, removalOf, statusLabel, useDestructions, useListMode } from "./ui";

type Filter = "all" | "running" | "preparing" | "failed" | "paused";

const filterLabels: Record<Filter, MsgKey> = {
  all: "apps.filter.all",
  running: "status.running",
  preparing: "status.preparing",
  failed: "status.failed",
  paused: "status.paused",
};

function bucket(status: string): Exclude<Filter, "all"> {
  if (status === "running" || status === "paused" || status === "failed") return status;
  return "preparing";
}

export function Home() {
  const navigate = useNavigate();
  const { canDeploy } = useSession();
  const { data: apps, setData: setApps, error } = usePoll<Application[]>("/api/applications", 8000);
  // The overview tells where each app runs; the list shows without it.
  const overview = usePoll<Overview>("/api/overview", 15000).data;
  const where = useMemo(() => placements(overview), [overview]);
  const [mode, toggleMode] = useListMode();
  const wide = useWide();
  const jobs = useDestructions();
  const toast = useToast();
  const [pausing, setPausing] = useState<Application | null>(null);
  const [spreading, setSpreading] = useState<Application | null>(null);
  const [restarting, setRestarting] = useState<Application | null>(null);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<Filter>("all");

  async function setPaused(app: Application, paused: boolean) {
    const next = await api<Application>(`/api/applications/${app.id}/pause`, { method: "POST", body: JSON.stringify({ paused }) });
    setApps((current) => current?.map((item) => (item.id === app.id ? { ...item, ...next } : item)) ?? current);
    setPausing(null);
    toast(paused ? text("apps.pausedDone", { name: app.name }) : text("apps.resumedDone", { name: app.name }));
  }

  const all = apps ?? [];
  const statusOf = (app: Application) => appHealth(app, undefined, overview?.pods.filter((pod) => pod.appId === app.id));
  const needle = query.trim().toLowerCase();
  const shown = all.filter(
    (app) =>
      (filter === "all" || bucket(statusOf(app)) === filter) &&
      (!needle || app.name.toLowerCase().includes(needle) || (app.hostname ?? "").toLowerCase().includes(needle)),
  );
  const online = all.filter((app) => statusOf(app) === "running").length;
  const single = all.filter((app) => where.get(app.id)?.single && !app.guarded);
  const filters = (["all", "running", "preparing", "failed", "paused"] as Filter[]).filter(
    (key) => key === "all" || key === "running" || all.some((app) => bucket(statusOf(app)) === key) || key === filter,
  );

  const pause = (app: Application) => (app.paused ? void setPaused(app, false).catch((err) => toast(err.message, "bad")) : setPausing(app));

  return (
    <>
      <PageTitle
        title={text("nav.apps")}
        note={apps && apps.length > 0 ? text("apps.online", { count: online, total: all.length }) : undefined}
        phoneAction={apps && apps.length > 0 && canDeploy ? { icon: Plus, label: text("home.new"), onClick: () => navigate("/nouveau") } : undefined}
        action={
          apps && apps.length > 0 && canDeploy ? (
            <Button type="button" onClick={() => navigate("/nouveau")}>
              <Icon icon={Plus} />
              {text("home.new")}
            </Button>
          ) : null
        }
      />
      <AppsTabs active="apps" />
      {error && !apps ? <ErrorText>{error}</ErrorText> : null}
      {apps === null && !error ? <LoadingCards /> : null}
      {apps && apps.length === 0 ? (
        canDeploy ? <EmptyList message={text("home.empty")} label={text("home.new")} onCreate={() => navigate("/nouveau")} /> : <p className="text-muted">{text("home.empty")}</p>
      ) : null}
      {single.length > 0 ? (
        <section aria-label={text("overview.attention")} className="mb-8 hidden rounded-md border border-line bg-surface md:block">
          <h2 className="flex items-center gap-2 border-b border-line px-4 py-3 text-warn">
            <Icon icon={AlertTriangle} />
            {text("overview.attention")} · {single.length}
          </h2>
          <ul>
            {single.map((app) => (
              <li key={app.id} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-line px-4 py-3 first:border-t-0">
                <span className="flex flex-col">
                  <span>{text("overview.singlePoint", { name: app.name, server: where.get(app.id)?.single ?? "" })}</span>
                  <span className="text-[13px] text-muted">{text("apps.singleHint")}</span>
                </span>
                {canDeploy ? (
                  <TextButton type="button" onClick={() => setSpreading(app)}>
                    {text("apps.addCopy")}
                  </TextButton>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      {apps && apps.length > 0 ? (
        <>
          <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-3">
            <label className="flex h-11 min-w-0 flex-[1_1_260px] items-center gap-2.5 rounded-md border border-line bg-surface px-3 focus-within:border-focus md:h-auto md:py-2">
              <Icon icon={Search} className="text-faint" />
              <input
                type="search"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={text("apps.filterPlaceholder")}
                aria-label={text("apps.filterLabel")}
                className="min-w-0 flex-1 bg-transparent outline-none"
              />
            </label>
            <div role="group" aria-label={text("apps.statusGroup")} className="-mx-4 flex w-[calc(100%+2rem)] gap-2 overflow-x-auto px-4 md:mx-0 md:w-auto md:flex-wrap md:overflow-visible md:px-0">
              {filters.map((key) => (
                <Pill key={key} on={filter === key} onClick={() => setFilter(key)} count={key === "all" ? all.length : all.filter((app) => bucket(statusOf(app)) === key).length}>
                  {text(filterLabels[key])}
                </Pill>
              ))}
            </div>
            <button
              type="button"
              onClick={toggleMode}
              aria-label={mode === "cards" ? text("list.rows") : text("list.cards")}
              title={mode === "cards" ? text("list.rows") : text("list.cards")}
              className={`hidden rounded-md border border-line bg-surface p-2 text-ink md:inline-flex ${focus}`}
            >
              <Icon icon={mode === "cards" ? List : LayoutGrid} />
            </button>
          </div>
          {shown.length === 0 ? <p className="text-muted">{text("overview.noMatch")}</p> : null}
          {!wide ? (
            shown.length > 0 ? (
              <>
                <RowGroup>
                  {shown.map((app) => (
                    <PhoneRow key={app.id} app={app} status={statusOf(app)} place={where.get(app.id)} />
                  ))}
                </RowGroup>
                {shown.some((app) => where.get(app.id)?.single) ? <p className="mx-1 mt-3 text-[13px] text-muted">{text("apps.triangleHint")}</p> : null}
              </>
            ) : null
          ) : (
            <ul className={mode === "cards" ? "grid grid-cols-1 gap-4 md:grid-cols-2" : "flex flex-col overflow-hidden rounded-md border border-line bg-surface"}>
              {shown.map((app) => (
                <li key={app.id} className={mode === "cards" ? "h-full" : "border-t border-line first:border-t-0"}>
                  <AppEntry
                    app={app}
                    status={statusOf(app)}
                    place={where.get(app.id)}
                    mode={mode}
                    removal={<RemovalNotice job={removalOf(jobs, "application", app.id)} />}
                    onPause={app.guarded || !canDeploy ? undefined : () => pause(app)}
                    onRestart={app.guarded || !canDeploy ? undefined : () => setRestarting(app)}
                  />
                </li>
              ))}
            </ul>
          )}
        </>
      ) : null}
      {pausing ? (
        <ConfirmDialog title={text("apps.pauseTitle", { name: pausing.name })} confirmLabel={text("apps.pauseAction")} onConfirm={() => setPaused(pausing, true)} onClose={() => setPausing(null)}>
          {text("apps.pauseHint")}
        </ConfirmDialog>
      ) : null}
      {restarting ? (
        <ConfirmDialog
          title={text("apps.restartTitle", { name: restarting.name })}
          confirmLabel={text("apps.restart")}
          onConfirm={async () => {
            await api(`/api/applications/${restarting.id}/restart`, { method: "POST", body: "{}" });
            toast(text("apps.restarted", { name: restarting.name }));
            setRestarting(null);
          }}
          onClose={() => setRestarting(null)}
        >
          {restarting.clusterId ? text("apps.restartHintCluster") : text("apps.restartHintMachine")}
        </ConfirmDialog>
      ) : null}
      {spreading ? (
        <RedundancyDialog
          app={spreading}
          nodes={where.get(spreading.id)?.nodes ?? 0}
          onClose={() => setSpreading(null)}
          onDone={() => {
            setSpreading(null);
            toast(text("redundancy.deploying"));
          }}
        />
      ) : null}
    </>
  );
}

function loadLine(place?: Placement) {
  if (!place?.load) return "";
  return text("apps.load", { cpu: place.load.cpu, ram: place.load.ram });
}

function whereLine(place?: Placement) {
  if (!place || place.pods === 0) return "";
  const pods = place.pods === 1 ? text("overview.pod1") : text("overview.podN", { count: place.pods });
  return place.places.length > 0 ? `${pods} · ${place.places.join(", ")}` : pods;
}

function AppEntry({
  app,
  status,
  place,
  mode,
  removal,
  onPause,
  onRestart,
}: {
  app: Application;
  status: string;
  place?: Placement;
  mode: "cards" | "rows";
  removal: ReactNode;
  onPause?: () => void;
  onRestart?: () => void;
}) {
  const navigate = useNavigate();
  const line = whereLine(place);
  const menu = (
    <ActionsMenu label={text("apps.actionsOf", { name: app.name })}>
      {app.hostname ? (
        <MenuItem onClick={() => window.open(`https://${app.hostname}`, "_blank", "noreferrer")}>{text("apps.openSite")}</MenuItem>
      ) : null}
      <MenuItem onClick={() => navigate(`/applications/${app.id}`)}>{text("overview.open")}</MenuItem>
      {onPause ? <MenuItem onClick={onPause}>{app.paused ? text("home.resume") : text("apps.pauseAction")}</MenuItem> : null}
      {onRestart && !app.paused ? <MenuItem onClick={onRestart}>{text("apps.restart")}</MenuItem> : null}
    </ActionsMenu>
  );
  const domain = app.hostname ? (
    <a href={`https://${app.hostname}`} target="_blank" rel="noreferrer" className={`flex min-w-0 items-center gap-2 font-mono text-[13px] text-ink underline underline-offset-[3px] ${focus}`}>
      <span className="truncate">{app.hostname}</span>
      <Icon icon={ExternalLink} className="shrink-0 text-muted" />
    </a>
  ) : null;
  const warning = place?.single ? (
    <span className="inline-flex items-center gap-1.5 text-[13px] text-warn">
      <Icon icon={AlertTriangle} className="shrink-0" />
      {text("apps.singleShort")}
    </span>
  ) : null;
  if (mode === "rows") {
    return (
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
        <Link to={`/applications/${app.id}`} className={`flex min-w-0 flex-[1_1_220px] items-center gap-3 text-ink ${focus}`}>
          <AppLogo slug={app.logo ?? ""} name={app.name} size={28} />
          <span className="truncate">{app.name}</span>
        </Link>
        <span className="w-[120px]">
          <StatusBadge status={status} />
        </span>
        <span className="min-w-0 flex-[1_1_200px]">{domain}</span>
        <span className="min-w-0 flex-[1_1_180px] truncate text-[13px] text-muted">{[line, loadLine(place)].filter(Boolean).join(" · ")}</span>
        {warning ? <span title={text("apps.singleShort")}>{warning}</span> : null}
        {menu}
        {removal}
      </div>
    );
  }
  return (
    <div className="flex h-full flex-col gap-3 rounded-md border border-line bg-surface p-4">
      <div className="flex items-start gap-3">
        <AppLogo slug={app.logo ?? ""} name={app.name} size={36} />
        <div className="flex min-w-0 flex-1 flex-col">
          <Link to={`/applications/${app.id}`} className={`truncate font-medium text-ink ${focus}`}>
            {app.name}
          </Link>
          <span className="flex flex-wrap items-center gap-2">
            <StatusBadge status={status} />
            <AppBadge id={app.id} />
          </span>
        </div>
        {menu}
      </div>
      {removal}
      {domain}
      {line ? (
        <div className="mt-auto flex flex-wrap justify-between gap-x-3 gap-y-1 border-t border-line pt-3 text-[13px] text-muted">
          <span>{line}</span>
          {loadLine(place) ? <span>{loadLine(place)}</span> : null}
        </div>
      ) : null}
      {warning}
    </div>
  );
}

function PhoneRow({ app, status, place }: { app: Application; status: string; place?: Placement }) {
  const label = statusLabel(status);
  const quiet = status === "running";
  return (
    <ListRow
      to={`/applications/${app.id}`}
      tile={<AppLogo slug={app.logo ?? ""} name={app.name} size={40} />}
      title={app.name}
      sub={app.hostname || whereLine(place)}
      end={
        <span className="flex shrink-0 items-center gap-2 text-[13px] text-muted">
          {place?.single ? (
            <span title={text("apps.singleShort")} className="text-warn">
              <Icon icon={AlertTriangle} />
            </span>
          ) : null}
          {quiet ? null : <span className={label.tone}>{label.text}</span>}
          <HealthDot status={status} />
        </span>
      }
    />
  );
}

// AppBadge says that a new version of an application is available.
function AppBadge({ id }: { id: string }) {
  const item = useUpdate("app", id);
  return item ? <UpdateBadge item={item} /> : null;
}
