import { FormEvent, useEffect, useState, type ReactNode } from "react";
import { ConfirmDialog, Sheet, Spinner, StatusBadge, Steps, useToast } from "./kit";
import { usePoll } from "./poll";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Copy, KeyRound, Plus, RotateCcw, Server as ServerIcon, SquareTerminal, Trash2 } from "lucide-react";
import { api, type Catalog, type Cost, type DeployStep, type Destruction, type Project, type Secret, type Server, type ServerRuntime, type ServerUsage } from "./api";
import { CancelDeployment } from "./deployments";
import { UsageMeters } from "./meters";
import { hourlyRate, money, podCpu, size, usageText } from "./measure";
import { HostingFields, HostingMark, ServerHosting, ServerSSHUser, emptyHosting, hostingLabel } from "./hosting";
import { OfferFields, OfferStorage, cheapestOffer, chosenStorage, formatPrice, memory } from "./offers";
import { knownPlaces, placeLabel, placeOf } from "./places";
import { ProviderSelect } from "./provider-select";
import { TofuFiles } from "./tofu";
import { EventList } from "./world";
import { currentLocale, text, knownMessage } from "./i18n";
import { useSession } from "./session";
import {
  ActionsMenu,
  Button,
  ErrorText,
  Field,
  Fold,
  Group,
  MenuItem,
  SectionTitle,
  focus,
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
  liveServer,
  removalOf,
  serverState,
  statusLabel,
  useDestructions,
} from "./ui";
import { RemovalNotice } from "./destructions";
import { PodActions } from "./pods";
import { ToolInstall } from "./tools";
import { ServerFacts, ServerUpdateNotice, UpdateBadge, UpdateJobs, useUpdate } from "./updates";
import { UpdateLock, useActivity } from "./activity";
import { useI18n } from "./lang";

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
  const [hosting, setHosting] = useState(emptyHosting);
  // An existing server joins with a command pasted on it, or MyInfra opens SSH itself.
  const [joinMode, setJoinMode] = useState<"script" | "ssh">("script");
  const [address, setAddress] = useState("");
  const [sshUser, setSshUser] = useState("");
  const direct = providerId === "manual" && joinMode === "ssh";
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [keySheet, setKeySheet] = useState(false);

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
          hosting: providerId === "manual" ? hosting : undefined,
          address: direct ? address.trim() : undefined,
          sshUser: direct ? sshUser.trim() : undefined,
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
        <Steps
          steps={[
            {
              title: text("steps.where"),
              ready: providerId !== "",
              content: (
                <>
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
                      { value: "manual", label: text("server.manualOption"), logo: "manual" },
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
                </>
              ),
            },
            ...(providerId === "manual"
              ? [
                  {
                    title: text("steps.machine"),
                    ready: (hosting.provider !== "other" || Boolean(hosting.name?.trim())) && (!direct || address.trim() !== ""),
                    content: (
                      <>
                        <Group label={text("server.joinMode")}>
                          <div className="flex flex-wrap gap-2" role="group" aria-label={text("server.joinMode")}>
                            {(["script", "ssh"] as const).map((mode) => (
                              <button
                                key={mode}
                                type="button"
                                aria-pressed={joinMode === mode}
                                onClick={() => setJoinMode(mode)}
                                className={`min-h-11 rounded-md border px-3 py-2 sm:min-h-0 ${joinMode === mode ? "border-ink bg-status" : "border-line"} ${focus}`}
                              >
                                {text(mode === "script" ? "server.joinScript" : "server.joinSsh")}
                              </button>
                            ))}
                          </div>
                          <p className="mt-2 text-faint">{text(direct ? "server.joinSshHint" : "server.joinScriptHint")}</p>
                        </Group>
                        {direct ? (
                          <Field label={text("server.directAddress")}>
                            <Input value={address} placeholder="203.0.113.10" required onChange={(e) => setAddress(e.target.value)} />
                          </Field>
                        ) : null}
                        {direct ? (
                          <Field label={text("server.sshUser")} hint={text("server.sshUserHint")}>
                            <Input value={sshUser} placeholder="root" autoCapitalize="off" autoComplete="off" spellCheck={false} onChange={(e) => setSshUser(e.target.value)} />
                          </Field>
                        ) : null}
                        <HostingFields value={hosting} onChange={setHosting} />
                      </>
                    ),
                  },
                ]
              : [
                  {
                    title: text("steps.machine"),
                    ready,
                    content: (
                      <>
                        {/* Only what depends on the place and the offer waits: the rest of the form stays. */}
                        {listsLoading ? (
                          <LoadingLines lines={3} label={text("transfer.offers")} />
                        ) : offer && catalog ? (
                          <>
                            <OfferFields offers={catalog.offers} value={offer.id} currency={catalog.currency} onChange={setOfferId} />
                            <p className="text-muted">
                              {offer.hourly ? formatPrice(offer.hourly, true, catalog.currency) : null}
                              {offer.hourly && offer.monthly ? " · " : null}
                              {offer.monthly ? formatPrice(offer.monthly, false, catalog.currency) : null}
                            </p>
                            <div className="flex gap-2" role="group" aria-label={text("server.billing")}>
                              {offer.hourly ? (
                                <button
                                  type="button"
                                  aria-pressed={period === "hour"}
                                  onClick={() => setPeriod("hour")}
                                  className={`min-h-11 rounded-md border px-3 py-2 sm:min-h-0 ${period === "hour" ? "border-ink bg-status" : "border-line"} ${focus}`}
                                >
                                  {text("ui.heure")}
                                </button>
                              ) : null}
                              {offer.monthly ? (
                                <button
                                  type="button"
                                  aria-pressed={period === "month"}
                                  onClick={() => setPeriod("month")}
                                  className={`min-h-11 rounded-md border px-3 py-2 sm:min-h-0 ${period === "month" ? "border-ink bg-status" : "border-line"} ${focus}`}
                                >
                                  {text("ui.mois")}
                                </button>
                              ) : null}
                            </div>
                            <OfferStorage
                              offer={offer}
                              period={period}
                              currency={catalog.currency}
                              storageId={storageId}
                              storageGb={storageGb}
                              onChange={(id, gb) => {
                                setStorageId(id);
                                setStorageGb(gb);
                              }}
                            />
                            {catalog.note ? <p className="text-faint">{catalog.note}</p> : null}
                          </>
                        ) : null}
                      </>
                    ),
                  },
                ]),
            {
              title: text("steps.access"),
              ready: keys.length > 0,
              content: providerId === "" ? null : (
                <>
                  <Field label={text("server.name")}>
                    <Input value={name} onChange={(e) => setName(e.target.value)} required />
                  </Field>
                  {alone ? null : (
                    <div>
                      <label className="flex items-center gap-2">
                        <input type="checkbox" checked={controlPlane} disabled={roleLocked} onChange={(e) => setControlPlane(e.target.checked)} />
                        {text("server.controlPlaneLabel")}
                      </label>
                      <p className="mt-2 text-faint">{roleLocked ? text("server.firstPlane") : text("server.manyPlanes")}</p>
                      {controlPlane && (planes + 1) % 2 === 0 ? <p className="mt-2 text-warn">{text("server.evenPlanes")}</p> : null}
                    </div>
                  )}
                  {keys.length > 0 ? (
                    <Group label={text("nav.ssh")} hint={direct ? text("server.joinSshKeys") : undefined}>
                    <div className="flex flex-col gap-2">
                      {keyIds.map((keyId, index) => {
                        const taken = new Set(keyIds.filter((id, i) => i !== index && id));
                        return (
                          <div key={index} className="flex items-center gap-3">
                            <Select
                              value={keyId}
                              aria-label={text("names.key", { index: index + 1 })}
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
                          <TextButton type="button" onClick={() => setKeyIds((current) => [...current, ""])}>
                            <Icon icon={Plus} />
                            {text("common.addKey")}
                          </TextButton>
                        </div>
                      ) : null}
                    </div>
                    </Group>
                  ) : (
                    <p className="flex flex-wrap items-center gap-3 text-muted">
                      <Icon icon={KeyRound} />
                      {text("server.noKey")}
                      <TextButton type="button" onClick={() => setKeySheet(true)}>
                        {text("server.createKey")}
                      </TextButton>
                    </p>
                  )}
                  {keys.length > 0 ? (
                    <ToolInstall ansible tofu={["hetzner", "scaleway"].includes(providers.find((item) => item.id === providerId)?.provider ?? "")} />
                  ) : null}
                </>
              ),
            },
          ]}
          error={error ? <ErrorText>{error}</ErrorText> : null}
          submit={
            <Button type="submit" disabled={submitting || !ready || keys.length === 0}>
              {submitting ? <Spinner /> : <Icon icon={Plus} />}
              {text("ui.creerLeServeur")}
            </Button>
          }
        />
      </form>
      {keySheet ? (
        <NewKeySheet
          onClose={() => setKeySheet(false)}
          onCreated={(key) => {
            setKeys((current) => [...current, key]);
            setKeyIds([key.id]);
            setKeySheet(false);
          }}
        />
      ) : null}
    </>
  );
}

