import { FormEvent, useEffect, useState } from "react";
import { ConfirmDialog, Sheet, Spinner, StatusBadge, Steps, appHealth, useToast } from "./kit";
import { usePoll } from "./poll";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ChevronDown, ChevronUp, Copy, ExternalLink, Plus, Trash2 } from "lucide-react";
import { api, type AppRuntime, type AppService, type Application, type CatalogueCard, type CatalogueVersions, type Cluster, type Destruction, type DnsZone, type Server } from "./api";
import { localizeCard } from "./catalogue-copy";
import { knownMessage, text } from "./i18n";
import { AppLogo } from "./app-logo";
import { CatalogueBrowser } from "./catalogue";
import { UsageMeters } from "./meters";
import { podCpu, size } from "./measure";
import { ImageEditor } from "./images";
import { AppUpdateNotice } from "./updates";
import { AppSecretLinks } from "./secrets";
import { PodActions } from "./pods";
import { LocalImagePicker } from "./localimages";
import { NameList, namesOf, type AppName } from "./names";
import { ImageOrigin } from "./registries";
import { TofuFiles } from "./tofu";
import { ActionsMenu, Button, ErrorText, Field, Fold, Group, Icon, IconButton, Input, LoadingLines, MenuItem, PageTitle, SectionTitle, Select, TextArea, TextButton, focus, podFailed, podHealth, statusLabel, useTrail } from "./ui";

type RedundancyPreview = {
  plan: { scaled: string[]; failover: string[]; database: string[]; alone: string[] };
  nodes: number;
  storageNodes: number;
};

