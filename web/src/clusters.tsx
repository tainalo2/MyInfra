import { FormEvent, useEffect, useState } from "react";
import { Network, Plus, Server as ServerIcon } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { api, peek, type Application, type ArgoApp, type Cluster, type ClusterUsage, type Destruction, type Server, type Template } from "./api";
import { text } from "./i18n";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { usePoll } from "./poll";
import { usageText } from "./measure";
import { RemovalNotice } from "./destructions";
import { ServerCard, ServerForm, billText } from "./servers";
import { UpdateBadge, UpdateDialog, urgent, useUpdates } from "./updates";
import { hourlyRate } from "./measure";
import { ActionsMenu, Button, EmptyList, ErrorText, Field, Icon, Input, ItemList, ListActions, LoadingCards, LoadingLines, MenuItem, PageTitle, Skeleton, TextArea, TextButton, entryClass, removalOf, useDestructions, useListMode, type ListMode } from "./ui";

export function Clusters() {
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
  const toast = useToast();

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

  return (
    <>
      <PageTitle
        title={text("nav.clusters")}
        icon={Network}
        action={
          creating ? (
            <TextButton type="button" onClick={close}>
              {text("common.cancel")}
            </TextButton>
          ) : ready && clusters.length > 0 ? (
            <div className="flex flex-wrap items-center gap-4">
              <TextButton type="button" onClick={() => setImporting(true)}>
                {text("cluster.import")}
              </TextButton>
              <ListActions mode={mode} onToggle={toggleMode} label={text("cluster.new")} onCreate={() => setCreatingCluster(true)} />
            </div>
          ) : null
        }
      />
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
            <TextArea value={kubeconfig} onChange={(e) => setKubeconfig(e.target.value)} required />
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
        <div className="flex flex-col items-start gap-6">
          <EmptyList message={text("cluster.empty")} label={text("cluster.new")} onCreate={() => setCreatingCluster(true)} />
          <TextButton type="button" onClick={() => setImporting(true)}>
            {text("cluster.import")}
          </TextButton>
        </div>
      ) : (
        <ItemList mode={mode} columns={1}>
          {clusters.map((cluster) => (
            <ClusterCard
              key={cluster.id}
              cluster={cluster}
              servers={membersOf(servers, cluster.id)}
              mode={mode}
              onAdd={() => setCreatingServer(cluster.id)}
            />
          ))}
        </ItemList>
      )}
      {ready && !creating ? (
        <section className="mt-12">
          <div className="mb-6 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
            <h2 className="flex items-center gap-3 text-lg">
              <Icon icon={ServerIcon} className="text-muted" />
              {text("server.aloneList")}
            </h2>
            <TextButton type="button" onClick={() => setCreatingServer(alone)}>
              <Icon icon={Plus} />
              {text("server.newAlone")}
            </TextButton>
          </div>
          {lone.length === 0 ? (
            <p className="text-muted">{text("server.aloneEmpty")}</p>
          ) : (
            <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {lone.map((server) => (
                <li key={server.id} className="min-w-0">
                  <ServerCard compact server={server} />
                </li>
              ))}
            </ul>
          )}
        </section>
      ) : null}
    </>
  );
}

// alone stands for a machine without a cluster in the creation state.
const alone = "alone";

function ClusterCard({
  cluster,
  servers,
  mode,
  onAdd,
}: {
  cluster: Cluster;
  servers: Server[];
  mode: ListMode;
  onAdd: () => void;
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
    <li className={entryClass(mode, "stack")}>
      <div className="flex flex-wrap items-center justify-between gap-4">
        <span className="flex items-center gap-3">
          <Icon icon={Network} className="text-muted" />
          {cluster.name}
        </span>
        <span className="flex items-center gap-4">
          {cluster.origin === "imported" ? null : (
            <Button type="button" onClick={onAdd}>
              <Icon icon={Plus} />
              {text("ui.nouveauServeur")}
            </Button>
          )}
          <ActionsMenu label={text("apps.more")}>
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
        usage === undefined ? (
          <div role="status" aria-label={text("common.measures")}>
            <Skeleton className="h-4 w-72" />
          </div>
        ) : (
          <p className="text-muted">{usage?.measured ? usageText(usage.cpu, usage.memory, usage.disk) : text("common.noMeasure")}</p>
        )
      )}
      {servers.length > 0 ? <p className="text-muted">{clusterBill(servers)}</p> : null}
      {servers.length > 0 ? (
        <p>
          <Link to={`/historique?cluster=${cluster.id}`} className="text-muted underline">{text("nav.history")}</Link>
        </p>
      ) : null}
      {cluster.origin === "imported" ? (
        <ArgoList id={cluster.id} />
      ) : servers.length > 0 ? (
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {servers.map((server) => (
            <li key={server.id} className="min-w-0">
              <ServerCard
                compact
                server={server}
                controlPlane={server.controlPlane}
                usageLoading={usage === undefined}
                usage={usage?.servers.find((row) => row.id === server.id)}
              />
            </li>
          ))}
        </ul>
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