// NewKeySheet makes an SSH key without leaving the form: MyInfra keeps the
// private half, the form goes on with the new key chosen.
function NewKeySheet({ onClose, onCreated }: { onClose: () => void; onCreated: (key: Secret) => void }) {
  const [name, setName] = useState("myinfra");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      onCreated(await api<Secret>("/api/secrets/ssh-keys", { method: "POST", body: JSON.stringify({ name }) }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  return (
    <Sheet title={text("server.createKey")} onClose={onClose} busy={busy}>
      <form onSubmit={submit} className="flex flex-col gap-6">
        <Field label={text("field.name")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} required data-autofocus />
        </Field>
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div className="flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={busy || name.trim() === ""}>
            {busy ? <Spinner /> : <Icon icon={Plus} />}
            {text("common.create")}
          </Button>
          <TextButton type="button" onClick={onClose} disabled={busy}>
            {text("common.cancel")}
          </TextButton>
        </div>
      </form>
    </Sheet>
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
  const label = statusLabel(serverState(server));
  const removal = removalOf(useDestructions(), "server", server.id);
  const update = useUpdate("server", server.id);
  if (compact) {
    return (
      <div className="card-link flex h-full flex-col rounded-md border border-line bg-canvas">
      <Link to={`/serveurs/${server.id}`} className="block flex-1 p-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="flex min-w-0 items-center gap-2">
            <Icon icon={ServerIcon} className="shrink-0 text-muted" />
            <span className="truncate">{server.name}</span>
          </span>
          <span className={`shrink-0 ${label.tone}`}>{label.text}</span>
        </div>
        {controlPlane ? <p className="mt-1 text-faint">{text("server.controlPlaneShort")}</p> : null}
        {update ? <p className="mt-2"><UpdateBadge item={update} /></p> : null}
        {server.address ? <p className="mt-2 truncate text-muted">{server.address}</p> : null}
        {placeLabel(server.plan?.location) || placeLabel(server.place) ? <p className="mt-1 truncate text-muted">{placeLabel(server.plan?.location) || placeLabel(server.place)}</p> : null}
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
          {controlPlane ? <span className="text-muted">{text("server.controlPlaneShort")}</span> : null}
        </span>
        <span className={label.tone}>{label.text}</span>
      </div>
      {update ? <p className="-mt-1 mb-3"><UpdateBadge item={update} /></p> : null}
      <p className="mb-3 flex items-center gap-2 text-muted">
        <HostingMark server={server} />
        {hostingLabel(server)}
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
          {server.plan.diskGb > 0 ? ` · ${server.plan.diskGb} ${text("unit.gb")}` : ""}
          {server.plan.storageGb ? ` · ${server.plan.storageLabel} ${server.plan.storageGb} ${text("unit.gb")}` : ""}
          {" · "}
          {server.plan.period === "month"
            ? formatPrice(server.plan.monthly, false, server.plan.currency)
            : formatPrice(server.plan.hourly, true, server.plan.currency)}
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
  const toast = useToast();
  const { canDeploy } = useSession();
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [copied, setCopied] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const serverPoll = usePoll<Server>(id ? `/api/servers/${id}` : null, 10000);
  const { updating, transferring } = useActivity();
  const server = serverPoll.data;
  const setServer = serverPoll.setData;
  const linked = !!server && liveServer(server);
  const deploying = server?.status === "pending" || server?.status === "deploying";
  const active = Boolean(server?.deploy?.some((step) => step.status === "running" || step.status === "pending")) && !linked;
  const runtimePoll = usePoll<ServerRuntime>(id && linked ? `/api/servers/${id}/runtime` : null, 10000);
  const runtime = linked ? (runtimePoll.data ?? (runtimePoll.error ? emptyServerRuntime : null)) : null;

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30000);
    return () => window.clearInterval(timer);
  }, []);

  // A deployment moves every few seconds: follow it closely, then calm down.
  useEffect(() => {
    if (!deploying) return;
    const timer = window.setInterval(() => {
      if (!document.hidden) void serverPoll.reload();
    }, 2000);
    return () => window.clearInterval(timer);
  }, [deploying, serverPoll.reload]);

  async function retry() {
    if (!server) return;
    setRetrying(true);
    setError("");
    try {
      setServer(await api<Server>(`/api/servers/${server.id}/deploy`, { method: "POST" }));
      toast(text("server.retried", { name: server.name }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setRetrying(false);
    }
  }

  async function remove() {
    if (!server) return;
    const job = await api<Destruction>(`/api/servers/${server.id}`, { method: "DELETE", body: JSON.stringify({ name: server.name }) });
    navigate(`/destructions/${job.id}`);
  }

  if (!server && !serverPoll.error) return <LoadingLines lines={4} />;
  if (!server) return <ErrorText>{serverPoll.error}</ErrorText>;
  const failed = server.status === "failed";
  const locked = updating(server.id, server.clusterId) || transferring;
  const facts: [string, ReactNode][] = [
    [text("server.status"), <StatusBadge status={serverState(server)} />],
    [text("server.provider"), (
      <span className="inline-flex items-center gap-2">
        <HostingMark server={server} />
        {hostingLabel(server)}
      </span>
    )],
    ...(server.clusterName ? [[text("server.cluster"), `${server.clusterName}${server.controlPlane ? ` · ${text("server.controlPlaneShort")}` : ""}`] as [string, ReactNode]] : []),
    ...(server.plan ? [[text("server.offer"), `${server.plan.name} · ${server.plan.vcpu} vCPU · ${memory(server.plan.memoryMb)}${server.plan.diskGb > 0 ? ` · ${server.plan.diskGb} ${text("unit.gb")}` : ""}`] as [string, ReactNode]] : []),
    ...(placeLabel(server.plan?.location) || (!(canDeploy && !server.providerId) && placeLabel(server.place)) ? [[text("server.place"), placeLabel(server.plan?.location) || placeLabel(server.place)] as [string, ReactNode]] : []),
    ...(server.address ? [[text("server.address"), <span className="break-all font-mono text-[13px]">{server.address}</span>] as [string, ReactNode]] : []),
    [text("server.cost"), server.cost ? billText(server.cost, false, hourlyRate(server)) : text("bill.none")],
    [text("server.uptime"), exploitation(server.createdAt, now)],
  ];

  return (
    <>
      <PageTitle title={server.name} icon={ServerIcon} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      <dl className="grid max-w-[720px] grid-cols-1 gap-x-8 gap-y-3 sm:grid-cols-[max-content_1fr]">
        {facts.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted">{label}</dt>
            <dd className="-mt-2 mb-1 min-w-0 sm:m-0">{value}</dd>
          </div>
        ))}
      </dl>
      {!server.providerId && canDeploy ? (
        <div className="mt-6 max-w-[720px]">
          <Field label={text("server.place")} hint={text("server.placeHint")}>
            <Select
              value={placeOf(server.place)?.label ?? server.place ?? ""}
              onChange={(e) => {
                const place = e.target.value;
                void api<Server>(`/api/servers/${server.id}/place`, { method: "PUT", body: JSON.stringify({ place }) })
                  .then(setServer)
                  .catch((err) => setError(err instanceof Error ? err.message : text("common.error")));
              }}
            >
              <option value="">{text("ui.sansLieuConnu")}</option>
              {server.place && !knownPlaces().some((item) => item.label === server.place) ? <option value={server.place}>{server.place}</option> : null}
              {knownPlaces().map((item) => (
                <option key={item.label} value={item.label}>
                  {item.label}
                </option>
              ))}
            </Select>
          </Field>
          <ServerHosting server={server} onSaved={setServer} />
          <ServerSSHUser server={server} onSaved={setServer} />
        </div>
      ) : null}
      <ReachNotice server={server} onChecked={setServer} onRemove={() => setConfirm(true)} />
      <ServerFacts serverId={server.id} />
      {!server.plan && server.joinCommand ? (
        <div className="mt-6 flex max-w-[720px] items-start gap-4">
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
      ) : null}
      <div className="mt-6 flex flex-wrap items-center gap-3">
        {linked && canDeploy ? (
          <Link to={`/serveurs/${server.id}/terminal`} className={`inline-flex min-h-11 items-center gap-2 rounded-md border border-line bg-surface px-4 py-2 sm:min-h-0 ${focus}`}>
            <Icon icon={SquareTerminal} />
            {text("server.terminal")}
          </Link>
        ) : null}
        {canDeploy && failed ? (
          <Button type="button" onClick={() => void retry()} disabled={retrying || locked}>
            {retrying ? <Spinner /> : <Icon icon={RotateCcw} />}
            {text("ui.relancerLeDeploiement")}
          </Button>
        ) : null}
        {canDeploy ? <CancelDeployment kind="server" id={server.id} active={deploying} /> : null}
        {canDeploy ? (
          <ActionsMenu label={text("apps.more")}>
            {!failed && !locked ? <MenuItem onClick={() => void retry()}>{text("ui.relancerLeDeploiement")}</MenuItem> : null}
            <MenuItem tone="danger" onClick={() => setConfirm(true)}>
              {text("common.delete")}
            </MenuItem>
          </ActionsMenu>
        ) : null}
      </div>
      <div className="max-w-[720px]">
        <UpdateLock locked={locked && !transferring} />
        <ServerUpdateNotice server={server} onStarted={() => void serverPoll.reload()} />
      </div>
      <div className="mt-6 max-w-[720px]">
        <UpdateJobs filter={(job) => job.status === "running" && job.steps.some((step) => step.serverId === server.id)} />
      </div>
      {failed || deploying ? <div className="mt-4"><ToolInstall ansible tofu={server.tofu} /></div> : null}
      {server.deploy && server.deploy.length > 0 ? (
        <div className="mt-8 max-w-[720px]">
          {linked ? (
            <Fold title={text("server.deployHistory")}>
              <DeployTimeline steps={server.deploy} live={false} />
            </Fold>
          ) : (
            <DeployTimeline steps={server.deploy} live={active} />
          )}
        </div>
      ) : null}
      {linked ? (
        <div className="mt-8 max-w-[720px]">
          <UsageMeters sample={runtime} />
          <div className="mt-10">
            <Fold title={text("apps.instances")} defaultOpen={Boolean(runtime?.pods.some((pod) => podFailed(pod)))}>
              {runtime && runtime.pods.length > 0 ? (
                <ul className="flex flex-col">
                  {runtime.pods.map((pod) => {
                    const health = podHealth(pod);
                    return (
                      <li key={pod.name} className="border-t border-line py-4">
                        <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
                          <span className="break-all font-mono text-[13px]">{pod.name}</span>
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
              ) : !runtime ? (
                <LoadingLines lines={2} label={text("apps.instances")} />
              ) : (
                <p className="text-muted">{runtime.measured ? text("common.noPod") : text("common.noMeasure")}</p>
              )}
            </Fold>
          </div>
        </div>
      ) : null}
      <div className="mt-10 max-w-[720px]">
        <Fold title={text("common.advanced")}>
          <TofuFiles path={`/api/servers/${server.id}/tofu`} />
        </Fold>
      </div>
      <div className="mt-10 max-w-[720px]">
        <SectionTitle>{text("nav.history")}</SectionTitle>
        <div className="mt-4">
          <EventList server={server.id} />
        </div>
      </div>
      {confirm ? (
        <ConfirmDialog
          title={text("server.removeNamed", { name: server.name })}
          confirmLabel={text("common.delete")}
          danger
          phrase={server.name}
          phraseLabel={text("server.serverName")}
          onConfirm={remove}
          onClose={() => setConfirm(false)}
        >
          {text("ui.tapezLeNomDuServeurPour")}
        </ConfirmDialog>
      ) : null}
    </>
  );
}

const emptyServerRuntime: ServerRuntime = { measured: false, cpu: { used: 0, total: 0 }, memory: { used: 0, total: 0 }, disk: { used: 0, total: 0 }, pods: [] };

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



function exploitation(iso: string | undefined, now: number) {
  if (!iso) return text("ui.dureeDExploitationInconnue");
  const start = new Date(iso).getTime();
  if (!Number.isFinite(start)) return text("ui.dureeDExploitationInconnue");
  const minutes = Math.max(0, Math.floor((now - start) / 60000));
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  const rest = minutes % 60;
  const parts = [];
  if (days) parts.push(`${days} ${text("unit.day")}`);
  if (hours || days) parts.push(`${hours} h`);
  if (!days) parts.push(`${rest} min`);
  return text("server.upFor", { time: parts.join(" ") });
}

export function billText(cost: Cost, cluster = false, hourly?: number) {
  const value = Number(cost.amount);
  const unit = cost.currency === "USD" ? "$" : "€";
  const kind = cost.theoretical ? (cluster ? text("bill.clusterTheoretical") : text("bill.theoretical")) : cluster ? text("bill.clusterBilled") : text("bill.billed");
  const total = text("bill.since", { kind, amount: `${money(Number.isFinite(value) ? value : 0)} ${unit}` });
  if (!hourly) return total;
  return `${total} · ${text("bill.rate", { amount: `${money(hourly)} ${unit}` })}`;
}



// ReachNotice tells, on the page of a server, that it no longer answers or
// that its provider no longer has it, and what to do about it.
function ReachNotice({ server, onChecked, onRemove }: { server: Server; onChecked: (server: Server) => void; onRemove: () => void }) {
  useI18n();
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState("");
  if (!server.reach) return null;
  const gone = server.reach === "gone";
  const provider = hostingLabel(server);

  async function check() {
    setChecking(true);
    setError("");
    try {
      onChecked(await api<Server>(`/api/servers/${server.id}/check`, { method: "POST" }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setChecking(false);
    }
  }

  return (
    <div className="mt-6 flex max-w-[720px] flex-col gap-3 rounded-md border border-bad px-4 py-3">
      <p className="font-medium text-bad">{gone ? text("reach.goneTitle", { provider }) : text("reach.lostTitle")}</p>
      <p className="text-muted">
        {gone
          ? text("reach.goneText")
          : server.seenAt
            ? text("reach.lostSince", { since: when(server.seenAt) })
            : text("reach.lostText")}
      </p>
      {!gone && server.providerCheckedAt ? (
        <p className="text-[13px] text-faint">
          {server.providerError ? text("reach.providerError", { error: knownMessage(server.providerError) }) : text("reach.providerPresent", { provider, at: when(server.providerCheckedAt) })}
        </p>
      ) : null}
      {error ? <ErrorText>{error}</ErrorText> : null}
      <div className="flex flex-wrap gap-3">
        {gone ? (
          <Button type="button" variant="danger" onClick={onRemove}>
            <Icon icon={Trash2} />
            {text("reach.remove")}
          </Button>
        ) : null}
        <Button type="button" variant="secondary" onClick={() => void check()} disabled={checking}>
          {checking ? <Spinner /> : <Icon icon={RotateCcw} />}
          {text("reach.check")}
        </Button>
      </div>
    </div>
  );
}

function when(at: string) {
  return new Date(at).toLocaleString(currentLocale(), { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
}
