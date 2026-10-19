import { FormEvent, useEffect, useState } from "react";
import { Network, Plus, Server as ServerIcon } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { api, peek, type Application, type ArgoApp, type Cluster, type ClusterUsage, type Destruction, type Server, type Template } from "./api";
import { text } from "./i18n";
import { usageText } from "./measure";
import { RemovalNotice } from "./destructions";
import { ServerCard, ServerForm, billText } from "./servers";
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
    setCreatingCluster(false);
    setImporting(false);
    setCreatingServer("");
    setName("");
    setKubeconfig("");
    setError("");
  }

  async function add(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await api("/api/clusters", { method: "POST", body: JSON.stringify({ name }) });
      setName("");
      setCreatingCluster(false);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  async function importCluster(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await api("/api/clusters/import", { method: "POST", body: JSON.stringify({ name, kubeconfig }) });
      close();
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
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
            <div className="flex items-center gap-4">
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
          <Field label="Kubeconfig">
            <TextArea value={kubeconfig} onChange={(e) => setKubeconfig(e.target.value)} required />
          </Field>
          <div>
            <Button type="submit">
              <Icon icon={Plus} />
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
            <Button type="submit">
              <Icon icon={Plus} />
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
          <p className="mb-8 text-muted">{target.name}</p>
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
          <div className="mb-6 flex items-center justify-between gap-4">
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
  const [usage, setUsage] = useState<ClusterUsage | null | undefined>(undefined);
  const [confirm, setConfirm] = useState(false);
  const [typed, setTyped] = useState("");
  const [removing, setRemoving] = useState(false);
  const [apps, setApps] = useState<Application[] | null>(null);
  const [dialogError, setDialogError] = useState("");

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

  useEffect(() => {
    if (!confirm) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape" && !removing) setConfirm(false);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [confirm, removing]);

  const hostsSelf = apps?.some((app) => app.guarded || app.id === "myinfra") ?? false;
  const confirmText = hostsSelf ? text("destroy.phrase") : cluster.name;

  async function destroy(event: FormEvent) {
    event.preventDefault();
    if (typed !== confirmText || !apps) return;
    setRemoving(true);
    setDialogError("");
    try {
      const job = await api<Destruction>(`/api/clusters/${cluster.id}`, { method: "DELETE", body: JSON.stringify({ name: typed }) });
      navigate(`/destructions/${job.id}`);
    } catch (err) {
      setDialogError(err instanceof Error ? err.message : text("common.error"));
      setRemoving(false);
    }
  }

  useEffect(() => {
    if (cluster.origin === "imported") return;
    let stop = false;
    const load = () => {
      api<ClusterUsage>(`/api/clusters/${cluster.id}/usage`)
        .then((next) => {
          if (!stop) setUsage(next);
        })
        .catch(() => {
          if (!stop) setUsage(null);
        });
    };
    load();
    const timer = window.setInterval(load, 10000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [cluster.id, cluster.origin]);

  return (
    <li className={entryClass(mode, "stack")}>
      <div className="flex items-center justify-between gap-4">
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
          <ActionsMenu label={`Actions de ${cluster.name}`}>
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
              onClick={() => {
                setTyped("");
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
      <RemovalNotice job={removal} />
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
        <div className="fixed inset-0 z-30 flex items-center justify-center bg-ink/40 px-6" role="dialog" aria-modal="true" aria-labelledby="delete-cluster-title">
          <form onSubmit={destroy} className="flex max-h-[80vh] w-full max-w-md flex-col rounded-md border border-line bg-surface p-6">
            <h2 id="delete-cluster-title" className="mb-3 text-lg">Détruire {cluster.name}</h2>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {dialogError ? (
                <div className="mb-6">
                  <ErrorText>{dialogError}</ErrorText>
                </div>
              ) : null}
              {cluster.origin === "imported" ? <p className="mb-6 text-muted">{text("cluster.importedStays")}</p> : null}
              {apps === null && !dialogError ? <LoadingLines lines={3} label={text("cluster.contents")} /> : null}
              {apps && servers.length > 0 ? (
                <div className="mb-6">
                  <p className="mb-2">{text("cluster.servers")}</p>
                  <ul>
                    {servers.map((server) => (
                      <li key={server.id} className="border-t border-line py-2 text-muted">
                        {server.name}
                        {server.address ? ` · ${server.address}` : ""}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
              {apps && apps.length > 0 ? (
                <div>
                  <p className="mb-2">{text("nav.apps")}</p>
                  <ul>
                    {apps.map((app) => (
                      <li key={app.id} className="border-t border-line py-2 text-muted">
                        {app.name}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </div>
            <div className="mt-6">
              <p className="mb-6 text-muted">
                {hostsSelf
                  ? text("cluster.selfWarn", { phrase: text("destroy.phrase") })
                  : text("ui.tapezLeNomDuClusterPour")}
                {servers.some((server) => server.providerId) ? " Les machines chez le provider sont détruites avec lui." : ""}
                {apps && apps.length > 0 ? " Les noms de domaine de ces applications sont retirés." : ""}
              </p>
              <Field label={hostsSelf ? "Confirmation" : text("cluster.name")}>
                <Input value={typed} autoFocus onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
              </Field>
              <div className="mt-6 flex items-center gap-4">
                <Button type="submit" disabled={typed !== confirmText || removing || apps === null}>
                  {removing ? <span className="size-4 animate-spin rounded-full border border-current border-t-transparent" /> : null}
                  Détruire
                </Button>
                <TextButton type="button" onClick={() => setConfirm(false)} disabled={removing}>
                  {text("common.cancel")}
                </TextButton>
              </div>
            </div>
          </form>
        </div>
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
