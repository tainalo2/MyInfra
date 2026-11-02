import { FormEvent, useEffect, useMemo, useRef, useState, type DragEvent } from "react";
import { AlertTriangle, Plus } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { api, peek, type Application, type ArgoApp, type Cluster, type ClusterUsage, type Destruction, type Gauge, type Overview, type Server, type ServerUsage, type Template } from "./api";
import { text } from "./i18n";
import { Code } from "./code-lazy";
import { ConfirmDialog, Spinner, StatusBadge, useToast } from "./kit";
import { usePoll } from "./poll";
import { money } from "./measure";
import { UsageMeters } from "./meters";
import { RemovalNotice } from "./destructions";
import { MoveServerDialog, ServerForm, billText } from "./servers";
import { machineLine } from "./offers";
import { placeLabel } from "./places";
import { UpdateBadge, UpdateDialog, urgent, useUpdate, useUpdates } from "./updates";
import { hourlyRate } from "./measure";
import { useSession } from "./session";
import { useActivity } from "./activity";
import { ActionsMenu, Button, EmptyList, ErrorText, Field, HealthDot, Icon, Input, ItemList, ListModeButton, LoadingCards, LoadingLines, MenuItem, PageTitle, Skeleton, TextButton, entryClass, focus, liveServer, removalOf, serverState, statusLabel, useDestructions, useListMode, type ListMode } from "./ui";

