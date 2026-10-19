import { FormEvent, useEffect, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ChevronDown, ChevronUp, Copy, ExternalLink, Plus, Trash2 } from "lucide-react";
import { api, peek, type AppRuntime, type AppService, type Application, type CatalogueCard, type CatalogueVersions, type Cluster, type Destruction, type DnsZone, type Server } from "./api";
import { localizeCard } from "./catalogue-copy";
import { knownMessage, text } from "./i18n";
import { AppLogo } from "./app-logo";
import { CatalogueBrowser } from "./catalogue";
import { UsageMeters } from "./meters";
import { podCpu, size } from "./measure";
import { ImageEditor } from "./images";
import { AppSecretLinks } from "./secrets";
import { PodActions } from "./pods";
import { TofuFiles } from "./tofu";
import { Button, ErrorText, Field, Fold, Icon, IconButton, Input, LoadingLines, PageTitle, SectionTitle, Select, TextArea, TextButton, podFailed, podHealth, statusLabel, useTrail } from "./ui";

export function NewApp({ onCancel }: { onCancel?: () => void }) {
  const navigate = useNavigate();
  const [search, setSearch] = useSearchParams();
  useTrail([
    { label: text("nav.apps"), to: "/" },
    { label: text("crumb.newApp") },
  ]);
  const [servers, setServers] = useState<Server[]>([]);
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
  const [replicas, setReplicas] = useState("1");
  const [listen, setListen] = useState("");
  const [variables, setVariables] = useState("");
  const [services, setServices] = useState<AppService[]>([]);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    Promise.all([api<Server[]>("/api/servers"), api<Cluster[]>("/api/clusters")])
      .then(([nextServers, nextClusters]) => {
        const lone = nextServers.filter((server) => !server.clusterId);
        const created = nextClusters.filter((cluster) => cluster.origin !== "imported");
        setServers(lone);
        setClusters(created);
        if (lone[0]) setServerId(lone[0].id);
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
    setError("");
    const vars: Record<string, string> = {};
    for (const line of variables.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const cut = trimmed.indexOf("=");
      if (cut <= 0) {
        setError(text("ui.uneVariableSEcritCleValeur"));
        return;
      }
      vars[trimmed.slice(0, cut)] = trimmed.slice(cut + 1);
    }
    try {
      if (sourceKind === "catalogue") {
        if (!article) {
          setError(text("ui.choisissezUneApplicationDuCatalogue"));
          return;
        }
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
          replicas: Number(replicas) || 1,
          variables: vars,
          services,
          listen: Number(listen) || 0,
        }),
      });
      navigate(`/applications/${created.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  const sourceLabel = sourceKind === "git" ? text("ui.depot") : sourceKind === "template" ? "Modèle" : text("common.image");

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
                  </span>
                </span>
                <Link to={`/catalogue/${article.id}`} className="shrink-0 text-ink">
                  {text("apps.more")}
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
          <Field label={sourceLabel}>
            <Input value={source} onChange={(e) => setSource(e.target.value)} required />
          </Field>
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
        <Field label={text("field.domain")}>
          {!zonesReady ? (
            <LoadingLines lines={1} label={text("apps.domains")} />
          ) : zones.length === 0 ? (
            <p className="text-muted">
              {text("apps.noDomain")} <Link to="/dns">{text("apps.linkDns")}</Link>
            </p>
          ) : (
            <div className="flex flex-col gap-3">
              {names.map((item, index) => (
                <div key={index} className="flex items-stretch">
                  <input
                    value={item.subdomain}
                    placeholder="sous-domaine"
                    onChange={(e) =>
                      setNames((current) => current.map((row, i) => (i === index ? { ...row, subdomain: e.target.value } : row)))
                    }
                    className="min-w-0 flex-1 rounded-l-md rounded-r-none border border-line bg-surface px-3 py-2 outline-none focus:border-focus"
                  />
                  <span className="flex items-center border-y border-line bg-surface px-2 text-muted">.</span>
                  <select
                    value={item.zone}
                    onChange={(e) =>
                      setNames((current) => current.map((row, i) => (i === index ? { ...row, zone: e.target.value } : row)))
                    }
                    className="max-w-[55%] rounded-l-none rounded-r-md border border-l-0 border-line bg-surface px-3 py-2 outline-none focus:border-focus"
                  >
                    {zones.map((zone) => (
                      <option key={zone.accountId + zone.name} value={zone.name}>
                        {zone.name}
                      </option>
                    ))}
                  </select>
                  {names.length > 1 ? (
                    <span className="ml-3 self-center">
                      <TextButton type="button" onClick={() => setNames((current) => current.filter((_, i) => i !== index))}>
                        {text("common.remove")}
                      </TextButton>
                    </span>
                  ) : null}
                </div>
              ))}
              <div>
                <TextButton type="button" onClick={() => setNames((current) => [...current, { subdomain: "", zone: zones[0].name }])}>
                  <Icon icon={Plus} />
                  {text("field.addName")}
                </TextButton>
              </div>
            </div>
          )}
        </Field>
        <Field label={text("field.where")}>
          <Select value={destination} onChange={(e) => setDestination(e.target.value)}>
            <option value="server">{text("common.oneMachine")}</option>
            <option value="cluster">{text("common.oneCluster")}</option>
          </Select>
        </Field>
        {destination === "server" ? (
          servers.length === 0 ? (
            <p className="text-muted">
              {text("apps.noMachine")} <Link to="/clusters">{text("apps.addMachine")}</Link>
            </p>
          ) : (
            <Field label={text("field.machine")}>
              <Select value={serverId} onChange={(e) => setServerId(e.target.value)}>
                {servers.map((server) => (
                  <option key={server.id} value={server.id}>
                    {server.name}
                  </option>
                ))}
              </Select>
            </Field>
          )
        ) : clusters.length === 0 ? (
          <p className="text-muted">
            {text("apps.noCluster")} <Link to="/clusters">{text("apps.createCluster")}</Link>
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
        {sourceKind === "catalogue" ? null : (
        <div>
          <button type="button" className="inline-flex items-center gap-2 text-muted" onClick={() => setOpen((value) => !value)}>
            <Icon icon={open ? ChevronUp : ChevronDown} />
            {open ? text("ui.masquerLesReglages") : text("ui.reglages")}
          </button>
          {open ? (
            <div className="mt-6 flex flex-col gap-6">
              <Field label={text("field.redundancy")}>
                <Input value={replicas} onChange={(e) => setReplicas(e.target.value)} inputMode="numeric" />
              </Field>
              <p className="text-faint">{text("app.replicasHint")}</p>
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
              <div className="flex flex-col gap-3">
                <span className="text-muted">{text("field.services")}</span>
                <p className="text-faint">{text("apps.serviceHint")}</p>
                {services.map((service, index) => (
                  <div key={index} className="flex flex-col gap-2 rounded-md border border-line p-3">
                    <Input
                      value={service.name}
                      placeholder="nom"
                      onChange={(e) => setServices((current) => current.map((item, i) => (i === index ? { ...item, name: e.target.value } : item)))}
                    />
                    <Input
                      value={service.image}
                      placeholder="image"
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
                      {text("status.ready")}
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
            </div>
          ) : null}
        </div>
        )}
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div>
          <Button type="submit">
            <Icon icon={Plus} />
            {text("common.create")}
          </Button>
        </div>
      </form>
    </>
  );
}

export function ApplicationPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [app, setApp] = useState<Application | null>(() => peek<Application>(`/api/applications/${id}`));
  const [runtime, setRuntime] = useState<AppRuntime | null>(() => peek<AppRuntime>(`/api/applications/${id}/runtime`));
  const [error, setError] = useState("");
  const [copied, setCopied] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [typed, setTyped] = useState("");
  const [removing, setRemoving] = useState(false);
  const [pausing, setPausing] = useState(false);
  const [pauseError, setPauseError] = useState("");

  useEffect(() => {
    if (!id) return;
    let stop = false;
    api<Application>(`/api/applications/${id}`)
      .then((next) => {
        if (!stop) setApp(next);
      })
      .catch((err) => {
        if (!stop) setError(err.message);
      });
    return () => {
      stop = true;
    };
  }, [id]);

  useEffect(() => {
    if (!id || app?.rollout !== "updating") return;
    let stop = false;
    const timer = window.setInterval(() => {
      api<Application>(`/api/applications/${id}`)
        .then((next) => {
          if (!stop) setApp(next);
        })
        .catch(() => undefined);
    }, 4000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [id, app?.rollout]);

  useEffect(() => {
    if (!id) return;
    let stop = false;
    const load = () => {
      api<AppRuntime>(`/api/applications/${id}/runtime`)
        .then((next) => {
          if (!stop) setRuntime(next);
        })
        .catch(() => {
          if (!stop) setRuntime({ measured: false, cpu: { used: 0, total: 0 }, memory: { used: 0, total: 0 }, disk: { used: 0, total: 0 }, pods: [] });
        });
    };
    load();
    const timer = window.setInterval(load, 8000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [id]);

  async function copy(record: string) {
    await navigator.clipboard.writeText(record);
    setCopied(record);
  }

  useEffect(() => {
    if (!confirm) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape" && !removing) setConfirm(false);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [confirm, removing]);

  async function destroy(event: FormEvent) {
    event.preventDefault();
    const phrase = app?.guarded ? text("destroy.phrase") : app?.name;
    if (!app || typed !== phrase) return;
    setRemoving(true);
    setError("");
    try {
      const job = await api<Destruction & { status?: string }>(`/api/applications/${app.id}`, { method: "DELETE", body: JSON.stringify({ name: typed }) });
      if (app.guarded) {
        setError("");
        setConfirm(false);
        setApp({ ...app, runStatus: "failed" });
        setRemoving(false);
        return;
      }
      navigate(`/destructions/${job.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setRemoving(false);
    }
  }

  if (error) return <ErrorText>{error}</ErrorText>;
  if (!app) return <LoadingLines lines={4} />;

  async function togglePause() {
    if (!app || app.guarded) return;
    setPausing(true);
    setPauseError("");
    try {
      setApp(await api<Application>(`/api/applications/${app.id}/pause`, { method: "POST", body: JSON.stringify({ paused: !app.paused }) }));
    } catch (err) {
      setPauseError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setPausing(false);
    }
  }

  const where = app.destinationKind === "cluster" ? app.clusterName : app.serverName;
  const address = app.domains?.find((domain) => domain.kind === "custom" && domain.status === "ready")?.hostname ?? app.domains?.find((domain) => domain.kind === "custom")?.hostname ?? "";
  const health = app.paused ? "paused" : runtime?.pods.some((pod) => podFailed(pod)) ? "failed" : runtime?.status || app.runStatus;

  return (
    <div className="max-w-[720px]">
      <PageTitle title={app.name} mark={<AppLogo slug={app.logo ?? ""} name={app.name} size={40} />} />
      {address ? (
        <p className="mb-2">
          <a href={`https://${address}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-2 font-mono text-[13px] underline">
            {address}
            <Icon icon={ExternalLink} />
          </a>
        </p>
      ) : null}
      <p className="text-muted">
        {app.source} · {where}
      </p>
      {health ? <p className={`mt-3 ${statusLabel(health).tone}`}>{statusLabel(health).text}</p> : null}
      {health !== "running" && health !== "paused" && app.runDetail ? <p className="mt-1 text-muted">{app.runDetail}</p> : null}
      {app.guarded ? null : (
        <div className="mt-4 flex flex-col items-start gap-2">
          <p className="text-muted">{text("apps.pauseHint")}</p>
          {pauseError ? <ErrorText>{pauseError}</ErrorText> : null}
          <Button type="button" onClick={togglePause} disabled={pausing}>
            {app.paused ? text("home.resume") : text("apps.pauseAction")}
          </Button>
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
                {service.name} · {service.image} · {service.replicas || 1} {service.replicas === 1 ? "copie" : "copies"}
              </p>
            ))}
          </div>
        ) : app.services && app.services.length > 0 ? (
          <ImageEditor app={app} onChange={setApp} />
        ) : (
          <p className="mt-4 text-muted">{text("apps.noService")}</p>
        )}
      </section>
      {app.guarded ? null : <AppVariables app={app} />}
      {app.guarded ? null : <AppSecretLinks appId={app.id} />}
      <div className="mt-10">
        <Fold title="Pods">
          <ul className="flex flex-col">
            {(runtime?.pods ?? []).map((pod) => {
              const state = podHealth(pod);
              return (
                <li key={pod.name} className="border-t border-line py-4">
                  <div className="flex items-baseline justify-between gap-6">
                    <span className="font-mono text-[13px]">{pod.name}</span>
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
          {!runtime ? <LoadingLines lines={2} label="Pods" /> : runtime.pods.length === 0 ? <p className="text-muted">{runtime.measured ? text("common.noPod") : text("common.noMeasure")}</p> : null}
        </Fold>
      </div>
      <ul className="mt-12 flex flex-col">
        {app.domains?.map((domain) => {
          const label = statusLabel(domain.status);
          return (
            <li key={domain.hostname} className="border-t border-line py-4">
              <div className="flex items-baseline justify-between gap-6">
                <a href={`https://${domain.hostname}`} target="_blank" rel="noreferrer" className="font-mono text-[13px] underline">
                  {domain.hostname}
                </a>
                <span className={label.tone}>{label.text}</span>
              </div>
              {domain.detail ? <p className={`mt-2 ${domain.status === "ready" ? "text-muted" : "text-bad"}`}>{domain.detail}</p> : null}
              {domain.zone ? <TofuFiles path={`/api/dns/${domain.hostname}/tofu`} /> : null}
              {domain.record ? (
                <div className="mt-3 flex items-center gap-4">
                  <code className="font-mono text-[13px] text-muted">{domain.record}</code>
                  <IconButton icon={Copy} label={text("common.copy")} onClick={() => copy(domain.record!)} />
                  {copied === domain.record ? <span className="text-muted">{text("common.copied")}</span> : null}
                </div>
              ) : null}
          </li>
        );
      })}
      </ul>
      <div className="mt-10">
        <TextButton type="button" onClick={() => { setTyped(""); setConfirm(true); }}>
          <Icon icon={Trash2} />
          {text("common.delete")}
        </TextButton>
      </div>
      {confirm ? (
        <div className="fixed inset-0 z-30 flex items-center justify-center bg-ink/40 px-6" role="dialog" aria-modal="true" aria-labelledby="delete-app-title">
          <form onSubmit={destroy} className="w-full max-w-md rounded-md border border-line bg-surface p-6">
            <h2 id="delete-app-title" className="mb-3 text-lg">{app.guarded ? text("apps.removeSelf") : text("apps.removeNamed", { name: app.name })}</h2>
            <p className="mb-6 text-muted">
              {app.guarded
                ? text("apps.selfWarn", { phrase: text("destroy.phrase") })
                : text("apps.confirmName")}
            </p>
            <Field label={app.guarded ? text("field.confirm") : text("field.appName")}>
              <Input value={typed} autoFocus onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
            </Field>
            <div className="mt-6 flex items-center gap-4">
              <Button type="submit" disabled={typed !== (app.guarded ? text("destroy.phrase") : app.name) || removing}>
                {removing ? <span className="size-4 animate-spin rounded-full border border-current border-t-transparent" /> : null}
                {text("common.delete")}
              </Button>
              <TextButton type="button" onClick={() => setConfirm(false)} disabled={removing}>
                {text("common.cancel")}
              </TextButton>
            </div>
          </form>
        </div>
      ) : null}
    </div>
  );
}

const masked = "••••••••";

// AppVariables lists the variables of an application. A sealed value stays
// hidden until its owner asks for it.
function AppVariables({ app }: { app: Application }) {
  const [shown, setShown] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const entries = Object.entries(app.variables ?? {}).sort(([a], [b]) => a.localeCompare(b));
  if (entries.length === 0) return null;

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
            <li key={name} className="flex items-center justify-between gap-6 border-t border-line py-3">
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
        {error ? <ErrorText>{error}</ErrorText> : null}
        <p className="mt-3 text-faint">{text("app.variablesHint")}</p>
      </Fold>
    </div>
  );
}
