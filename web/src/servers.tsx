import { FormEvent, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Copy, KeyRound, Plus, RotateCcw, Server as ServerIcon, Trash2 } from "lucide-react";
import { api, peek, type Catalog, type Cost, type DeployStep, type Destruction, type Offer, type Project, type Secret, type Server, type ServerRuntime, type ServerUsage } from "./api";
import { CancelDeployment } from "./deployments";
import { UsageMeters } from "./meters";
import { hourlyRate, money, podCpu, size, usageText } from "./measure";
import { ProviderLogo } from "./logos";
import { OfferFields, cheapestOffer } from "./offers";
import { placeLabel } from "./places";
import { ProviderSelect } from "./provider-select";
import { TofuFiles } from "./tofu";
import { EventList } from "./world";
import { currentLocale, text, knownMessage } from "./i18n";
import {
  Button,
  ErrorText,
  Field,
  Fold,
  Icon,
  IconButton,
  Input,
  LoadingLines,
  PageTitle,
  Select,
  Skeleton,
  TextButton,
  podFailed,
  podHealth,
  removalOf,
  statusLabel,
  useDestructions,
} from "./ui";
import { RemovalNotice } from "./destructions";
import { PodActions } from "./pods";
import { ToolInstall } from "./tools";

export function ServerForm({ clusterId }: { clusterId: string }) {
  const navigate = useNavigate();
  const [keys, setKeys] = useState<Secret[]>([]);
  const [providers, setProviders] = useState<Secret[]>([]);
  const [providerId, setProviderId] = useState("");
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsReady, setProjectsReady] = useState(false);
  const [projectId, setProjectId] = useState("");
  const [offersLoading, setOffersLoading] = useState(false);
  const [location, setLocation] = useState("");
  const [offerId, setOfferId] = useState("");
  const [period, setPeriod] = useState("hour");
  const [storageId, setStorageId] = useState("");
  const [storageGb, setStorageGb] = useState("");
  const [name, setName] = useState("");
  const [controlPlane, setControlPlane] = useState(false);
  const [roleLocked, setRoleLocked] = useState(true);
  const [planes, setPlanes] = useState(0);
  const alone = clusterId === "";
  const [keyIds, setKeyIds] = useState<string[]>([""]);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    api<Server[]>("/api/servers")
      .then((rows) => {
        const count = clusterId ? rows.filter((row) => row.clusterId === clusterId && row.controlPlane).length : 0;
        setPlanes(count);
        setControlPlane(clusterId !== "" && count === 0);
        setRoleLocked(count === 0);
      })
      .catch(() => {
        setControlPlane(true);
        setRoleLocked(true);
      });
  }, [clusterId]);

  useEffect(() => {
    api<Secret[]>("/api/secrets")
      .then((secrets) => {
        setKeys(secrets.filter((secret) => secret.kind === "ssh_key"));
        setProviders(secrets.filter((secret) => secret.kind === "provider_token"));
      })
      .catch((err) => setError(err.message));
  }, []);

  useEffect(() => {
    if (!providerId || providerId === "manual") {
      setProjects([]);
      setProjectId("");
      setProjectsReady(true);
      return;
    }
    let cancel = false;
    setProjectsReady(false);
    api<Project[]>(`/api/providers/${providerId}/projects`)
      .then((rows) => {
        if (cancel) return;
        setProjects(rows);
        setProjectId(rows[0]?.id ?? "");
      })
      .catch((err) => {
        if (!cancel) {
          setProjects([]);
          setProjectId("");
          setError(err instanceof Error ? err.message : text("common.error"));
        }
      })
      .finally(() => {
        if (!cancel) setProjectsReady(true);
      });
    return () => {
      cancel = true;
    };
  }, [providerId]);

  useEffect(() => {
    if (!providerId || providerId === "manual") {
      setCatalog(null);
      setOffersLoading(false);
      return;
    }
    if (!projectsReady || (projects.length > 0 && !projectId)) {
      setOffersLoading(true);
      return;
    }
    let cancel = false;
    setOffersLoading(true);
    const params = new URLSearchParams();
    if (location) params.set("location", location);
    if (projectId) params.set("project", projectId);
    const query = params.toString() ? `?${params}` : "";
    api<Catalog>(`/api/providers/${providerId}/offers${query}`)
      .then((next) => {
        if (cancel) {
          return;
        }
        setCatalog(next);
        setError("");
        if (next.location && next.location !== location) {
          setLocation(next.location);
        }
        setOfferId((current) => (next.offers.some((offer) => offer.id === current) ? current : cheapestOffer(next.offers)?.id ?? ""));
        setPeriod((current) => (next.periods.includes(current) ? current : next.periods[0] ?? "hour"));
      })
      .catch((err) => {
        if (!cancel) {
          setCatalog(null);
          setError(err instanceof Error ? err.message : text("common.error"));
        }
      })
      .finally(() => {
        if (!cancel) setOffersLoading(false);
      });
    return () => {
      cancel = true;
    };
  }, [providerId, location, projectId, projectsReady, projects.length]);

  async function addServer(event: FormEvent) {
    event.preventDefault();
    setError("");
    const chosen = keyIds.filter(Boolean);
    if (!providerId || chosen.length === 0 || chosen.length !== keyIds.length) {
      setError(text("ui.choisissezUnProviderEtAuMoins"));
      return;
    }
    setSubmitting(true);
    try {
      const storage = chosenStorage(offer, storageId, storageGb);
      const created = await api<Server>("/api/servers", {
        method: "POST",
        body: JSON.stringify({
          name,
          keyIds: chosen,
          providerId,
          clusterId,
          controlPlane: alone ? false : controlPlane,
          offerId: providerId === "manual" ? "" : offerId,
          location,
          projectId: providerId === "manual" ? "" : projectId,
          period,
          storageId: storage.id,
          storageGb: storage.gb,
        }),
      });
      navigate(`/serveurs/${created.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setSubmitting(false);
    }
  }

  const offer = catalog?.offers.find((item) => item.id === offerId);
  const listsLoading = providerId !== "" && providerId !== "manual" && (offersLoading || !projectsReady);
  const ready = providerId === "manual" || (Boolean(offer) && !listsLoading);
  const chosenKeys = keyIds.filter(Boolean);
  const canAddKey = keys.length > keyIds.length && chosenKeys.length === keyIds.length;

  return (
    <>
      <form onSubmit={addServer} className="flex max-w-[720px] flex-col gap-6">
        {alone ? <p className="text-muted">{text("server.aloneHint")}</p> : null}
        <ProviderSelect
          label={text("ui.provider")}
          aside={<Link to="/providers">{text("server.connect")}</Link>}
          value={providerId}
          onChange={(value) => {
            setProviderId(value);
            setLocation("");
            setProjectId("");
            setProjects([]);
            setProjectsReady(false);
            setOfferId("");
            setCatalog(null);
            setStorageId("");
            setStorageGb("");
          }}
          options={[
            { value: "manual", label: "Manual enrollment", logo: "manual" },
            ...providers.map((provider) => ({
              value: provider.id,
              label: provider.name,
              logo: provider.provider ?? "",
            })),
          ]}
        />
        {providerId !== "manual" && projectsReady && projects.length > 0 ? (
          <Field label={text("provider.project")}>
            <Select
              value={projectId}
              onChange={(e) => {
                setProjectId(e.target.value);
                setOfferId("");
              }}
            >
              {projects.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </Select>
          </Field>
        ) : null}
        {catalog && catalog.locations.length > 1 ? (
          <Field label={text("field.place")}>
            <Select value={location || catalog.location} onChange={(e) => setLocation(e.target.value)}>
              {catalog.locations.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.label}
                </option>
              ))}
            </Select>
          </Field>
        ) : null}
        {!listsLoading && catalog && catalog.offers.length === 0 ? <p className="text-muted">{text("ui.aucuneMachineNEstProposeeA")}</p> : null}
        {providerId !== "" ? (
          <>
            {/* Only what depends on the place and the offer waits: the rest of the form stays. */}
            {listsLoading ? (
              <LoadingLines lines={3} label={text("transfer.offers")} />
            ) : offer && catalog ? (
              <>
                <OfferFields offers={catalog.offers} value={offer.id} currency={catalog.currency} onChange={setOfferId} />
                <p className="text-muted">
                  {offer.hourly ? price(offer.hourly, true, catalog.currency) : null}
                  {offer.hourly && offer.monthly ? " · " : null}
                  {offer.monthly ? price(offer.monthly, false, catalog.currency) : null}
                </p>
                <div className="flex gap-2">
                  {offer.hourly ? (
                    <button
                      type="button"
                      aria-pressed={period === "hour"}
                      onClick={() => setPeriod("hour")}
                      className={`rounded-md border px-3 py-2 ${period === "hour" ? "border-ink bg-status" : "border-line"}`}
                    >
                      {text("ui.heure")}
                    </button>
                  ) : null}
                  {offer.monthly ? (
                    <button
                      type="button"
                      aria-pressed={period === "month"}
                      onClick={() => setPeriod("month")}
                      className={`rounded-md border px-3 py-2 ${period === "month" ? "border-ink bg-status" : "border-line"}`}
                    >
                      {text("ui.mois")}
                    </button>
                  ) : null}
                </div>
                <StorageFields
                  offer={offer}
                  period={period}
                  currency={catalog.currency}
                  storageId={storageId}
                  storageGb={storageGb}
                  onId={setStorageId}
                  onGb={setStorageGb}
                />
                {catalog.note ? <p className="text-faint">{catalog.note}</p> : null}
              </>
            ) : null}
            <Field label={text("server.name")}>
              <Input value={name} onChange={(e) => setName(e.target.value)} required />
            </Field>
            {alone ? null : (
              <div>
                <label className="flex items-center gap-2">
                  <input type="checkbox" checked={controlPlane} disabled={roleLocked} onChange={(e) => setControlPlane(e.target.checked)} />
                  Control plane
                </label>
                <p className="mt-2 text-faint">{roleLocked ? text("server.firstPlane") : text("server.manyPlanes")}</p>
                {controlPlane && (planes + 1) % 2 === 0 ? <p className="mt-2 text-warn">{text("server.evenPlanes")}</p> : null}
              </div>
            )}
            {keys.length > 0 ? (
              <div className="flex flex-col gap-2">
                <span className="mb-2 text-muted">{text("nav.ssh")}</span>
                {keyIds.map((keyId, index) => {
                  const taken = new Set(keyIds.filter((id, i) => i !== index && id));
                  return (
                    <div key={index} className="flex items-center gap-3">
                      <Select
                        value={keyId}
                        required
                        onChange={(e) =>
                          setKeyIds((current) => current.map((id, i) => (i === index ? e.target.value : id)))
                        }
                      >
                        <option value="">{text("server.chooseKey")}</option>
                        {keys
                          .filter((key) => !taken.has(key.id))
                          .map((key) => (
                            <option key={key.id} value={key.id}>
                              {key.name}
                            </option>
                          ))}
                      </Select>
                      {index > 0 ? (
                        <IconButton
                          type="button"
                          icon={Trash2}
                          label={text("common.remove")}
                          onClick={() => setKeyIds((current) => current.filter((_, i) => i !== index))}
                        />
                      ) : null}
                    </div>
                  );
                })}
                {canAddKey ? (
                  <div>
                    <TextButton type="button" aria-label={text("common.addKey")} onClick={() => setKeyIds((current) => [...current, ""])}>
                      <Icon icon={Plus} />
                    </TextButton>
                  </div>
                ) : null}
              </div>
            ) : (
              <p className="flex items-center gap-3 text-muted">
                <Icon icon={KeyRound} />
                Aucune clé SSH. <Link to="/cles">{text("server.createKey")}</Link>
              </p>
            )}
            {keys.length > 0 ? (
              <div className="flex flex-col items-start gap-4">
                <ToolInstall ansible tofu={["hetzner", "scaleway"].includes(providers.find((item) => item.id === providerId)?.provider ?? "")} />
                <Button type="submit" disabled={submitting || !ready}>
                  {submitting ? <span className="size-4 animate-spin rounded-full border border-current border-t-transparent" /> : <Icon icon={Plus} />}
                  {text("ui.creerLeServeur")}
                </Button>
              </div>
            ) : null}
          </>
        ) : null}
      </form>
      {error ? (
        <div className="mt-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
    </>
  );
}

export function ServerCard({
  server,
  controlPlane,
  usage,
  usageLoading,
  compact,
}: {
  server: Server;
  controlPlane?: boolean;
  usage?: ServerUsage;
  usageLoading?: boolean;
  compact?: boolean;
}) {
  const label = statusLabel(server.status);
  const removal = removalOf(useDestructions(), "server", server.id);
  if (compact) {
    return (
      <div className="card-link flex h-full flex-col rounded-md border border-line bg-canvas">
      <Link to={`/serveurs/${server.id}`} className="block flex-1 p-3">
        <div className="flex items-center justify-between gap-2">
          <span className="flex min-w-0 items-center gap-2">
            <Icon icon={ServerIcon} className="shrink-0 text-muted" />
            <span className="truncate">{server.name}</span>
          </span>
          <span className={`shrink-0 ${label.tone}`}>{label.text}</span>
        </div>
        {controlPlane ? <p className="mt-1 text-faint">Control plane</p> : null}
        {server.address ? <p className="mt-2 truncate text-muted">{server.address}</p> : null}
        {placeLabel(server.plan?.location) ? <p className="mt-1 truncate text-muted">{placeLabel(server.plan?.location)}</p> : null}
        {server.plan ? (
          <p className="mt-1 truncate text-muted">
            {server.plan.name} · {server.plan.vcpu} vCPU · {memory(server.plan.memoryMb)}
          </p>
        ) : null}
        {usageLoading ? <Skeleton className="mt-2 h-4 w-full" /> : usage ? <p className="mt-1 truncate text-muted">{usage.measured ? usageText(usage.cpu, usage.memory, usage.disk) : text("common.noMeasure")}</p> : null}
      </Link>
      {removal ? <span className="px-3 pb-3"><RemovalNotice job={removal} /></span> : null}
      </div>
    );
  }
  return (
    <div className="card-link rounded-md border border-line bg-canvas">
    <Link to={`/serveurs/${server.id}`} className="block p-4">
      <div className="mb-3 flex items-center justify-between gap-6">
        <span className="flex items-center gap-3">
          <Icon icon={ServerIcon} className="text-muted" />
          {server.name}
          {controlPlane ? <span className="text-muted">Control plane</span> : null}
        </span>
        <span className={label.tone}>{label.text}</span>
      </div>
      <p className="mb-3 flex items-center gap-2 text-muted">
        {server.provider ? <ProviderLogo id={server.provider} /> : <Icon icon={ServerIcon} />}
        {server.providerName || "Manual enrollment"}
      </p>
      {server.address ? <p className="mb-3 text-muted">{server.address}</p> : null}
      {usageLoading ? (
        <Skeleton className="mb-3 h-4 w-full" />
      ) : usage ? (
        <p className="mb-3 text-muted">{usage.measured ? usageText(usage.cpu, usage.memory, usage.disk) : text("common.noMeasure")}</p>
      ) : null}
      {server.plan ? (
        <p className="text-muted">
          {server.plan.name} · {server.plan.vcpu} vCPU · {memory(server.plan.memoryMb)}
          {server.plan.diskGb > 0 ? ` · ${server.plan.diskGb} Go` : ""}
          {server.plan.storageGb ? ` · ${server.plan.storageLabel} ${server.plan.storageGb} Go` : ""}
          {" · "}
          {server.plan.period === "month"
            ? price(server.plan.monthly, false, server.plan.currency)
            : price(server.plan.hourly, true, server.plan.currency)}
          {placeLabel(server.plan.location) ? ` · ${placeLabel(server.plan.location)}` : ""}
        </p>
      ) : (
        <code className="block break-all font-mono text-[13px] text-muted">{server.joinCommand}</code>
      )}
    </Link>
    {removal ? <div className="px-4 pb-4"><RemovalNotice job={removal} /></div> : null}
    </div>
  );
}

export function ServerPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [server, setServer] = useState<Server | null>(() => peek<Server>(`/api/servers/${id}`));
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [typed, setTyped] = useState("");
  const [removing, setRemoving] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [copied, setCopied] = useState(false);
  const [runtime, setRuntime] = useState<ServerRuntime | null>(() => peek<ServerRuntime>(`/api/servers/${id}/runtime`));
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    let cancel = false;
    async function load() {
      try {
        const next = await api<Server>(`/api/servers/${id}`);
        if (!cancel) {
          setServer(next);
          setError("");
        }
      } catch (err) {
        if (!cancel) setError(err instanceof Error ? err.message : text("common.error"));
      }
    }
    void load();
    const timer = window.setInterval(() => {
      void load();
    }, 2000);
    return () => {
      cancel = true;
      window.clearInterval(timer);
    };
  }, [id]);

  const active = server?.deploy?.some((step) => step.status === "running" || step.status === "pending") && server.status !== "linked";
  const linked = server?.status === "linked";

  useEffect(() => {
    if (!id || !linked) {
      setRuntime(null);
      return;
    }
    let cancel = false;
    const load = () => {
      api<ServerRuntime>(`/api/servers/${id}/runtime`)
        .then((next) => {
          if (!cancel) setRuntime(next);
        })
        .catch(() => {
          if (!cancel) {
            setRuntime({ measured: false, cpu: { used: 0, total: 0 }, memory: { used: 0, total: 0 }, disk: { used: 0, total: 0 }, pods: [] });
          }
        });
    };
    load();
    const timer = window.setInterval(load, 10000);
    return () => {
      cancel = true;
      window.clearInterval(timer);
    };
  }, [id, linked]);

  useEffect(() => {
    if (!confirm) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") setConfirm(false);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [confirm]);

  async function retry() {
    if (!server) return;
    setRetrying(true);
    setError("");
    try {
      const next = await api<Server>(`/api/servers/${server.id}/deploy`, { method: "POST" });
      setServer(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setRetrying(false);
    }
  }

  async function remove(event: FormEvent) {
    event.preventDefault();
    if (!server || typed !== server.name) return;
    setRemoving(true);
    setError("");
    try {
      const job = await api<Destruction>(`/api/servers/${server.id}`, { method: "DELETE", body: JSON.stringify({ name: typed }) });
      navigate(`/destructions/${job.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setRemoving(false);
    }
  }

  if (!server && !error) return <LoadingLines lines={4} />;
  if (!server) return <ErrorText>{error}</ErrorText>;
  const label = statusLabel(server.status);

  return (
    <>
      <PageTitle title={server.name} icon={ServerIcon} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {server.controlPlane ? <p className="mb-3 text-muted">Control plane</p> : null}
      <p className="mb-3 flex items-center gap-2 text-muted">
        {server.provider ? <ProviderLogo id={server.provider} /> : <Icon icon={ServerIcon} />}
        {server.providerName || "Manual enrollment"}
        <span className={label.tone}>{label.text}</span>
      </p>
      {server.clusterName ? <p className="mb-3 text-muted">{server.clusterName}</p> : null}
      {server.address ? <p className="mb-3 text-muted">{server.address}</p> : null}
      <p className="mb-3 text-muted">{exploitation(server.createdAt, now)}</p>
      {server.cost ? <p className="mb-3 text-muted">{billText(server.cost, false, hourlyRate(server))}</p> : <p className="mb-3 text-muted">{text("bill.none")}</p>}
      {server.plan ? (
        <p className="mb-6 text-muted">
          {server.plan.name} · {server.plan.vcpu} vCPU · {memory(server.plan.memoryMb)}
          {server.plan.diskGb > 0 ? ` · ${server.plan.diskGb} Go` : ""}
          {placeLabel(server.plan.location) ? ` · ${placeLabel(server.plan.location)}` : ""}
        </p>
      ) : (
        <div className="mb-6 flex items-start gap-4">
          <code className="min-w-0 flex-1 break-all font-mono text-[13px] text-muted">{server.joinCommand}</code>
          <IconButton
            icon={Copy}
            label={text("common.copy")}
            onClick={() => {
              void navigator.clipboard.writeText(server.joinCommand);
              setCopied(true);
            }}
          />
          {copied ? <span className="text-muted">{text("common.copied")}</span> : null}
        </div>
      )}
      <TofuFiles path={`/api/servers/${server.id}/tofu`} />
      <div className="mb-8 flex flex-col items-start gap-4">
        <ToolInstall ansible tofu={server.tofu} />
        <Button type="button" onClick={() => void retry()} disabled={retrying}>
          {retrying ? <span className="size-4 animate-spin rounded-full border border-current border-t-transparent" /> : <Icon icon={RotateCcw} />}
          {text("ui.relancerLeDeploiement")}
        </Button>
      </div>
      <CancelDeployment kind="server" id={server.id} active={server.status === "pending" || server.status === "deploying"} />
      {linked ? (
        <div className="mb-8">
          <Link to={`/serveurs/${server.id}/terminal`} className="text-muted underline">
            Terminal
          </Link>
        </div>
      ) : null}
      {linked ? (
        <div className="max-w-[720px]">
          <UsageMeters sample={runtime} />
          <div className="mt-10">
            <Fold title="Pods">
              {runtime && runtime.pods.length > 0 ? (
                <ul className="flex flex-col">
                  {runtime.pods.map((pod) => {
                    const health = podHealth(pod);
                    return (
                      <li key={pod.name} className="border-t border-line py-4">
                        <div className="flex items-baseline justify-between gap-6">
                          <span className="font-mono text-[13px]">{pod.name}</span>
                          <span className={health.tone}>{health.text}</span>
                        </div>
                        <p className="mt-1 text-muted">
                          <Link to={`/applications/${pod.appId}`} className="underline">{pod.application}</Link>
                          {pod.service ? ` · ${pod.service}` : ""}
                          {" · "}
                          {podCpu(pod.cpu)} · {size(pod.memory)} · {size(pod.disk)}
                        </p>
                        <PodActions appId={pod.appId} name={pod.name} failed={podFailed(pod)} />
                      </li>
                    );
                  })}
                </ul>
              ) : (
                !runtime ? <LoadingLines lines={2} label="Pods" /> : <p className="text-muted">{runtime.measured ? text("common.noPod") : text("common.noMeasure")}</p>
              )}
            </Fold>
          </div>
        </div>
      ) : server.deploy && server.deploy.length > 0 ? (
        <DeployTimeline steps={server.deploy} live={Boolean(active)} />
      ) : null}
      <div className="mt-10 max-w-[720px]">
        <h2 className="mb-4">{text("nav.history")}</h2>
        <EventList server={server.id} />
      </div>
      <div className="mt-10">
        <TextButton type="button" onClick={() => { setTyped(""); setConfirm(true); }}>
          <Icon icon={Trash2} />
          {text("common.delete")}
        </TextButton>
      </div>
      {confirm ? (
        <div className="fixed inset-0 z-30 flex items-center justify-center bg-ink/40 px-6" role="dialog" aria-modal="true" aria-labelledby="delete-server-title">
          <form onSubmit={remove} className="w-full max-w-md rounded-md border border-line bg-surface p-6">
            <h2 id="delete-server-title" className="mb-3 text-lg">Supprimer {server.name}</h2>
            <p className="mb-6 text-muted">{text("ui.tapezLeNomDuServeurPour")}</p>
            <Field label={text("server.serverName")}>
              <Input value={typed} autoFocus onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
            </Field>
            <div className="mt-6 flex items-center gap-4">
              <Button type="submit" disabled={typed !== server.name || removing}>
                {removing ? <span className="size-4 animate-spin rounded-full border border-current border-t-transparent" /> : null}
                {text("common.delete")}
              </Button>
              <TextButton type="button" onClick={() => setConfirm(false)}>
                {text("common.cancel")}
              </TextButton>
            </div>
          </form>
        </div>
      ) : null}
    </>
  );
}

function DeployTimeline({ steps, live }: { steps: DeployStep[]; live: boolean }) {
  return (
    <ol className="mt-8 max-w-[720px]" aria-live={live ? "polite" : "off"}>
      {steps.map((step, index) => (
        <li key={step.id} className="flex gap-4">
          <span className="flex flex-col items-center">
            <span className={stepDot(step.status)} />
            {index < steps.length - 1 ? <span className="min-h-8 w-px flex-1 bg-line" /> : null}
          </span>
          <span className="pb-6">
            <span className={step.status === "pending" ? "text-faint" : step.status === "failed" ? "text-bad" : step.status === "done" ? "text-ok" : ""}>
              {knownMessage(step.label)}
            </span>
            {step.detail ? <span className="mt-1 block text-faint">{knownMessage(step.detail)}</span> : null}
          </span>
        </li>
      ))}
    </ol>
  );
}

function stepDot(status: string) {
  if (status === "running") return "mt-1.5 size-2.5 animate-pulse rounded-full bg-warn";
  if (status === "done") return "mt-1.5 size-2.5 rounded-full bg-ok";
  if (status === "failed") return "mt-1.5 size-2.5 rounded-full bg-bad";
  return "mt-1.5 size-2.5 rounded-full border border-line";
}

function StorageFields({
  offer,
  period,
  currency,
  storageId,
  storageGb,
  onId,
  onGb,
}: {
  offer: Offer;
  period: string;
  currency: string;
  storageId: string;
  storageGb: string;
  onId: (id: string) => void;
  onGb: (gb: string) => void;
}) {
  const options = offer.storage ?? [];
  const required = options.find((item) => item.required);
  const optional = options.filter((item) => !item.required);
  if (required) {
    return (
      <Field label={`${required.label} (Go)`}>
        <Input
          type="number"
          min={required.minGb}
          max={required.maxGb || undefined}
          value={storageGb || String(required.minGb)}
          onChange={(e) => {
            onId(required.id);
            onGb(e.target.value);
          }}
          required
        />
      </Field>
    );
  }
  if (optional.length === 0) {
    return null;
  }
  const selected = optional.find((item) => item.id === storageId);
  return (
    <>
      <Field label={text("field.extraStorage")}>
        <Select
          value={storageId}
          onChange={(e) => {
            onId(e.target.value);
            const next = optional.find((item) => item.id === e.target.value);
            onGb(next ? String(next.minGb) : "");
          }}
        >
          <option value="">{text("common.none")}</option>
          {optional.map((item) => (
            <option key={item.id} value={item.id}>
              {item.label}
              {period === "month" && item.monthlyPerGb ? ` · ${price(item.monthlyPerGb, false, currency)}/Go` : ""}
              {period !== "month" && item.hourlyPerGb ? ` · ${price(item.hourlyPerGb, true, currency)}/Go` : ""}
            </option>
          ))}
        </Select>
      </Field>
      {selected ? (
        <Field label={text("field.sizeGb")}>
          <Input type="number" min={selected.minGb} max={selected.maxGb || undefined} value={storageGb} onChange={(e) => onGb(e.target.value)} required />
        </Field>
      ) : null}
    </>
  );
}

function chosenStorage(offer: Offer | undefined, storageId: string, storageGb: string) {
  const options = offer?.storage ?? [];
  const required = options.find((item) => item.required);
  const selected = required ?? options.find((item) => item.id === storageId);
  if (!selected) {
    return { id: "", gb: 0 };
  }
  const size = Number(storageGb);
  return { id: selected.id, gb: Number.isFinite(size) && size > 0 ? size : selected.minGb };
}

function exploitation(iso: string | undefined, now: number) {
  if (!iso) return text("ui.dureeDExploitationInconnue");
  const start = new Date(iso).getTime();
  if (!Number.isFinite(start)) return text("ui.dureeDExploitationInconnue");
  const minutes = Math.max(0, Math.floor((now - start) / 60000));
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  const rest = minutes % 60;
  const parts = [];
  if (days) parts.push(`${days} j`);
  if (hours || days) parts.push(`${hours} h`);
  if (!days) parts.push(`${rest} min`);
  return `En service depuis ${parts.join(" ")}`;
}

export function billText(cost: Cost, cluster = false, hourly?: number) {
  const value = Number(cost.amount);
  const unit = cost.currency === "USD" ? "$" : "€";
  const kind = cost.theoretical ? (cluster ? text("bill.clusterTheoretical") : text("bill.theoretical")) : cluster ? text("bill.clusterBilled") : text("bill.billed");
  const total = text("bill.since", { kind, amount: `${money(Number.isFinite(value) ? value : 0)} ${unit}` });
  if (!hourly) return total;
  return `${total} · ${text("bill.rate", { amount: `${money(hourly)} ${unit}` })}`;
}

function price(raw: string | undefined, hour: boolean, currency = "EUR") {
  if (!raw) {
    return "";
  }
  const value = Number(raw);
  if (!Number.isFinite(value)) {
    return raw;
  }
  const digits = value < 0.001 ? 6 : hour && value < 1 ? 4 : 2;
  const text = value.toLocaleString(currentLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
  const unit = currency === "USD" ? "$" : "€";
  return hour ? `${text} ${unit}/h` : `${text} ${unit}/mois`;
}

function memory(mb: number) {
  if (mb >= 1024) {
    return `${(mb / 1024).toLocaleString(currentLocale(), { maximumFractionDigits: 1 })} Go`;
  }
  return `${mb} Mo`;
}
