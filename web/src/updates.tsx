import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { ArrowUpCircle, ExternalLink, RefreshCw, ShieldAlert } from "lucide-react";
import { api, type Impact, type ImpactEffect, type Server, type UpdateItem, type UpdateJob, type UpdateJobStep, type UpdatesView, type VersionView } from "./api";
import { currentLocale, knownMessage, text, type MsgKey } from "./i18n";
import { Sheet, Spinner, useToast } from "./kit";
import { useI18n } from "./lang";
import { usePoll } from "./poll";
import { useSession } from "./session";
import { Button, ErrorText, Icon, LoadingLines, PageTitle, SectionTitle, TextButton, focus } from "./ui";

type UpdatesState = { view: UpdatesView | null; reload: () => void };

const UpdatesContext = createContext<UpdatesState>({ view: null, reload: () => undefined });

// UpdatesHost reads what can be updated once a minute, for the badges of
// every page.
export function UpdatesHost({ children }: { children: ReactNode }) {
  const { data, reload } = usePoll<UpdatesView>("/api/updates", 60_000);
  const value = useMemo(() => ({ view: data, reload: () => void reload() }), [data, reload]);
  return <UpdatesContext.Provider value={value}>{children}</UpdatesContext.Provider>;
}

export function useUpdates() {
  return useContext(UpdatesContext);
}

// useUpdate is the update offered for one application, server or MyInfra.
export function useUpdate(kind: UpdateItem["kind"], id: string | undefined) {
  const { view } = useUpdates();
  if (!id) return undefined;
  return view?.updates.find((item) => item.kind === kind && item.id === id);
}

// urgent updates fix security issues, or wait for a restart to take effect.
export function urgent(item: UpdateItem) {
  return item.level === "security" || item.level === "reboot";
}