export function Clusters() {
  const { canDeploy } = useSession();
  const [clusters, setClusters] = useState<Cluster[]>(() => peek<Cluster[]>("/api/clusters") ?? []);
  const [servers, setServers] = useState<Server[]>(() => peek<Server[]>("/api/servers") ?? []);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [ready, setReady] = useState(() => peek("/api/clusters") !== null && peek("/api/servers") !== null);
  const [creatingCluster, setCreatingCluster] = useState(false);
  const [importing, setImporting] = useState(false);
  const [kubeconfig, setKubeconfig] = useState("");
  const [creatingServer, setCreatingServer] = useState("");
  const [mode, toggleMode] = useListMode();
  const [busy, setBusy] = useState(false);
  const [fresh, setFresh] = useState("");
  const [dragId, setDragId] = useState<string | null>(null);
  const [over, setOver] = useState<string | null>(null);
  const [pending, setPending] = useState<{ server: Server; clusterId: string; name: string } | null>(null);
  const { updating, transferring } = useActivity();
  const toast = useToast();
  // The overview tells which apps run on each server.
  const overview = usePoll<Overview>("/api/overview", 15000).data;
  const podsByServer = useMemo(() => {
    const out = new Map<string, string[]>();
    const names = new Map((overview?.apps ?? []).map((app) => [app.id, app.name]));
    for (const pod of overview?.pods ?? []) {
      if (!pod.serverId) continue;
      out.set(pod.serverId, [...(out.get(pod.serverId) ?? []), names.get(pod.appId) ?? ""]);
    }
    return out;
  }, [overview]);

  async function reload() {
    const [nextClusters, nextServers] = await Promise.all([api<Cluster[]>("/api/clusters"), api<Server[]>("/api/servers")]);
    setClusters(nextClusters);
    setServers(nextServers);
  }

  useEffect(() => {
    reload()
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
  }, []);

  function close() {
    setFresh("");
    setCreatingCluster(false);
    setImporting(false);
    setCreatingServer("");
    setName("");
    setKubeconfig("");
    setError("");
  }

  // A new cluster is empty: its first server is asked for at once, in the
  // same place, instead of leaving the person to find "New server".
  async function add(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setError("");
    setBusy(true);
    try {
      const created = await api<Cluster>("/api/clusters", { method: "POST", body: JSON.stringify({ name }) });
      await reload();
      setName("");
      setCreatingCluster(false);
      setFresh(created.id);
      setCreatingServer(created.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy(false);
    }
  }

  async function importCluster(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setError("");
    setBusy(true);
    try {
      await api("/api/clusters/import", { method: "POST", body: JSON.stringify({ name, kubeconfig }) });
      toast(text("cluster.imported", { name }));
      close();
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy(false);
    }
  }

  const creating = creatingCluster || importing || creatingServer !== "";
  const target = clusters.find((cluster) => cluster.id === creatingServer);
  const lone = servers.filter((server) => !server.clusterId);
  const dragged = servers.find((server) => server.id === dragId);

  function canDrag(server: Server) {
    return canDeploy && liveServer(server) && !transferring && !updating(server.id, server.clusterId);
  }

  function accepts(targetId: string) {
    if (!dragged || !canDrag(dragged)) return false;
    if ((dragged.clusterId ?? "") === targetId) return false;
    if (targetId !== "" && clusters.find((cluster) => cluster.id === targetId)?.origin === "imported") return false;
    return true;
  }

  function hover(event: DragEvent, targetId: string) {
    if (!accepts(targetId)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
    setOver(targetId);
  }

  function leave(event: DragEvent, targetId: string) {
    if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
    setOver((current) => (current === targetId ? null : current));
  }

  function drop(event: DragEvent, targetId: string) {
    event.preventDefault();
    const id = event.dataTransfer.getData(serverDrag) || dragId;
    setDragId(null);
    setOver(null);
    const server = servers.find((row) => row.id === id);
    if (!server || !canDrag(server) || (server.clusterId ?? "") === targetId) return;
    if (targetId !== "" && clusters.find((cluster) => cluster.id === targetId)?.origin === "imported") return;
    const name = targetId === "" ? text("server.moveAlone") : clusters.find((cluster) => cluster.id === targetId)?.name ?? "";
    setPending({ server, clusterId: targetId, name });
  }

  function startDrag(event: DragEvent, server: Server) {
    event.dataTransfer.setData("text/plain", server.id);
    event.dataTransfer.setData(serverDrag, server.id);
    event.dataTransfer.effectAllowed = "move";
    setDragId(server.id);
  }

  function endDrag() {
    setDragId(null);
    setOver(null);
  }

  return (
    <>
      <PageTitle
        title={creatingServer ? (target ? text("server.newIn", { name: target.name }) : text("ui.nouveauServeur")) : creatingCluster ? text("cluster.new") : importing ? text("cluster.import") : text("nav.servers")}
        note={!creating && ready && servers.length > 0 ? pageNote(servers) : undefined}
        phoneAction={!creating && ready && clusters.length > 0 && canDeploy ? { icon: Plus, label: clusters.length === 1 ? text("ui.nouveauServeur") : text("cluster.new"), onClick: () => (clusters.length === 1 ? setCreatingServer(clusters[0].id) : setCreatingCluster(true)) } : undefined}
        action={
          creating ? (
            <TextButton type="button" onClick={close}>
              {text("common.cancel")}
            </TextButton>
          ) : ready && clusters.length > 0 ? (
            <div className="flex flex-wrap items-center gap-4">
              <span className="hidden md:inline-flex">
                <ListModeButton mode={mode} onToggle={toggleMode} />
              </span>
              {canDeploy ? (
                <>
                  <TextButton type="button" onClick={() => setImporting(true)}>
                    {text("cluster.import")}
                  </TextButton>
                  {clusters.length === 1 ? (
                    <>
                      <Button type="button" variant="secondary" onClick={() => setCreatingCluster(true)} disabled={transferring}>
                        {text("cluster.new")}
                      </Button>
                      <Button type="button" onClick={() => setCreatingServer(clusters[0].id)} disabled={transferring}>
                        <Icon icon={Plus} />
                        {text("ui.nouveauServeur")}
                      </Button>
                    </>
                  ) : (
                    <Button type="button" onClick={() => setCreatingCluster(true)} disabled={transferring}>
                      <Icon icon={Plus} />
                      {text("cluster.new")}
                    </Button>
                  )}
                </>
              ) : null}
            </div>
          ) : null
        }
      />
      {ready && !creating ? <PilotNotice clusters={clusters} servers={servers} /> : null}
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {importing ? (
        <form onSubmit={importCluster} className="flex max-w-[720px] flex-col gap-6">
          <Field label={text("cluster.name")}>
            <Input value={name} onChange={(e) => setName(e.target.value)} required />
          </Field>
          <Field label={text("cluster.kubeconfig")}>
            <Code value={kubeconfig} onChange={setKubeconfig} language="yaml" label={text("cluster.kubeconfig")} placeholder="apiVersion: v1" minHeight="10rem" />
          </Field>
          <div>
            <Button type="submit" disabled={busy}>
              {busy ? <Spinner /> : <Icon icon={Plus} />}
              {text("cluster.import")}
            </Button>
          </div>
        </form>
      ) : creatingCluster ? (
        <form onSubmit={add} className="flex max-w-[720px] flex-col gap-6">
          <Field label={text("cluster.name")}>
            <Input value={name} onChange={(e) => setName(e.target.value)} required />
          </Field>
          <div>
            <Button type="submit" disabled={busy}>
              {busy ? <Spinner /> : <Icon icon={Plus} />}
              {text("ui.creerLeCluster")}
            </Button>
          </div>
        </form>
      ) : creatingServer === alone ? (
        <>
          <p className="mb-8 text-muted">{text("server.alone")}</p>
          <ServerForm clusterId="" />
        </>
      ) : creatingServer && target ? (
        <>
          <p className="mb-8 text-muted">{fresh === target.id ? text("cluster.firstServer", { name: target.name }) : target.name}</p>
          <ServerForm clusterId={target.id} />
        </>
      ) : !ready ? (
        <LoadingCards />
      ) : clusters.length === 0 ? (
        canDeploy ? (
          <div className="flex flex-col items-start gap-6">
            <EmptyList message={text("cluster.empty")} label={text("cluster.new")} onCreate={() => setCreatingCluster(true)} />
            <TextButton type="button" onClick={() => setImporting(true)}>
              {text("cluster.import")}
            </TextButton>
          </div>
        ) : (
          <p className="text-muted">{text("cluster.empty")}</p>
        )
      ) : (
        <>
          {canDeploy ? <p className="mb-4 hidden text-[13px] text-muted md:block">{text("server.moveDrag")}</p> : null}
          <ItemList mode={mode} columns={1}>
          {clusters.map((cluster) => (
            <ClusterCard
              key={cluster.id}
              cluster={cluster}
              servers={membersOf(servers, cluster.id)}
              mode={mode}
              onAdd={() => setCreatingServer(cluster.id)}
              pods={podsByServer}
              drag={cluster.origin === "imported" ? undefined : {
                id: dragId,
                hot: over === cluster.id,
                canDrag,
                onDragStart: startDrag,
                onDragEnd: endDrag,
                onDragOver: (event) => hover(event, cluster.id),
                onDragLeave: (event) => leave(event, cluster.id),
                onDrop: (event) => drop(event, cluster.id),
              }}
            />
          ))}
          </ItemList>
        </>
      )}
      {ready && !creating ? (
        <section
          className={`mt-10 rounded-md ${over === "" ? "bg-status" : ""}`}
          onDragOver={(event) => hover(event, "")}
          onDragLeave={(event) => leave(event, "")}
          onDrop={(event) => drop(event, "")}
        >
          <div className="mb-3 flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
            <div>
              <h2>{text("server.aloneList")}</h2>
              <p className="text-[13px] text-muted">{text("server.aloneListHint")}</p>
            </div>
            {canDeploy ? (
              <TextButton type="button" onClick={() => setCreatingServer(alone)}>
                <Icon icon={Plus} />
                {text("server.newAlone")}
              </TextButton>
            ) : null}
          </div>
          {lone.length === 0 ? (
            <p className="text-muted">{text("server.aloneEmpty")}</p>
          ) : (
            <ServerTable
              servers={lone}
              pods={podsByServer}
              drag={{ id: dragId, canDrag, onDragStart: startDrag, onDragEnd: endDrag }}
            />
          )}
        </section>
      ) : null}
      {pending ? (
        <MoveServerDialog
          server={pending.server}
          fixedDestination={{ id: pending.clusterId, name: pending.name }}
          onClose={() => setPending(null)}
          onMoved={(next) => {
            setServers((rows) => rows.map((row) => (row.id === next.id ? next : row)));
            setPending(null);
            if (next.status === "deploying") toast(text("server.moveStarted", { name: next.name }));
          }}
        />
      ) : null}
    </>
  );
}

// alone stands for a machine without a cluster in the creation state.
const alone = "alone";
const serverDrag = "application/x-myinfra-server";

function ClusterCard({
  cluster,
  servers,
  mode,
  onAdd,
  drag,
  pods,
}: {
  cluster: Cluster;
  servers: Server[];
  mode: ListMode;
  onAdd: () => void;
  pods: Map<string, string[]>;
  drag?: {
    id: string | null;
    hot: boolean;
    canDrag: (server: Server) => boolean;
    onDragStart: (event: DragEvent, server: Server) => void;
    onDragEnd: () => void;
    onDragOver: (event: DragEvent) => void;
    onDragLeave: (event: DragEvent) => void;
    onDrop: (event: DragEvent) => void;
  };
}) {
  const navigate = useNavigate();
  const removal = removalOf(useDestructions(), "cluster", cluster.id);
  const { data: usageData, error: usageError } = usePoll<ClusterUsage>(cluster.origin === "imported" ? null : `/api/clusters/${cluster.id}/usage`, 10000);
  const usage = usageData ?? (usageError ? null : undefined);
  const [confirm, setConfirm] = useState(false);
  const [apps, setApps] = useState<Application[] | null>(null);
  const [dialogError, setDialogError] = useState("");
  const [updating, setUpdating] = useState(false);
  const { view: updatesView } = useUpdates();
  const nodeUpdates = (updatesView?.updates ?? []).filter((item) => item.kind === "server" && servers.some((server) => server.id === item.id));

  useEffect(() => {
    if (!confirm) return;
    let cancel = false;
    api<Application[]>("/api/applications")
      .then((rows) => {
        if (cancel) return;
        const ids = new Set(servers.map((server) => server.id));
        setApps(rows.filter((app) => app.clusterId === cluster.id || (app.serverId ? ids.has(app.serverId) : false)));
      })
      .catch((err) => {
        if (!cancel) setDialogError(err instanceof Error ? err.message : text("common.error"));
      });
    return () => {
      cancel = true;
    };
  }, [confirm, cluster.id, servers]);

  const hostsSelf = apps?.some((app) => app.guarded || app.id === "myinfra") ?? false;
  const confirmText = hostsSelf ? text("destroy.phrase") : cluster.name;

  async function destroy() {
    if (!apps) throw new Error(text("cluster.contents"));
    const job = await api<Destruction>(`/api/clusters/${cluster.id}`, { method: "DELETE", body: JSON.stringify({ name: confirmText }) });
    navigate(`/destructions/${job.id}`);
  }

  return (
    <li
      className={`${entryClass(mode, "stack")} ${drag?.hot ? "bg-status" : ""}`}
      onDragOver={drag?.onDragOver}
      onDragLeave={drag?.onDragLeave}
      onDrop={drag?.onDrop}
    >
      <div className="flex flex-wrap items-start justify-between gap-4">
        {/* The name, its state and what makes it, on one line while it fits. */}
        <span className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1">
          <h2 className="text-[16px]">{cluster.name}</h2>
          {servers.length > 0 ? <StatusBadge status={servers.every((server) => liveServer(server)) ? "linked" : servers.some((server) => liveServer(server)) ? "preparing" : servers[0].status} /> : null}
          <span className="text-muted">{clusterLine(servers)}</span>
        </span>
        <span className="flex items-center gap-4">
          {cluster.origin === "imported" ? null : (
            <TextButton type="button" onClick={onAdd}>
              <Icon icon={Plus} />
              {text("cluster.addServer")}
            </TextButton>
          )}
          <ActionsMenu label={text("apps.actionsOf", { name: cluster.name })}>
            <MenuItem onClick={() => navigate(`/historique?cluster=${cluster.id}`)}>{text("nav.history")}</MenuItem>
            {cluster.origin === "imported" || nodeUpdates.length === 0 ? null : (
              <MenuItem onClick={() => setUpdating(true)}>{text("upd.cluster.update")}</MenuItem>
            )}
            {cluster.origin === "imported" ? null : (
              <MenuItem
                onClick={() => {
                  api<Template>("/api/templates/from-cluster", { method: "POST", body: JSON.stringify({ clusterId: cluster.id }) })
                    .then((created) => navigate(`/templates/${created.id}`))
                    .catch((err) => setDialogError(err instanceof Error ? err.message : text("common.error")));
                }}
              >
                {text("ui.creerUnTemplate")}
              </MenuItem>
            )}
            <MenuItem
              tone="danger"
              onClick={() => {
                setApps(null);
                setDialogError("");
                setConfirm(true);
              }}
            >
              {text("common.delete")}
            </MenuItem>
          </ActionsMenu>
        </span>
      </div>
      {!removal && servers.length > 0 && servers.every((server) => server.reach) ? (
        <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-bad px-4 py-3">
          <p className="text-bad">{servers.every((server) => server.reach === "gone") ? text("reach.clusterGone") : text("reach.clusterLost")}</p>
          {servers.every((server) => server.reach === "gone") ? (
            <Button
              type="button"
              variant="danger"
              onClick={() => {
                setApps(null);
                setDialogError("");
                setConfirm(true);
              }}
            >
              {text("reach.removeCluster")}
            </Button>
          ) : null}
        </div>
      ) : null}
      {!removal && servers.some((server) => server.reach) && !servers.every((server) => server.reach) ? (
        <p className="mt-4 rounded-md border border-bad px-4 py-3 text-bad">
          {text("reach.clusterSome", { count: String(servers.filter((server) => server.reach).length), total: String(servers.length) })}
        </p>
      ) : null}
      <RemovalNotice job={removal} />
      {nodeUpdates.length > 0 && cluster.origin !== "imported" ? (
        <p className="flex flex-wrap items-center gap-2 text-muted">
          <UpdateBadge item={nodeUpdates.find(urgent) ?? nodeUpdates[0]} />
          {text("upd.cluster.nodes", { count: nodeUpdates.length })}
        </p>
      ) : null}
      {updating ? (
        <UpdateDialog
          target={{ kind: "cluster", id: cluster.id, name: cluster.name, k3s: nodeUpdates.some((item) => item.detail.k3sNext) }}
          onClose={() => setUpdating(false)}
          onStarted={() => navigate("/mises-a-jour")}
        />
      ) : null}
      {dialogError && !confirm ? <ErrorText>{dialogError}</ErrorText> : null}
      {cluster.origin === "imported" ? null : (
        // On a card, the measures make a band of their own between two lines.
        <div className={`flex flex-col gap-4 md:flex-row md:items-end md:gap-6 ${mode === "cards" ? "-mx-4 border-t border-line px-4 pt-4" : ""}`}>
          <div className="min-w-0 flex-1">
            <UsageMeters sample={usage === undefined ? null : usage ?? { measured: false, cpu: zero, memory: zero, disk: zero }} />
          </div>
          {servers.length > 0 ? <p className="text-[13px] text-muted md:max-w-[240px] md:text-right">{clusterBill(servers)}</p> : null}
        </div>
      )}
      {cluster.origin === "imported" ? (
        <ArgoList id={cluster.id} />
      ) : servers.length > 0 ? (
        <ServerTable
          flush={mode === "cards"}
          servers={servers}
          usage={usage ?? undefined}
          usageLoading={usage === undefined}
          pods={pods}
          drag={drag}
        />
      ) : (
        <p className="text-muted">{text("cluster.noServer")}</p>
      )}
      {confirm ? (
        <ConfirmDialog
          title={text("server.removeNamed", { name: cluster.name })}
          confirmLabel={text("common.delete")}
          danger
          phrase={confirmText}
          phraseLabel={hostsSelf ? text("field.confirm") : text("cluster.name")}
          onConfirm={destroy}
          onClose={() => setConfirm(false)}
        >
          {dialogError ? (
            <div className="mb-6">
              <ErrorText>{dialogError}</ErrorText>
            </div>
          ) : null}
          {cluster.origin === "imported" ? <p className="mb-6">{text("cluster.importedStays")}</p> : null}
          {apps === null && !dialogError ? <LoadingLines lines={3} label={text("cluster.contents")} /> : null}
          {apps && servers.length > 0 ? (
            <div className="mb-6">
              <p className="mb-2 text-ink">{text("cluster.servers")}</p>
              <ul>
                {servers.map((server) => (
                  <li key={server.id} className="border-t border-line py-2">
                    {server.name}
                    {server.address ? ` · ${server.address}` : ""}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {apps && apps.length > 0 ? (
            <div className="mb-6">
              <p className="mb-2 text-ink">{text("nav.apps")}</p>
              <ul>
                {apps.map((app) => (
                  <li key={app.id} className="border-t border-line py-2">
                    {app.name}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          <p>
            {hostsSelf ? text("cluster.selfWarn", { phrase: text("destroy.phrase") }) : text("ui.tapezLeNomDuClusterPour")}
            {servers.some((server) => server.providerId) ? ` ${text("cluster.machinesGo")}` : ""}
            {apps && apps.length > 0 ? ` ${text("cluster.domainsGo")}` : ""}
          </p>
        </ConfirmDialog>
      ) : null}
    </li>
  );
}

function ArgoList({ id }: { id: string }) {
  const [apps, setApps] = useState<ArgoApp[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api<ArgoApp[]>(`/api/clusters/${id}/argo`)
      .then(setApps)
      .catch((err) => setError(err.message));
  }, [id]);

  if (error) return <ErrorText>{error}</ErrorText>;
  if (!apps) return <LoadingLines label={text("cluster.readingArgo")} />;
  if (apps.length === 0) return <p className="text-muted">{text("cluster.noArgo")}</p>;
  return (
    <ul className="flex flex-col gap-2">
      {apps.map((app) => (
        <li key={`${app.namespace}/${app.name}`} className="text-muted">
          {app.name} · {app.sync || "inconnu"} · {app.health || "inconnu"}
        </li>
      ))}
    </ul>
  );
}

function clusterBill(servers: Server[]) {
  const sums = new Map<string, { amount: number; hourly: number; theoretical: boolean; currency: string }>();
  let priced = 0;
  for (const server of servers) {
    if (!server.cost) continue;
    priced += 1;
    const currency = server.cost.currency || "EUR";
    const row = sums.get(currency) ?? { amount: 0, hourly: 0, theoretical: false, currency };
    row.amount += Number(server.cost.amount) || 0;
    row.hourly += hourlyRate(server);
    row.theoretical = row.theoretical || server.cost.theoretical;
    sums.set(currency, row);
  }
  if (priced === 0) return text("bill.none");
  const label = [...sums.values()].map((row) => billText({ amount: String(row.amount), currency: row.currency, theoretical: row.theoretical }, true, row.hourly)).join(" · ");
  if (priced < servers.length) return `${label} · ${text("bill.partial")}`;
  return label;
}

function membersOf(servers: Server[], clusterId: string) {
  const members = servers.filter((server) => server.clusterId === clusterId);
  if (members.some((server) => server.createdAt)) {
    return members.sort((a, b) => (a.createdAt ?? "").localeCompare(b.createdAt ?? ""));
  }
  return members.slice().reverse();
}

const zero = { used: 0, total: 0 };

function pageNote(servers: Server[]) {
  const live = servers.filter((server) => liveServer(server)).length;
  const totals = new Map<string, number>();
  for (const server of servers) {
    const hourly = hourlyRate(server);
    if (!hourly) continue;
    const unit = server.plan?.currency === "USD" || server.cost?.currency === "USD" ? "$" : "€";
    totals.set(unit, (totals.get(unit) ?? 0) + hourly);
  }
  const cost = [...totals.entries()].map(([unit, hourly]) => text("overview.perMonth", { amount: `${money(hourly * 730)} ${unit}` }));
  return [text("server.inService", { count: live }), ...cost].join(" · ");
}

function clusterLine(servers: Server[]) {
  const hourly = servers.reduce((sum, server) => sum + hourlyRate(server), 0);
  const unit = servers.some((server) => server.plan?.currency === "USD") ? "$" : "€";
  const count = servers.length === 1 ? text("server.count1") : text("server.countN", { count: servers.length });
  return [text("cluster.kind"), count, hourly ? text("overview.perHour", { amount: `${money(hourly)} ${unit}` }) : ""].filter(Boolean).join(" · ");
}

// PilotNotice warns when a cluster has a single server that drives it:
// should it fall, the apps keep running but nothing deploys any more.
function PilotNotice({ clusters, servers }: { clusters: Cluster[]; servers: Server[] }) {
  const lonely = clusters.filter((cluster) => {
    if (cluster.origin === "imported") return false;
    const members = servers.filter((server) => server.clusterId === cluster.id);
    return members.length > 1 && members.filter((server) => server.controlPlane).length === 1;
  });
  if (lonely.length === 0) return null;
  return (
    <section aria-label={text("overview.attention")} className="mb-8 flex flex-col gap-1 rounded-md border border-line bg-surface px-4 py-3">
      {lonely.map((cluster) => {
        const pilot = servers.find((server) => server.clusterId === cluster.id && server.controlPlane);
        return (
          <p key={cluster.id} className="flex items-start gap-2">
            <Icon icon={AlertTriangle} className="mt-1 shrink-0 text-warn" />
            <span>
              <span className="text-warn">{text("overview.attention")} · </span>
              {text("cluster.onePilot", { server: pilot?.name ?? "", cluster: cluster.name })}
              <span className="block text-[13px] text-muted">{text("cluster.onePilotHint")}</span>
            </span>
          </p>
        );
      })}
    </section>
  );
}

type DragProps = {
  id: string | null;
  canDrag: (server: Server) => boolean;
  onDragStart: (event: DragEvent, server: Server) => void;
  onDragEnd: () => void;
};

// ServerTable lists servers as rows: name and address, place, size, load and
// what runs there. A row can be dragged to another cluster.
// ServerTable lists servers with their place, size and load. flush lays it
// in the card that holds it, from edge to edge under a line.
function ServerTable({
  servers,
  usage,
  usageLoading,
  pods,
  drag,
  flush = false,
}: {
  flush?: boolean;
  servers: Server[];
  usage?: ClusterUsage;
  usageLoading?: boolean;
  pods: Map<string, string[]>;
  drag?: DragProps;
}) {
  const grid = "md:grid md:grid-cols-[minmax(0,1.4fr)_minmax(0,0.9fr)_minmax(0,1.1fr)_minmax(0,1.2fr)_minmax(0,0.8fr)] md:items-center md:gap-4";
  return (
    <div className={flush ? "-mx-4 -mb-4 mt-1 overflow-hidden rounded-b-md border-t border-line" : "overflow-hidden rounded-md border border-line bg-surface"}>
      <div aria-hidden className={`hidden border-b border-line px-4 py-2 text-[12px] text-muted ${grid}`}>
        <span>{text("server.col.name")}</span>
        <span>{text("server.col.place")}</span>
        <span>{text("server.col.size")}</span>
        <span>{text("server.col.load")}</span>
        <span>{text("server.col.pods")}</span>
      </div>
      <ul>
        {servers.map((server) => (
          <ServerLine
            key={server.id}
            server={server}
            grid={grid}
            usage={usage?.servers.find((row) => row.id === server.id)}
            usageLoading={usageLoading}
            apps={pods.get(server.id) ?? []}
            drag={drag}
          />
        ))}
      </ul>
    </div>
  );
}

function ServerLine({
  server,
  grid,
  usage,
  usageLoading,
  apps,
  drag,
}: {
  server: Server;
  grid: string;
  usage?: ServerUsage;
  usageLoading?: boolean;
  apps: string[];
  drag?: DragProps;
}) {
  const removal = removalOf(useDestructions(), "server", server.id);
  const update = useUpdate("server", server.id);
  const dragged = useRef(false);
  const draggable = drag?.canDrag(server) ?? false;
  const place = placeLabel(server.plan?.location) || placeLabel(server.place);
  const distinct = [...new Set(apps.filter(Boolean))];
  return (
    <li
      className={`card-link flex flex-col gap-1 border-t border-line px-4 py-3 first:border-t-0 ${grid} ${draggable ? "cursor-grab active:cursor-grabbing" : ""} ${drag?.id === server.id ? "opacity-50" : ""}`}
      draggable={draggable || undefined}
      onDragStart={(event) => {
        dragged.current = true;
        drag?.onDragStart(event, server);
      }}
      onDragEnd={() => {
        drag?.onDragEnd();
        window.setTimeout(() => {
          dragged.current = false;
        }, 0);
      }}
    >
      <span className="flex min-w-0 flex-col">
        <span className="flex flex-wrap items-center gap-2">
          <HealthDot status={serverState(server)} />
          <Link
            to={`/serveurs/${server.id}`}
            onClick={(event) => {
              if (dragged.current) event.preventDefault();
            }}
            className={`font-medium text-ink ${focus}`}
          >
            {server.name}
          </Link>
          {server.controlPlane ? <span className="rounded border border-line px-1.5 text-[12px] text-muted">{text("server.pilot")}</span> : null}
          {update ? <UpdateBadge item={update} /> : null}
        </span>
        {server.address ? <span className="pl-4 font-mono text-[12px] text-muted">{server.address}</span> : null}
        {removal ? <RemovalNotice job={removal} /> : null}
      </span>
      <span className="text-[13px] text-muted md:text-[15px] md:text-ink">{place || "—"}</span>
      <span className="flex flex-col text-[13px]">
        {server.plan ? (
          <>
            <span className="hidden md:inline">{server.plan.name}</span>
            <span className="text-muted">{machineLine("", server.plan.vcpu, server.plan.memoryMb, server.plan.gpus, server.plan.gpu).replace(/^ · /, "")}</span>
          </>
        ) : (
          <span className="text-muted">{statusLabel(serverState(server)).text}</span>
        )}
      </span>
      <span className="flex flex-col gap-1">
        {usageLoading ? <Skeleton className="h-3 w-28" /> : usage?.measured ? (
          <>
            <MiniBar label={text("common.cpu")} gauge={usage.cpu} />
            <MiniBar label={text("common.ram")} gauge={usage.memory} />
          </>
        ) : usage ? (
          <span className="text-[13px] text-faint">{text("common.noMeasure")}</span>
        ) : null}
      </span>
      <span className="truncate text-[13px] text-muted" title={distinct.join(", ")}>
        {apps.length > 0 ? (apps.length === 1 ? `1 pod · ${distinct[0]}` : `${apps.length} pods`) : ""}
      </span>
    </li>
  );
}

function MiniBar({ label, gauge }: { label: string; gauge: Gauge }) {
  const ratio = gauge.total > 0 ? Math.max(0, Math.min(1, gauge.used / gauge.total)) : 0;
  return (
    <span className="flex items-center gap-2 text-[12px] text-muted" title={`${label} ${Math.round(ratio * 100)} %`}>
      <span className="w-8">{label}</span>
      <span className="h-1.5 w-20 overflow-hidden rounded-full bg-line" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(ratio * 100)}>
        <span className={`block h-full rounded-full ${ratio >= 0.9 ? "bg-bad" : ratio >= 0.75 ? "bg-warn" : "bg-ink"}`} style={{ width: `${ratio * 100}%` }} />
      </span>
      <span>{Math.round(ratio * 100)} %</span>
    </span>
  );
}