export function NewApp({ onCancel }: { onCancel?: () => void }) {
  const navigate = useNavigate();
  const [search, setSearch] = useSearchParams();
  useTrail([
    { label: text("nav.apps"), to: "/applications" },
    { label: text("crumb.newApp") },
  ]);
  const [servers, setServers] = useState<Server[]>([]);
  const [linked, setLinked] = useState<Server[]>([]);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [name, setName] = useState("");
  const [zones, setZones] = useState<DnsZone[]>([]);
  const [zonesReady, setZonesReady] = useState(false);
  const [names, setNames] = useState<{ subdomain: string; zone: string }[]>([]);
  const [sourceKind, setSourceKind] = useState("catalogue");
  const [source, setSource] = useState("");
  const [article, setArticle] = useState<CatalogueCard | null>(null);
  const [versions, setVersions] = useState<CatalogueVersions | null>(null);
  const [tag, setTag] = useState("latest");
  const [tagTouched, setTagTouched] = useState(false);
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [nameTouched, setNameTouched] = useState(false);
  const [destination, setDestination] = useState("server");
  const [serverId, setServerId] = useState("");
  const [clusterId, setClusterId] = useState("");
  const [copies, setCopies] = useState(1);
  const [preview, setPreview] = useState<RedundancyPreview | null>(null);
  const [listen, setListen] = useState("");
  const [variables, setVariables] = useState("");
  const [services, setServices] = useState<AppService[]>([]);
  const [creating, setCreating] = useState(false);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");

  const nodes = linked.filter((server) => server.clusterId === clusterId).length;
  const placed = destination === "server" ? Boolean(serverId) : Boolean(clusterId);
  // What each service becomes is decided by the server, which knows how each
  // image keeps its data.
  useEffect(() => {
    if (destination !== "cluster" || copies <= 1 || (sourceKind === "catalogue" ? !article : !source.trim())) {
      setPreview(null);
      return;
    }
    let stop = false;
    const body = sourceKind === "catalogue" ? { clusterId, article: article?.id } : { clusterId, image: source.trim() };
    api<RedundancyPreview>("/api/redundancy/preview", { method: "POST", body: JSON.stringify(body) })
      .then((next) => {
        if (!stop) setPreview(next);
      })
      .catch(() => {
        if (!stop) setPreview(null);
      });
    return () => {
      stop = true;
    };
  }, [destination, copies, clusterId, sourceKind, source, article]);

  useEffect(() => {
    if (copies > Math.max(1, nodes)) setCopies(1);
  }, [nodes, copies]);

  useEffect(() => {
    Promise.all([api<Server[]>("/api/servers"), api<Cluster[]>("/api/clusters")])
      .then(([nextServers, nextClusters]) => {
        const lone = nextServers.filter((server) => !server.clusterId);
        const created = nextClusters.filter((cluster) => cluster.origin !== "imported");
        setServers(lone);
        setLinked(nextServers.filter((server) => server.status === "linked"));
        setClusters(created);
        const usable = lone.find((server) => server.status === "linked") ?? lone[0];
        if (usable) setServerId(usable.id);
        if (created[0]) setClusterId(created[0].id);
        if (!lone[0] && created[0]) setDestination("cluster");
      })
      .catch((err) => setError(err.message));
    // The zones come from the DNS provider: only the names section waits for them.
    api<DnsZone[]>("/api/dns/zones")
      .then((nextZones) => {
        setZones(nextZones);
        if (nextZones[0]) setNames([{ subdomain: "", zone: nextZones[0].name }]);
      })
      .catch((err) => setError(err.message))
      .finally(() => setZonesReady(true));
  }, []);

  useEffect(() => {
    const id = search.get("article");
    if (!id) return;
    api<CatalogueCard>(`/api/catalogue/${id}`)
      .then((card) => {
        setSourceKind("catalogue");
        setArticle(card);
        setName((current) => current || card.name);
      })
      .catch(() => undefined);
  }, [search]);

  useEffect(() => {
    if (!article) return;
    setVersions(null);
    setTagTouched(false);
    api<CatalogueVersions>(`/api/catalogue/${article.id}/versions`)
      .then(setVersions)
      .catch(() => setVersions({ name: "", image: "", tags: [], detail: text("ui.lesVersionsNOntPasPu") }));
  }, [article]);

  useEffect(() => {
    if (!versions || tagTouched) return;
    if (versions.tags.includes("latest")) setTag("latest");
    else if (versions.tags[0]) setTag(versions.tags[0]);
  }, [versions, tagTouched]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (creating) return;
    setError("");
    const vars: Record<string, string> = {};
    for (const [index, line] of variables.split("\n").entries()) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const cut = trimmed.indexOf("=");
      if (cut <= 0) {
        setError(`${text("ui.uneVariableSEcritCleValeur")} ${text("apps.badLine", { line: index + 1 })}`);
        return;
      }
      vars[trimmed.slice(0, cut)] = trimmed.slice(cut + 1);
    }
    if (sourceKind === "catalogue" && !article) {
      setError(text("ui.choisissezUneApplicationDuCatalogue"));
      return;
    }
    setCreating(true);
    try {
      if (sourceKind === "catalogue" && article) {
        const created = await api<{ id: string }>(`/api/catalogue/${article.id}`, {
          method: "POST",
          body: JSON.stringify({
            name,
            names: names.filter((item) => item.subdomain.trim() !== ""),
            destinationKind: destination,
            serverId: destination === "server" ? serverId : "",
            clusterId: destination === "cluster" ? clusterId : "",
            inputs,
            tag,
            copies: destination === "cluster" ? copies : 1,
          }),
        });
        navigate(`/applications/${created.id}`);
        return;
      }
      const created = await api<Application>("/api/applications", {
        method: "POST",
        body: JSON.stringify({
          name,
          names: names.filter((item) => item.subdomain.trim() !== ""),
          sourceKind,
          source,
          destinationKind: destination,
          serverId: destination === "server" ? serverId : "",
          clusterId: destination === "cluster" ? clusterId : "",
          replicas: destination === "cluster" ? copies : 1,
          variables: vars,
          services: sourceKind === "image" ? services : [],
          listen: Number(listen) || 0,
        }),
      });
      navigate(`/applications/${created.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setCreating(false);
    }
  }

  const sourceLabel = sourceKind === "git" ? text("ui.depot") : sourceKind === "template" ? text("common.template") : text("common.image");

  function cancelSelection() {
    setArticle(null);
    setVersions(null);
    setInputs({});
    setTag("latest");
    setTagTouched(false);
    if (!nameTouched) setName("");
    if (search.has("article")) {
      const next = new URLSearchParams(search);
      next.delete("article");
      setSearch(next, { replace: true });
    }
  }

  return (
    <>
      <PageTitle
        title={text("crumb.newApp")}
        icon={Plus}
        action={
          onCancel ? (
            <TextButton type="button" onClick={onCancel}>
              {text("common.cancel")}
            </TextButton>
          ) : null
        }
      />
      <form onSubmit={onSubmit} className="flex max-w-[720px] flex-col gap-6">
        <Steps
          steps={[
            {
              title: text("steps.what"),
              ready: sourceKind !== "catalogue" || article !== null,
              content: (
                <>
                  <Field label={text("field.source")}>
                    <Select
                      value={sourceKind}
                      onChange={(e) => {
                        setSourceKind(e.target.value);
                        setArticle(null);
                      }}
                    >
                      <option value="catalogue">{text("common.catalogue")}</option>
                      <option value="image">{text("common.image")}</option>
                      <option value="git">{text("common.git")}</option>
                      <option value="template">{text("common.template")}</option>
                    </Select>
                  </Field>
                  {sourceKind === "catalogue" ? (
                    article ? (
                      <>
                        <div className="flex items-start justify-between gap-4 rounded-md border border-line bg-surface p-4">
                          <span className="flex items-center gap-3">
                            <AppLogo slug={article.logo} name={article.name} size={40} />
                            <span>
                              <span className="font-medium">{article.name}</span>
                              <p className="mt-1 text-muted">{localizeCard(article).summary}</p>
                              {(article.services.find((service) => service.published) ?? article.services[0]) ? (
                                <p className="mt-1">
                                  <ImageOrigin image={(article.services.find((service) => service.published) ?? article.services[0]).image} />
                                </p>
                              ) : null}
                            </span>
                          </span>
                          <Link to={`/catalogue/${article.id}`} className="shrink-0 text-ink underline">
                            {text("catalogue.details")}
                          </Link>
                        </div>
                        <div>
                          <TextButton type="button" onClick={cancelSelection}>
                            {text("common.clear")}
                          </TextButton>
                        </div>
                        <p>{localizeCard(article).description}</p>
                        <Field label={text("field.version")}>
                          <Select
                            value={tag}
                            onChange={(e) => {
                              setTagTouched(true);
                              setTag(e.target.value);
                            }}
                          >
                            {(versions?.tags?.length ? versions.tags : [tag]).map((item) => (
                              <option key={item} value={item}>
                                {item}
                              </option>
                            ))}
                          </Select>
                        </Field>
                        {versions?.detail ? <p className="text-bad">{knownMessage(versions.detail)}</p> : null}
                        {article.inputs.map((input) => (
                          <Field key={input.key} label={input.label}>
                            <Input value={inputs[input.key] ?? ""} onChange={(e) => setInputs((current) => ({ ...current, [input.key]: e.target.value }))} required />
                          </Field>
                        ))}
                      </>
                    ) : (
                      <CatalogueBrowser
                        selected=""
                        onSelect={(card) => {
                          setArticle(card);
                          setInputs({});
                          if (!nameTouched) setName(card.name);
                        }}
                      />
                    )
                  ) : (
                    <div className="flex flex-col gap-3">
                      <Field label={sourceLabel}>
                        <Input
                          value={source}
                          onChange={(e) => setSource(e.target.value)}
                          required
                          autoCapitalize="off"
                          spellCheck={false}
                          placeholder={sourceKind === "image" ? "ghcr.io/organisation/app:1.0.0" : undefined}
                        />
                      </Field>
                      {sourceKind === "image" ? (
                        <>
                          {source.trim() ? <ImageOrigin image={source.trim()} /> : null}
                          <LocalImagePicker value={source} onPick={setSource} />
                          <p className="text-faint">
                            {text("images.sourceHint")}{" "}
                            <Link to="/registres" className="text-ink underline">
                              {text("nav.registries")}
                            </Link>
                            {" · "}
                            <Link to="/images" className="text-ink underline">
                              {text("nav.images")}
                            </Link>
                          </p>
                        </>
                      ) : null}
                    </div>
                  )}
                  <Field label={text("field.name")}>
                    <Input
                      value={name}
                      onChange={(e) => {
                        setNameTouched(true);
                        setName(e.target.value);
                      }}
                      required
                    />
                  </Field>
                </>
              ),
            },
            {
              title: text("steps.address"),
              content: (
                <>
                  {!zonesReady ? (
                    <LoadingLines lines={1} label={text("apps.domains")} />
                  ) : zones.length === 0 ? (
                    <Group label={text("field.domain")}>
                      <p className="text-muted">
                        {text("apps.noDomain")} <Link to="/dns" className="text-ink underline">{text("apps.linkDns")}</Link>
                      </p>
                    </Group>
                  ) : (
                    <NameList names={names} zones={zones} onChange={setNames} keepOne />
                  )}
                </>
              ),
            },
            {
              title: text("steps.where"),
              ready: placed,
              content: (
                <>
                  <Field label={text("field.where")}>
                    <Select value={destination} onChange={(e) => setDestination(e.target.value)}>
                      <option value="server">{text("common.oneMachine")}</option>
                      <option value="cluster">{text("common.oneCluster")}</option>
                    </Select>
                  </Field>
                  {destination === "server" ? (
                    servers.length === 0 ? (
                      <p className="text-muted">
                        {text("apps.noMachine")} <Link to="/clusters" className="text-ink underline">{text("apps.addMachine")}</Link>
                      </p>
                    ) : (
                      <Field label={text("field.machine")}>
                        <Select value={serverId} onChange={(e) => setServerId(e.target.value)}>
                          {servers.map((server) => (
                            <option key={server.id} value={server.id}>
                              {server.status === "linked" ? server.name : `${server.name} · ${statusLabel(server.status).text}`}
                            </option>
                          ))}
                        </Select>
                      </Field>
                    )
                  ) : clusters.length === 0 ? (
                    <p className="text-muted">
                      {text("apps.noCluster")} <Link to="/clusters" className="text-ink underline">{text("apps.createCluster")}</Link>
                    </p>
                  ) : (
                    <Field label={text("field.cluster")}>
                      <Select value={clusterId} onChange={(e) => setClusterId(e.target.value)}>
                        {clusters.map((cluster) => (
                          <option key={cluster.id} value={cluster.id}>
                            {cluster.name}
                          </option>
                        ))}
                      </Select>
                    </Field>
                  )}
                  {destination === "cluster" && clusters.length > 0 ? (
                    <div className="flex flex-col gap-3">
                      <Field label={text("field.redundancy")}>
                        <Select value={copies} onChange={(e) => setCopies(Number(e.target.value))}>
                          <option value={1}>{text("redundancy.single")}</option>
                          {Array.from({ length: Math.max(0, nodes - 1) }, (_, i) => i + 2).map((count) => (
                            <option key={count} value={count}>
                              {text("redundancy.option", { count })}
                            </option>
                          ))}
                        </Select>
                      </Field>
                      {copies <= 1 ? <p className="text-faint">{text("redundancy.createHint")}</p> : null}
                      {preview && preview.plan.failover.length + preview.plan.database.length > 0 && preview.nodes < preview.storageNodes ? (
                        <p className="text-warn">{text("redundancy.storageShort", { count: preview.storageNodes })}</p>
                      ) : null}
                      {preview ? (
                        <ul>
                          {preview.plan.scaled.map((name) => (
                            <li key={name} className="border-t border-line py-2 text-muted">{text("redundancy.scaled", { name, count: copies })}</li>
                          ))}
                          {preview.plan.database.map((name) => (
                            <li key={name} className="border-t border-line py-2 text-muted">{text("redundancy.databaseNew", { name, count: copies })}</li>
                          ))}
                          {preview.plan.failover.map((name) => (
                            <li key={name} className="border-t border-line py-2 text-muted">{text("redundancy.failover", { name })}</li>
                          ))}
                          {preview.plan.alone.map((name) => (
                            <li key={name} className="border-t border-line py-2 text-muted">{text("redundancy.alone", { name })}</li>
                          ))}
                        </ul>
                      ) : null}
                    </div>
                  ) : null}
                </>
              ),
            },
            ...(sourceKind === "catalogue"
              ? []
              : [
                  {
                    title: text("steps.settings"),
                    content: (
                      <>
                        {sourceKind === "catalogue" ? null : (
                        <div>
                          <button type="button" className="inline-flex items-center gap-2 text-muted" onClick={() => setOpen((value) => !value)}>
                            <Icon icon={open ? ChevronUp : ChevronDown} />
                            {open ? text("ui.masquerLesReglages") : text("ui.reglages")}
                          </button>
                          {open ? (
                            <div className="mt-6 flex flex-col gap-6">
                              {sourceKind === "image" && services.length === 0 ? (
                                <>
                                  <Field label={text("app.port")}>
                                    <Input value={listen} onChange={(e) => setListen(e.target.value.replace(/[^0-9]/g, ""))} inputMode="numeric" placeholder={text("app.portDetected")} />
                                  </Field>
                                  <p className="text-faint">{text("app.portHint")}</p>
                                </>
                              ) : null}
                              <Field label={text("app.variables")}>
                                <TextArea value={variables} onChange={(e) => setVariables(e.target.value)} placeholder={"NOM=valeur"} />
                              </Field>
                              {sourceKind === "image" ? (
                              <div className="flex flex-col gap-3">
                                <span className="text-muted">{text("field.services")}</span>
                                <p className="text-faint">{text("apps.serviceHint")}</p>
                                {services.map((service, index) => (
                                  <div key={index} className="flex flex-col gap-2 rounded-md border border-line p-3">
                                    <Input
                                      value={service.name}
                                      placeholder={text("field.name")}
                                      aria-label={text("field.name")}
                                      onChange={(e) => setServices((current) => current.map((item, i) => (i === index ? { ...item, name: e.target.value } : item)))}
                                    />
                                    <Input
                                      value={service.image}
                                      placeholder={text("common.image")}
                                      aria-label={text("common.image")}
                                      onChange={(e) => setServices((current) => current.map((item, i) => (i === index ? { ...item, image: e.target.value } : item)))}
                                    />
                                    <label className="flex items-center gap-2 text-muted">
                                      <input
                                        type="checkbox"
                                        checked={service.published}
                                        onChange={() =>
                                          setServices((current) => current.map((item, i) => ({ ...item, published: i === index })))
                                        }
                                      />
                                      {text("apps.servicePublic")}
                                    </label>
                                    <label className="flex items-center gap-2 text-muted">
                                      <input
                                        type="checkbox"
                                        checked={service.disk}
                                        onChange={(e) => setServices((current) => current.map((item, i) => (i === index ? { ...item, disk: e.target.checked } : item)))}
                                      />
                                      {text("common.disk")}
                                    </label>
                                    <TextButton type="button" onClick={() => setServices((current) => current.filter((_, i) => i !== index))}>
                                      {text("common.remove")}
                                    </TextButton>
                                  </div>
                                ))}
                                <div>
                                  <TextButton
                                    type="button"
                                    onClick={() => setServices((current) => [...current, { name: "", image: "", published: current.length === 0, disk: false }])}
                                  >
                                    <Icon icon={Plus} />
                                    {text("ui.ajouterUnService")}
                                  </TextButton>
                                </div>
                              </div>
                              ) : null}
                            </div>
                          ) : null}
                        </div>
                        )}
                      </>
                    ),
                  },
                ]),
          ]}
          error={error ? <ErrorText>{error}</ErrorText> : null}
          submit={
            <Button type="submit" disabled={creating || !placed || (sourceKind === "catalogue" && !article)}>
              {creating ? <Spinner /> : <Icon icon={Plus} />}
              {text("common.create")}
            </Button>
          }
        />
      </form>
    </>
  );
}

export function ApplicationPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const toast = useToast();
  const appPoll = usePoll<Application>(id ? `/api/applications/${id}` : null, 8000);
  const app = appPoll.data;
  const setApp = appPoll.setData;
  const runtimePoll = usePoll<AppRuntime>(id ? `/api/applications/${id}/runtime` : null, 8000);
  const runtime = runtimePoll.data ?? (runtimePoll.error ? emptyRuntime : null);
  const [copied, setCopied] = useState("");
  const [confirm, setConfirm] = useState<"" | "delete" | "pause">("");
  const [pausing, setPausing] = useState(false);
  const [editingNames, setEditingNames] = useState(false);

  // A new version is followed closely until it answers.
  useEffect(() => {
    if (app?.rollout !== "updating") return;
    const timer = window.setInterval(() => void appPoll.reload(), 4000);
    return () => window.clearInterval(timer);
  }, [app?.rollout, appPoll.reload]);

  async function copy(record: string) {
    await navigator.clipboard.writeText(record);
    setCopied(record);
  }

  if (appPoll.error && !app) return <ErrorText>{appPoll.error}</ErrorText>;
  if (!app) return <LoadingLines lines={4} />;

  async function destroy() {
    if (!app) return;
    const typed = app.guarded ? text("destroy.phrase") : app.name;
    const job = await api<Destruction & { status?: string }>(`/api/applications/${app.id}`, { method: "DELETE", body: JSON.stringify({ name: typed }) });
    if (app.guarded) {
      setConfirm("");
      setApp({ ...app, runStatus: "removing" });
      return;
    }
    navigate(`/destructions/${job.id}`);
  }

  async function setPaused(paused: boolean) {
    if (!app || app.guarded) return;
    setPausing(true);
    try {
      setApp(await api<Application>(`/api/applications/${app.id}/pause`, { method: "POST", body: JSON.stringify({ paused }) }));
      setConfirm("");
      toast(paused ? text("apps.pausedDone", { name: app.name }) : text("apps.resumedDone", { name: app.name }));
    } finally {
      setPausing(false);
    }
  }

  const where = app.destinationKind === "cluster" ? app.clusterName : app.serverName;
  const address = app.domains?.find((domain) => domain.kind === "custom" && domain.status === "ready")?.hostname ?? app.domains?.find((domain) => domain.kind === "custom")?.hostname ?? "";
  const pods = runtime?.pods ?? [];
  const health = appHealth(app, runtime?.status, pods);
  const failing = pods.some((pod) => podFailed(pod));

  return (
    <div className="max-w-[720px]">
      <PageTitle title={app.name} mark={<AppLogo slug={app.logo ?? ""} name={app.name} size={40} />} />
      <div className="-mt-3 flex flex-col gap-2 md:-mt-8">
        <StatusBadge status={health} />
        {health !== "running" && health !== "paused" && app.runDetail ? <p className="text-muted">{app.runDetail}</p> : null}
        {address ? (
          <a href={`https://${address}`} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-2 break-all font-mono text-[13px] underline">
            {address}
            <Icon icon={ExternalLink} />
          </a>
        ) : null}
        <p className="break-all text-muted">
          {app.source.replace(/@sha256:[a-f0-9]+$/, "")} · {where}
        </p>
      </div>
      {app.guarded ? null : <AppUpdateNotice app={app} onStarted={() => void appPoll.reload()} />}
      {app.guarded ? null : (
        <div className="mt-6 flex flex-wrap items-center gap-3">
          {address ? (
            <a href={`https://${address}`} target="_blank" rel="noreferrer" className={`inline-flex min-h-11 items-center gap-2 rounded-md bg-ink px-4 py-2 text-canvas sm:min-h-0 ${focus}`}>
              <Icon icon={ExternalLink} />
              {text("apps.open")}
            </a>
          ) : null}
          <Button type="button" variant="secondary" onClick={() => (app.paused ? void setPaused(false) : setConfirm("pause"))} disabled={pausing}>
            {pausing ? <Spinner /> : null}
            {app.paused ? text("home.resume") : text("apps.pauseAction")}
          </Button>
          <ActionsMenu label={text("apps.more")}>
            <MenuItem tone="danger" onClick={() => setConfirm("delete")}>
              {text("common.delete")}
            </MenuItem>
          </ActionsMenu>
        </div>
      )}
      <div className="mt-8">
        <UsageMeters sample={runtime} />
      </div>
      <section className="mt-10">
        <SectionTitle>{text("field.services")}</SectionTitle>
        {app.guarded ? (
          <div className="mt-4 flex flex-col gap-3">
            <p className="text-muted">{text("apps.selfCopies")}</p>
            {(app.services ?? []).map((service) => (
              <p key={service.name} className="font-mono text-[13px]">
                {service.name} · {service.image} · {text(service.replicas === 1 ? "apps.copyOne" : "apps.copyMany", { count: service.replicas || 1 })}
              </p>
            ))}
          </div>
        ) : app.services && app.services.length > 0 ? (
          <ImageEditor app={app} onChange={setApp} />
        ) : (
          <p className="mt-4 text-muted">{text("apps.noService")}</p>
        )}
      </section>
      <div className="mt-10">
        <Fold title={text("apps.instances")} defaultOpen={failing}>
          <ul className="flex flex-col">
            {pods.map((pod) => {
              const state = podHealth(pod);
              return (
                <li key={pod.name} className="border-t border-line py-4">
                  <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
                    <span className="break-all font-mono text-[13px]">{pod.name}</span>
                    <span className={state.tone}>{state.text}</span>
                  </div>
                  <p className="mt-1 text-muted">
                    {pod.server} · {podCpu(pod.cpu)} · {size(pod.memory)} · {size(pod.disk)}
                  </p>
                  <PodActions appId={app.id} name={pod.name} failed={podFailed(pod)} />
                </li>
              );
            })}
          </ul>
          {!runtime ? <LoadingLines lines={2} label={text("apps.instances")} /> : pods.length === 0 ? <p className="text-muted">{runtime.measured ? text("common.noPod") : text("common.noMeasure")}</p> : null}
        </Fold>
      </div>
      {(app.domains && app.domains.length > 0) || !app.guarded ? (
        <section className="mt-10">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <SectionTitle>{text("apps.addresses")}</SectionTitle>
            {app.guarded ? null : (
              <TextButton type="button" onClick={() => setEditingNames(true)}>
                {text("names.edit")}
              </TextButton>
            )}
          </div>
          <ul className="mt-2 flex flex-col">
            {(app.domains ?? []).filter((domain) => domain.kind === "custom").length === 0 ? (
              <li className="border-t border-line py-4 text-muted">{text("names.none")}</li>
            ) : null}
            {(app.domains ?? []).filter((domain) => domain.kind === "custom").map((domain) => (
              <li key={domain.hostname} className="border-t border-line py-4">
                <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
                  <a href={`https://${domain.hostname}`} target="_blank" rel="noreferrer" className="break-all font-mono text-[13px] underline">
                    {domain.hostname}
                  </a>
                  <StatusBadge status={domain.status} />
                </div>
                {domain.detail ? <p className={`mt-2 ${domain.status === "ready" ? "text-muted" : "text-bad"}`}>{domain.detail}</p> : null}
                {domain.record ? (
                  <div className="mt-3 flex flex-wrap items-center gap-4">
                    <code className="break-all font-mono text-[13px] text-muted">{domain.record}</code>
                    <IconButton icon={Copy} label={text("common.copy")} onClick={() => copy(domain.record!)} />
                    {copied === domain.record ? <span className="text-muted">{text("common.copied")}</span> : null}
                  </div>
                ) : null}
                {domain.zone ? (
                  <div className="mt-3">
                    <Fold title={text("common.advanced")}>
                      <TofuFiles path={`/api/dns/${domain.hostname}/tofu`} />
                    </Fold>
                  </div>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      {app.guarded ? null : <AppVariables app={app} onSaved={() => void appPoll.reload()} />}
      {app.guarded ? null : <AppSecretLinks appId={app.id} />}
      {app.guarded ? (
        <div className="mt-10">
          <TextButton type="button" tone="danger" onClick={() => setConfirm("delete")}>
            <Icon icon={Trash2} />
            {text("common.delete")}
          </TextButton>
        </div>
      ) : null}
      {editingNames ? (
        <NamesSheet
          app={app}
          onClose={() => setEditingNames(false)}
          onSaved={(next) => {
            setApp(next);
            setEditingNames(false);
            toast(text("names.saved", { name: next.name }));
          }}
        />
      ) : null}
      {confirm === "pause" ? (
        <ConfirmDialog title={text("apps.pauseTitle", { name: app.name })} confirmLabel={text("apps.pauseAction")} onConfirm={() => setPaused(true)} onClose={() => setConfirm("")}>
          {text("apps.pauseHint")}
        </ConfirmDialog>
      ) : null}
      {confirm === "delete" ? (
        <ConfirmDialog
          title={app.guarded ? text("apps.removeSelf") : text("apps.removeNamed", { name: app.name })}
          confirmLabel={text("common.delete")}
          danger
          phrase={app.guarded ? text("destroy.phrase") : app.name}
          phraseLabel={app.guarded ? text("field.confirm") : text("field.appName")}
          onConfirm={destroy}
          onClose={() => setConfirm("")}
        >
          {app.guarded ? text("apps.selfWarn", { phrase: text("destroy.phrase") }) : text("apps.confirmName")}
        </ConfirmDialog>
      ) : null}
    </div>
  );
}

const emptyRuntime: AppRuntime = { measured: false, cpu: { used: 0, total: 0 }, memory: { used: 0, total: 0 }, disk: { used: 0, total: 0 }, pods: [] };

const masked = "••••••••";

// AppVariables lists the variables of an application. A sealed value stays
// hidden until its owner asks for it.
function AppVariables({ app, onSaved }: { app: Application; onSaved: () => void }) {
  const [shown, setShown] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const entries = Object.entries(app.variables ?? {}).sort(([a], [b]) => a.localeCompare(b));

  async function reveal(name: string) {
    setError("");
    try {
      const out = await api<{ value: string }>(`/api/applications/${app.id}/variables/${encodeURIComponent(name)}`);
      setShown((current) => ({ ...current, [name]: out.value }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  return (
    <div className="mt-10">
      <Fold title={text("app.variables")}>
        <ul className="flex flex-col">
          {entries.map(([name, value]) => (
            <li key={name} className="flex flex-wrap items-center justify-between gap-6 border-t border-line py-3">
              <span className="font-mono text-[13px]">{name}</span>
              <span className="flex min-w-0 items-center gap-3">
                <span className="truncate font-mono text-[13px] text-muted">{shown[name] ?? value}</span>
                {value === masked && shown[name] === undefined ? (
                  <TextButton type="button" onClick={() => reveal(name)}>
                    {text("app.reveal")}
                  </TextButton>
                ) : null}
              </span>
            </li>
          ))}
        </ul>
        {entries.length === 0 ? <p className="text-muted">{text("app.noVariables")}</p> : null}
        {error ? <ErrorText>{error}</ErrorText> : null}
        <p className="mt-3 text-faint">{text("app.variablesHint")}</p>
        <div className="mt-4">
          <Button type="button" variant="secondary" onClick={() => setEditing(true)} disabled={app.rollout === "updating"}>
            {text("app.editVariables")}
          </Button>
        </div>
      </Fold>
      {editing ? (
        <VariablesSheet
          app={app}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            onSaved();
          }}
        />
      ) : null}
    </div>
  );
}

// VariablesSheet changes the variables of a running application. Its copies
// restart one after the other; if they do not start, the previous variables
// come back. A hidden value left as it is keeps its sealed value.
function VariablesSheet({ app, onClose, onSaved }: { app: Application; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [draft, setDraft] = useState(() =>
    Object.entries(app.variables ?? {})
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([name, value]) => `${name}=${value}`)
      .join("\n"),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent) {
    event.preventDefault();
    const variables: Record<string, string> = {};
    for (const [index, line] of draft.split("\n").entries()) {
      if (!line.trim()) continue;
      const at = line.indexOf("=");
      if (at <= 0) {
        setError(`${text("ui.uneVariableSEcritCleValeur")} ${text("apps.badLine", { line: index + 1 })}`);
        return;
      }
      variables[line.slice(0, at).trim()] = line.slice(at + 1);
    }
    setBusy(true);
    setError("");
    try {
      const out = await api<{ status: string }>(`/api/applications/${app.id}/variables`, { method: "PUT", body: JSON.stringify({ variables }) });
      toast(out.status === "updating" ? text("app.variablesRolling") : text("app.variablesSame"));
      onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  return (
    <Sheet title={text("app.editVariables")} onClose={onClose} busy={busy} wide>
      <form onSubmit={save} className="flex flex-col gap-4">
        <p className="text-muted">{text("app.variablesRestart")}</p>
        <Field label={text("app.variables")}>
          <TextArea value={draft} onChange={(e) => setDraft(e.target.value)} rows={8} placeholder={"NOM=valeur"} spellCheck={false} />
        </Field>
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div className="flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={busy}>
            {busy ? <Spinner /> : null}
            {text("app.saveVariables")}
          </Button>
          <TextButton type="button" onClick={onClose} disabled={busy}>
            {text("common.cancel")}
          </TextButton>
        </div>
      </form>
    </Sheet>
  );
}

// NamesSheet changes the addresses of an application while it runs: the names
// kept are not touched, a new one answers once its record and certificate are
// there, a name taken away stops answering.
function NamesSheet({ app, onClose, onSaved }: { app: Application; onClose: () => void; onSaved: (app: Application) => void }) {
  const [names, setNames] = useState<AppName[]>(() => namesOf(app.domains));
  const [zones, setZones] = useState<DnsZone[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const before = namesOf(app.domains);
  const removed = before.filter((old) => !names.some((name) => name.subdomain === old.subdomain && name.zone === old.zone));

  useEffect(() => {
    api<DnsZone[]>("/api/dns/zones")
      .then(setZones)
      .catch((err) => {
        setZones([]);
        setError(err instanceof Error ? err.message : text("common.error"));
      });
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const kept = names.filter((name) => name.zone !== "" && (name.subdomain.trim() !== "" || before.some((old) => old.zone === name.zone && old.subdomain === "")));
      onSaved(await api<Application>(`/api/applications/${app.id}/names`, { method: "PUT", body: JSON.stringify({ names: kept }) }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  return (
    <Sheet title={text("names.title", { name: app.name })} onClose={onClose} busy={busy} wide>
      <form onSubmit={submit} className="flex flex-col gap-6">
        <p className="text-muted">{text("names.hint")}</p>
        {zones === null ? (
          <LoadingLines lines={2} label={text("apps.domains")} />
        ) : zones.length === 0 ? (
          <p className="text-muted">
            {text("apps.noDomain")}{" "}
            <Link to="/dns" className="text-ink underline">
              {text("apps.linkDns")}
            </Link>
          </p>
        ) : (
          <NameList names={names} zones={zones} onChange={setNames} />
        )}
        {removed.length > 0 ? (
          <p className="text-warn">{text("names.removing", { names: removed.map((name) => (name.subdomain ? `${name.subdomain}.${name.zone}` : name.zone)).join(", ") })}</p>
        ) : null}
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div className="flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={busy || zones === null}>
            {busy ? <Spinner /> : null}
            {text("common.save")}
          </Button>
          <TextButton type="button" onClick={onClose} disabled={busy}>
            {text("common.cancel")}
          </TextButton>
        </div>
      </form>
    </Sheet>
  );
}