// UpdateBadge says that an update is available, in words: red when it fixes
// security issues or waits for a restart.
export function UpdateBadge({ item }: { item: UpdateItem }) {
  useI18n();
  const hot = urgent(item);
  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[12px] ${hot ? "border-bad text-bad" : "border-line text-link"}`}
      title={summary(item)}
    >
      <Icon icon={hot ? ShieldAlert : ArrowUpCircle} className="size-3.5" />
      {hot && item.level === "security" ? text("upd.badge.security") : hot ? text("upd.badge.reboot") : text("upd.badge.available")}
    </span>
  );
}

function plainImage(image: string) {
  const at = image.lastIndexOf("@sha256:");
  return at > 0 ? image.slice(0, at) : image;
}

// summary says in one line what an update changes.
export function summary(item: UpdateItem): string {
  if (item.kind === "myinfra" || item.kind === "library") return `${item.current} → ${item.available}`;
  if (item.kind === "app") {
    const services = item.detail.services ?? [];
    return services
      .map((svc) => (svc.level === "build" ? text("upd.app.build", { image: plainImage(svc.image) }) : `${plainImage(svc.image)} → ${plainImage(svc.next)}`))
      .join(" · ");
  }
  const parts: string[] = [];
  const { packages = 0, security = 0, reboot, k3s, k3sNext } = item.detail;
  if (packages > 0) parts.push(security > 0 ? text("upd.server.packagesSecurity", { count: packages, security }) : text("upd.server.packages", { count: packages }));
  if (reboot) parts.push(text("upd.server.reboot"));
  if (k3sNext) parts.push(`k3s ${k3s ?? "?"} → ${k3sNext}`);
  return parts.join(" · ");
}

function levelLabel(item: UpdateItem) {
  return text(`upd.level.${item.level}` as MsgKey);
}

export type UpdateTarget =
  | { kind: "app"; id: string; name: string; item?: UpdateItem }
  | { kind: "server"; id: string; name: string; item?: UpdateItem }
  | { kind: "cluster"; id: string; name: string; k3s: boolean }
  | { kind: "myinfra"; name: string; item?: UpdateItem; version: VersionView };

function impactPath(target: UpdateTarget) {
  switch (target.kind) {
    case "app":
      return `/api/applications/${target.id}/update/impact`;
    case "server":
      return `/api/servers/${target.id}/update/impact`;
    case "cluster":
      return `/api/clusters/${target.id}/update/impact`;
    default:
      return null;
  }
}

const effectTone: Record<ImpactEffect, string> = { none: "bg-ok", short: "bg-warn", down: "bg-bad" };

// UpdateDialog says what an update will interrupt before it starts: the
// applications that keep answering, those that stop for a moment, those that
// stop until a server is back.
export function UpdateDialog({ target, onClose, onStarted }: { target: UpdateTarget; onClose: () => void; onStarted?: (answer: { id?: string; version?: string }) => void }) {
  useI18n();
  const toast = useToast();
  const { reload } = useUpdates();
  const path = impactPath(target);
  const [impact, setImpact] = useState<Impact | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [read, setRead] = useState(false);
  const k3sOffered = target.kind === "server" ? Boolean(target.item?.detail.k3sNext) : target.kind === "cluster" ? target.k3s : false;
  const [k3s, setK3s] = useState(false);

  useEffect(() => {
    if (!path) return;
    api<Impact>(path)
      .then(setImpact)
      .catch((err) => setError(err instanceof Error ? err.message : text("common.error")));
  }, [path]);

  const major = target.kind !== "cluster" && target.item?.level === "major";
  const ready = (!path || impact !== null) && (!major || read);

  async function start() {
    setBusy(true);
    setError("");
    try {
      let answer: { id?: string; version?: string } = {};
      if (target.kind === "app") answer = await api(`/api/applications/${target.id}/update`, { method: "POST" });
      if (target.kind === "server") answer = await api(`/api/servers/${target.id}/update`, { method: "POST", body: JSON.stringify({ k3s }) });
      if (target.kind === "cluster") answer = await api(`/api/clusters/${target.id}/update`, { method: "POST", body: JSON.stringify({ k3s }) });
      if (target.kind === "myinfra") answer = await api(`/api/updates/myinfra`, { method: "POST" });
      if (target.kind !== "myinfra") toast(text("upd.started", { name: target.name }));
      reload();
      onStarted?.(answer);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  return (
    <Sheet title={text("upd.dialog.title", { name: target.name })} onClose={onClose} busy={busy} wide>
      <div className="flex flex-col gap-5">
        {target.kind !== "cluster" && target.item ? (
          <p className="text-muted">
            {summary(target.item)} · {levelLabel(target.item)}
          </p>
        ) : null}
        {target.kind === "myinfra" ? <MyInfraImpact version={target.version} /> : null}
        {path && !impact && !error ? <LoadingLines lines={3} label={text("upd.dialog.impact")} /> : null}
        {impact ? <ImpactList impact={impact} /> : null}
        {k3sOffered ? (
          <label className="flex items-start gap-3">
            <input type="checkbox" checked={k3s} onChange={(event) => setK3s(event.target.checked)} className="mt-1" />
            <span>
              {text("upd.dialog.k3s")}
              <span className="block text-[13px] text-muted">{text("upd.dialog.k3sHint")}</span>
            </span>
          </label>
        ) : null}
        {major ? (
          <label className="flex items-start gap-3">
            <input type="checkbox" checked={read} onChange={(event) => setRead(event.target.checked)} className="mt-1" />
            <span>
              {text("upd.dialog.major")}
              {target.kind === "myinfra" && target.item?.detail.url ? (
                <a href={target.item.detail.url} target="_blank" rel="noreferrer" className="ml-1 underline">
                  {text("upd.notes")}
                </a>
              ) : null}
            </span>
          </label>
        ) : null}
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div className="flex flex-wrap items-center gap-4">
          <Button type="button" onClick={start} disabled={!ready || busy}>
            {busy ? <Spinner /> : null}
            {text("upd.dialog.confirm")}
          </Button>
          <TextButton type="button" onClick={onClose} disabled={busy}>
            {text("common.cancel")}
          </TextButton>
        </div>
      </div>
    </Sheet>
  );
}

function MyInfraImpact({ version }: { version: VersionView }) {
  const key = `upd.myinfra.${version.method}` as MsgKey;
  return (
    <div className="flex items-start gap-3">
      <span className={`mt-1.5 inline-block size-2.5 shrink-0 rounded-full ${version.method === "pod" ? "bg-ok" : "bg-warn"}`} aria-hidden />
      <p>{text(key)}</p>
    </div>
  );
}

function ImpactList({ impact }: { impact: Impact }) {
  const sorted = [...impact.apps].sort((a, b) => rank(b.effect) - rank(a.effect) || a.name.localeCompare(b.name));
  return (
    <div className="flex flex-col gap-3">
      <p className="flex items-center gap-2 font-medium">
        <span className={`inline-block size-2.5 rounded-full ${effectTone[impact.effect]}`} aria-hidden />
        {text(`upd.effect.${impact.effect}` as MsgKey)}
      </p>
      {sorted.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {sorted.map((app) => (
            <li key={app.id} className="flex items-start gap-3">
              <span className={`mt-1.5 inline-block size-2 shrink-0 rounded-full ${effectTone[app.effect]}`} aria-hidden />
              <span className="min-w-0">
                <span className="font-medium">{app.name}</span>
                <span className="block text-[13px] text-muted">{text(`upd.reason.${app.reason}` as MsgKey)}</span>
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-muted">{text("upd.dialog.noApp")}</p>
      )}
      {impact.notes.map((note) => (
        <p key={note} className="text-[13px] text-muted">
          {text(`upd.note.${note.replace("-", "_")}` as MsgKey)}
        </p>
      ))}
    </div>
  );
}

function rank(effect: ImpactEffect) {
  return effect === "down" ? 2 : effect === "short" ? 1 : 0;
}

// MyInfraRestarting covers the page while MyInfra replaces itself, then
// reloads it once the new version answers.
export function MyInfraRestarting({ version }: { version: string }) {
  useI18n();
  const [late, setLate] = useState(false);
  useEffect(() => {
    const started = Date.now();
    const timer = window.setInterval(async () => {
      if (Date.now() - started > 5 * 60_000) setLate(true);
      try {
        const res = await fetch("/api/version", { headers: { Accept: "application/json" } });
        if (!res.ok) return;
        const body = (await res.json()) as VersionView;
        if (body.current === version) window.location.reload();
      } catch {
        // Not back yet.
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [version]);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-canvas/95 px-6" role="alertdialog" aria-live="assertive" aria-label={text("upd.myinfra.restarting", { version })}>
      <div className="flex max-w-sm flex-col items-center gap-4 text-center">
        {late ? <Icon icon={ShieldAlert} className="size-6 text-warn" /> : <Spinner />}
        <p className="text-lg">{text("upd.myinfra.restarting", { version })}</p>
        <p className="text-muted">{late ? text("upd.myinfra.late") : text("upd.myinfra.wait")}</p>
        {late ? (
          <Button type="button" variant="secondary" onClick={() => window.location.reload()}>
            {text("upd.myinfra.reload")}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

const stepKeys: Record<UpdateJobStep["action"], MsgKey> = {
  check: "upd.step.check",
  drain: "upd.step.drain",
  packages: "upd.step.packages",
  k3s: "upd.step.k3s",
  reboot: "upd.step.reboot",
  ready: "upd.step.ready",
  uncordon: "upd.step.uncordon",
  settle: "upd.step.settle",
};

function stepDot(status: UpdateJobStep["status"]) {
  const base = "mt-1.5 inline-block size-2.5 shrink-0 rounded-full";
  if (status === "done") return `${base} bg-ok`;
  if (status === "failed") return `${base} bg-bad`;
  if (status === "running") return `${base} animate-pulse bg-warn`;
  return `${base} border border-line`;
}

// JobTimeline follows an update server by server, step by step.
export function JobTimeline({ job }: { job: UpdateJob }) {
  const servers: { id: string; name: string; steps: UpdateJobStep[] }[] = [];
  for (const step of job.steps) {
    const last = servers[servers.length - 1];
    if (last && last.id === step.serverId) last.steps.push(step);
    else servers.push({ id: step.serverId, name: step.name, steps: [step] });
  }
  return (
    <ol className="flex flex-col gap-4" aria-live={job.status === "running" ? "polite" : "off"}>
      {servers.map((server) => {
        const status = server.steps.some((step) => step.status === "failed")
          ? "failed"
          : server.steps.every((step) => step.status === "done" || step.status === "skipped")
            ? "done"
            : server.steps.some((step) => step.status === "running")
              ? "running"
              : "pending";
        return (
          <li key={server.id}>
            <p className="flex items-center gap-2 font-medium">
              <span className={stepDot(status)} />
              <Link to={`/serveurs/${server.id}`} className="underline-offset-2 hover:underline">
                {server.name}
              </Link>
            </p>
            {status === "running" || status === "failed" ? (
              <ul className="ml-1.5 mt-2 flex flex-col gap-1.5 border-l border-line pl-4">
                {server.steps.map((step, index) => (
                  <li key={index} className={step.status === "pending" || step.status === "skipped" ? "text-faint" : step.status === "failed" ? "text-bad" : ""}>
                    {text(stepKeys[step.action], { version: step.k3s ?? "" })}
                    {step.status === "skipped" ? ` · ${text("upd.step.skipped")}` : ""}
                    {step.detail ? <span className="block text-[13px] text-muted">{knownMessage(step.detail)}</span> : null}
                  </li>
                ))}
              </ul>
            ) : null}
          </li>
        );
      })}
    </ol>
  );
}

// UpdateJobs lists the server updates of the last week, the running ones
// followed live, with resume and stop.
export function UpdateJobs({ filter }: { filter?: (job: UpdateJob) => boolean }) {
  useI18n();
  const toast = useToast();
  const { reload: reloadUpdates } = useUpdates();
  const { data, reload } = usePoll<UpdateJob[]>("/api/update-jobs", 4000);
  const [busy, setBusy] = useState("");
  const jobs = (data ?? []).filter((job) => !filter || filter(job));
  const running = jobs.some((job) => job.status === "running");
  useEffect(() => {
    if (!running) reloadUpdates();
  }, [running, reloadUpdates]);
  if (jobs.length === 0) return null;

  async function act(job: UpdateJob, action: "resume" | "cancel") {
    setBusy(job.id + action);
    try {
      await api(`/api/update-jobs/${job.id}/${action}`, { method: "POST" });
      toast(action === "resume" ? text("upd.job.resumed") : text("upd.job.stopping"));
      await reload();
    } catch (err) {
      toast(err instanceof Error ? err.message : text("common.error"), "bad");
    } finally {
      setBusy("");
    }
  }

  return (
    <section className="flex flex-col gap-6">
      <SectionTitle>{text("upd.jobs")}</SectionTitle>
      {jobs.map((job) => (
        <article key={job.id} className="rounded-md border border-line p-4">
          <header className="mb-4 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <h3 className="font-medium">{job.name}</h3>
            <span className={job.status === "failed" ? "text-bad" : job.status === "done" ? "text-ok" : job.status === "running" ? "text-warn" : "text-muted"}>
              {text(`upd.job.${job.status}` as MsgKey)} · {new Date(job.createdAt).toLocaleString(currentLocale(), { dateStyle: "short", timeStyle: "short" })}
            </span>
          </header>
          <JobTimeline job={job} />
          {job.status === "failed" || job.status === "running" ? (
            <div className="mt-4 flex flex-wrap gap-4">
              {job.status === "failed" ? (
                <Button type="button" variant="secondary" onClick={() => act(job, "resume")} disabled={busy !== ""}>
                  {busy === job.id + "resume" ? <Spinner /> : null}
                  {text("upd.job.resume")}
                </Button>
              ) : (
                <TextButton type="button" onClick={() => act(job, "cancel")} disabled={busy !== ""}>
                  {text("upd.job.stop")}
                </TextButton>
              )}
            </div>
          ) : null}
        </article>
      ))}
    </section>
  );
}

// VersionNote shows the running version under the menu, and a new one.
export function VersionNote() {
  useI18n();
  const { view } = useUpdates();
  const version = view?.version;
  if (!version) return null;
  const next = version.latest && version.latest !== version.current && view?.updates.some((item) => item.kind === "myinfra");
  return (
    <Link to="/mises-a-jour" className={`flex items-center gap-2 rounded-md text-[12px] text-faint ${focus}`}>
      <span>MyInfra {version.current}</span>
      {next ? <span className="rounded-full border border-line px-1.5 text-link">{text("upd.version.new", { version: version.latest ?? "" })}</span> : null}
    </Link>
  );
}

export function UpdatesPage() {
  useI18n();
  const { canDeploy } = useSession();
  const { view, reload } = useUpdates();
  const toast = useToast();
  const servers = usePoll<Server[]>("/api/servers", 0).data ?? [];
  const [target, setTarget] = useState<UpdateTarget | null>(null);
  const [restarting, setRestarting] = useState("");
  const [checking, setChecking] = useState(false);
  const items = view?.updates ?? [];
  const myinfra = items.find((item) => item.kind === "myinfra");
  const apps = items.filter((item) => item.kind === "app");
  const serverItems = items.filter((item) => item.kind === "server");
  const libraryItems = items.filter((item) => item.kind === "library");
  const version = view?.version;

  useEffect(() => {
    if (!checking && !view?.checking) return;
    const timer = window.setInterval(reload, 3000);
    return () => window.clearInterval(timer);
  }, [checking, view?.checking, reload]);

  useEffect(() => {
    if (checking && view && !view.checking) setChecking(false);
  }, [checking, view]);

  useEffect(() => {
    if (version?.updating) setRestarting(version.updating);
  }, [version?.updating]);

  async function checkNow() {
    setChecking(true);
    try {
      await api("/api/updates/check", { method: "POST" });
      window.setTimeout(reload, 1500);
    } catch (err) {
      setChecking(false);
      toast(err instanceof Error ? err.message : text("common.error"), "bad");
    }
  }

  // Servers grouped by cluster: a cluster is updated node after node.
  const groups = new Map<string, { clusterId?: string; name: string; items: UpdateItem[] }>();
  for (const item of serverItems) {
    const server = servers.find((row) => row.id === item.id);
    const key = server?.clusterId || `alone:${item.id}`;
    const group = groups.get(key) ?? { clusterId: server?.clusterId, name: server?.clusterName || item.name, items: [] };
    group.items.push(item);
    groups.set(key, group);
  }

  const lastCheck = view?.checkedAt ? new Date(view.checkedAt).toLocaleString(currentLocale(), { dateStyle: "short", timeStyle: "short" }) : "";
  const busyCheck = checking || Boolean(view?.checking);

  return (
    <>
      <PageTitle
        title={text("nav.updates")}
        icon={ArrowUpCircle}
        action={
          canDeploy ? (
            <Button type="button" variant="secondary" onClick={checkNow} disabled={busyCheck}>
              {busyCheck ? <Spinner /> : <Icon icon={RefreshCw} />}
              {busyCheck ? text("upd.checking") : text("upd.check")}
            </Button>
          ) : null
        }
      />
      {!view ? (
        <LoadingLines lines={4} label={text("nav.updates")} />
      ) : (
        <div className="flex flex-col gap-12">
          <p className="-mt-6 text-muted md:-mt-8">
            {text("upd.intro")} {lastCheck ? text("upd.lastCheck", { when: lastCheck }) : ""}
          </p>

          <section className="flex flex-col gap-3">
            <SectionTitle>MyInfra</SectionTitle>
            <div className="flex flex-wrap items-center justify-between gap-4 rounded-md border border-line p-4">
              <div className="min-w-0">
                <p className="font-medium">
                  {myinfra ? `${myinfra.current} → ${myinfra.available}` : text("upd.myinfra.current", { version: version?.current ?? "" })}
                </p>
                <p className="text-[13px] text-muted">
                  {myinfra ? levelLabel(myinfra) : version?.latest ? text("upd.upToDate") : text("upd.myinfra.unknown")}
                  {myinfra?.detail.url ? (
                    <>
                      {" · "}
                      <a href={myinfra.detail.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 underline">
                        {text("upd.notes")}
                        <Icon icon={ExternalLink} className="size-3.5" />
                      </a>
                    </>
                  ) : null}
                </p>
                {myinfra && version && !version.canUpdate && version.reason ? <p className="mt-2 text-[13px] text-muted">{knownMessage(version.reason)}</p> : null}
              </div>
              {myinfra && version?.canUpdate && canDeploy ? (
                <Button type="button" onClick={() => setTarget({ kind: "myinfra", name: "MyInfra", item: myinfra, version })}>
                  {text("upd.update")}
                </Button>
              ) : null}
            </div>
          </section>

          <section className="flex flex-col gap-3">
            <SectionTitle>{text("nav.apps")}</SectionTitle>
            {apps.length === 0 ? (
              <p className="text-muted">{text("upd.apps.none")}</p>
            ) : (
              <ul className="flex flex-col">
                {apps.map((item) => (
                  <li key={item.id} className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-line py-4">
                    <div className="min-w-0">
                      <p className="flex flex-wrap items-center gap-2">
                        <Link to={`/applications/${item.id}`} className="font-medium underline-offset-2 hover:underline">
                          {item.name}
                        </Link>
                        <span className="text-[13px] text-muted">{levelLabel(item)}</span>
                      </p>
                      <p className="text-[13px] text-muted [overflow-wrap:anywhere]">{summary(item)}</p>
                    </div>
                    {canDeploy ? (
                      <Button type="button" variant="secondary" onClick={() => setTarget({ kind: "app", id: item.id, name: item.name, item })}>
                        {text("upd.update")}
                      </Button>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="flex flex-col gap-3">
            <SectionTitle>{text("nav.servers")}</SectionTitle>
            {groups.size === 0 ? (
              <p className="text-muted">{text("upd.servers.none")}</p>
            ) : (
              [...groups.values()].map((group) => (
                <div key={group.clusterId ?? group.items[0].id} className="flex flex-col">
                  {group.clusterId ? (
                    <div className="flex flex-wrap items-center justify-between gap-4 border-t border-line py-3">
                      <p className="font-medium">{text("upd.cluster", { name: group.name })}</p>
                      {canDeploy ? (
                        <Button
                          type="button"
                          variant="secondary"
                          onClick={() => setTarget({ kind: "cluster", id: group.clusterId!, name: group.name, k3s: group.items.some((item) => item.detail.k3sNext) })}
                        >
                          {text("upd.cluster.update")}
                        </Button>
                      ) : null}
                    </div>
                  ) : null}
                  <ul className="flex flex-col">
                    {group.items.map((item) => (
                      <li key={item.id} className={`flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-line py-4 ${group.clusterId ? "pl-4" : ""}`}>
                        <div className="min-w-0">
                          <p className="flex flex-wrap items-center gap-2">
                            <Link to={`/serveurs/${item.id}`} className="font-medium underline-offset-2 hover:underline">
                              {item.name}
                            </Link>
                            <UpdateBadge item={item} />
                          </p>
                          <p className="text-[13px] text-muted">{summary(item)}</p>
                        </div>
                        {canDeploy ? (
                          <Button type="button" variant="secondary" onClick={() => setTarget({ kind: "server", id: item.id, name: item.name, item })}>
                            {text("upd.update")}
                          </Button>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                </div>
              ))
            )}
          </section>

          <section className="flex flex-col gap-3">
            <SectionTitle>{text("upd.library.title")}</SectionTitle>
            {libraryItems.length === 0 ? (
              <p className="text-muted">{text("upd.library.none")}</p>
            ) : (
              <>
                <p className="text-[13px] text-muted">{text("upd.library.hint")}</p>
                <ul className="flex flex-col">
                  {libraryItems.map((item) => (
                    <LibraryUpdateRow key={item.id} item={item} canDeploy={canDeploy} onDone={reload} />
                  ))}
                </ul>
              </>
            )}
          </section>

          <UpdateJobs />
        </div>
      )}
      {target ? (
        <UpdateDialog
          target={target}
          onClose={() => setTarget(null)}
          onStarted={(answer) => {
            if (target.kind === "myinfra" && answer.version) setRestarting(answer.version);
          }}
        />
      ) : null}
      {restarting ? <MyInfraRestarting version={restarting} /> : null}
    </>
  );
}

// LibraryUpdateRow offers the newer version of a package or template taken
// from the library. A copy the person changed asks once more, since the
// update replaces it.
function LibraryUpdateRow({ item, canDeploy, onDone }: { item: UpdateItem; canDeploy: boolean; onDone: () => void }) {
  useI18n();
  const toast = useToast();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const kind = item.detail.kind ?? "package";
  const id = item.detail.id ?? "";
  const link = kind === "template" ? `/templates/${id}` : `/applications/paquets/${id}`;

  async function apply() {
    if (item.detail.edited && !confirming) {
      setConfirming(true);
      return;
    }
    setBusy(true);
    try {
      await api(`/api/updates/library/${kind}/${id}`, { method: "POST" });
      toast(text("upd.library.done", { name: item.name }));
      onDone();
    } catch (err) {
      toast(err instanceof Error ? err.message : text("common.error"), "bad");
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  }

  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-line py-4">
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2">
          <Link to={link} className="font-medium underline-offset-2 hover:underline">
            {item.name}
          </Link>
          <span className="text-[13px] text-muted">
            {text(kind === "template" ? "upd.library.template" : "upd.library.package")} · {levelLabel(item)}
          </span>
        </p>
        <p className="text-[13px] text-muted">
          {item.current} → {item.available}
          {item.detail.changes ? ` · ${item.detail.changes}` : ""}
        </p>
        {item.detail.edited ? <p className="text-[13px] text-bad">{text("upd.library.edited")}</p> : null}
      </div>
      {canDeploy ? (
        <Button type="button" variant="secondary" onClick={() => void apply()} disabled={busy}>
          {busy ? <Spinner /> : null}
          {confirming ? text("upd.library.confirm") : text("upd.update")}
        </Button>
      ) : null}
    </li>
  );
}

// UpdateNotice offers the update of one application or server on its page:
// what changes, and the button that first says what it will interrupt.
function UpdateNotice({ item, target, onStarted, disabled }: { item: UpdateItem; target: UpdateTarget; onStarted?: () => void; disabled?: boolean }) {
  useI18n();
  const [open, setOpen] = useState(false);
  return (
    <div className={`mt-6 flex flex-wrap items-center justify-between gap-4 rounded-md border p-4 ${urgent(item) ? "border-bad" : "border-line"}`}>
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2">
          <UpdateBadge item={item} />
          <span className="text-[13px] text-muted">{levelLabel(item)}</span>
        </p>
        <p className="mt-1 text-[13px] text-muted [overflow-wrap:anywhere]">{summary(item)}</p>
      </div>
      <Button type="button" variant="secondary" onClick={() => setOpen(true)} disabled={disabled}>
        {text("upd.update")}
      </Button>
      {open ? <UpdateDialog target={target} onClose={() => setOpen(false)} onStarted={onStarted} /> : null}
    </div>
  );
}

export function AppUpdateNotice({ app, onStarted }: { app: { id: string; name: string; rollout?: string }; onStarted?: () => void }) {
  const item = useUpdate("app", app.id);
  if (!item) return null;
  return <UpdateNotice item={item} target={{ kind: "app", id: app.id, name: app.name, item }} onStarted={onStarted} disabled={app.rollout === "updating"} />;
}

export function ServerUpdateNotice({ server, onStarted }: { server: { id: string; name: string; status: string }; onStarted?: () => void }) {
  const item = useUpdate("server", server.id);
  if (!item) return null;
  return <UpdateNotice item={item} target={{ kind: "server", id: server.id, name: server.name, item }} onStarted={onStarted} disabled={server.status !== "linked"} />;
}

// ServerFacts are the system, kernel and Kubernetes a server runs, as its
// last check read them.
export function ServerFacts({ serverId }: { serverId: string }) {
  useI18n();
  const item = useUpdate("server", serverId);
  if (!item) return null;
  const { system, kernel, k3s } = item.detail;
  const rows = [
    [text("upd.facts.system"), system],
    [text("upd.facts.kernel"), kernel],
    ["k3s", k3s],
  ].filter(([, value]) => value);
  if (rows.length === 0) return null;
  return (
    <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-[13px]">
      {rows.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="text-muted">{label}</dt>
          <dd className="break-all font-mono">{value}</dd>
        </div>
      ))}
    </dl>
  );
}
