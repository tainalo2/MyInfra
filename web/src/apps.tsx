import { FormEvent, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";
import { ConfirmDialog, Sheet, Spinner, StatusBadge, Steps, Tabs, appHealth, useToast, useWide } from "./kit";
import { usePoll } from "./poll";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ChevronDown, ChevronUp, Copy, ExternalLink, Pencil, Plus, Trash2 } from "lucide-react";
import { api, type AppRuntime, type AppService, type Application, type CatalogueCard, type CatalogueVersions, type Cluster, type Destruction, type DnsZone, type PackageRow, type PodRuntime, type Server, fillAddress, followsAddress } from "./api";
import { ChosenName, PlanLinks, PlanPanel, appPlan, serviceOfPod, underPlan } from "./plan";
import { localizeCard } from "./catalogue-copy";
import { knownMessage, text, type MsgKey } from "./i18n";
import { Code } from "./code-lazy";
import { AppLogo } from "./app-logo";
import { CatalogueBrowser } from "./catalogue";
import { UsageMeters } from "./meters";
import { podCpu, size } from "./measure";
import { ImageEditor } from "./images";
import { AppUpdateNotice } from "./updates";
import { UpdateLock, useActivity } from "./activity";
import { useSession } from "./session";
import { AppSecretLinks } from "./secrets";
import { PodActions } from "./pods";
import { LocalImagePicker } from "./localimages";
import { NameList, NameRow, namesOf, publicServices, type AppName } from "./names";
import { ImageOrigin } from "./registries";
import { AppBackups } from "./backups";
import { ActionsMenu, Button, ErrorText, Field, Group, Icon, IconButton, Input, LoadingLines, MenuItem, PageTitle, Select, TextButton, focus, podFailed, podHealth, statusLabel, useTrail } from "./ui";

type RedundancyPreview = {
  plan: { scaled: string[]; failover: string[]; database: string[]; alone: string[] };
  nodes: number;
  storageNodes: number;
};

const sources: { key: string; label: MsgKey; hint: MsgKey }[] = [
  { key: "catalogue", label: "common.catalogue", hint: "source.catalogueHint" },
  { key: "image", label: "source.myImage", hint: "source.imageHint" },
  { key: "package", label: "package.source", hint: "source.packageHint" },
];

// SourceChoice picks where a new application comes from: three cards for the
// usual ways, and the rarer ones below.
function SourceChoice({ value, onChange }: { value: string; onChange: (next: string) => void }) {
  const other = !sources.some((item) => item.key === value);
  return (
    <div className="flex flex-col gap-2">
      <div role="radiogroup" aria-label={text("field.source")} className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {sources.map((item) => {
          const on = item.key === value;
          return (
            <button
              key={item.key}
              type="button"
              role="radio"
              aria-checked={on}
              onClick={() => onChange(item.key)}
              className={`flex flex-col gap-1 rounded-md border bg-surface p-4 text-left ${on ? "border-ink shadow-[inset_0_0_0_1px_var(--color-ink)]" : "border-line"} ${focus}`}
            >
              <span className="flex items-center justify-between gap-2">
                {text(item.label)}
                <span aria-hidden className={`size-4 rounded-full border ${on ? "border-ink bg-[radial-gradient(circle,var(--color-ink)_0_3px,var(--color-surface)_3.5px)]" : "border-line"}`} />
              </span>
              <span className="text-[13px] text-muted">{text(item.hint)}</span>
            </button>
          );
        })}
      </div>
      <label className="flex items-center gap-2 self-start text-[13px] text-muted">
        {text("source.other")}
        <select value={other ? value : ""} onChange={(event) => event.target.value && onChange(event.target.value)} className={`rounded-md border border-line bg-surface px-2 py-1 ${focus}`}>
          <option value="">—</option>
          <option value="git">{text("common.git")}</option>
          <option value="template">{text("common.template")}</option>
        </select>
      </label>
    </div>
  );
}

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
  const [chosenPackage, setChosenPackage] = useState<PackageRow | null>(null);
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
  // A catalogue article or a package must be chosen before anything is created.
  const picked = sourceKind === "catalogue" ? article !== null : sourceKind === "package" ? chosenPackage !== null : true;
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
    const id = search.get("package");
    if (!id) return;
    api<PackageRow>(`/api/packages/${id}`)
      .then((row) => {
        setSourceKind("package");
        setChosenPackage(row);
        setName((current) => current || row.name);
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
    if (sourceKind === "package" && !chosenPackage) {
      setError(text("package.choose"));
      return;
    }
    setCreating(true);
    try {
      if (sourceKind === "package" && chosenPackage) {
        const created = await api<{ id: string }>(`/api/packages/${chosenPackage.id}/install`, {
          method: "POST",
          body: JSON.stringify({
            name,
            names: names.filter((item) => item.subdomain.trim() !== ""),
            destinationKind: destination,
            serverId: destination === "server" ? serverId : "",
            clusterId: destination === "cluster" ? clusterId : "",
            inputs,
            copies: destination === "cluster" ? copies : 1,
          }),
        });
        navigate(`/applications/${created.id}`);
        return;
      }
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
    setChosenPackage(null);
    setVersions(null);
    setInputs({});
    setTag("latest");
    setTagTouched(false);
    if (!nameTouched) setName("");
    if (search.has("article") || search.has("package")) {
      const next = new URLSearchParams(search);
      next.delete("article");
      next.delete("package");
      setSearch(next, { replace: true });
    }
  }

  return (
    <>
      <PageTitle
        inTree
        title={text("crumb.newApp")}
        action={
          onCancel ? (
            <TextButton type="button" onClick={onCancel}>
              {text("common.cancel")}
            </TextButton>
          ) : null
        }
      />
      <div className="flex flex-col gap-8 lg:flex-row lg:items-start">
      <form onSubmit={onSubmit} className="flex min-w-0 max-w-[720px] flex-1 flex-col gap-6">
        <Steps
          steps={[
            {
              title: text("steps.what"),
              ready: picked,
              content: (
                <>
                  <SourceChoice
                    value={sourceKind}
                    onChange={(next) => {
                      setSourceKind(next);
                      setArticle(null);
                      setChosenPackage(null);
                    }}
                  />
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
                  ) : sourceKind === "package" ? (
                    chosenPackage ? (
                      <>
                        <div className="flex items-start justify-between gap-4 rounded-md border border-line bg-surface p-4">
                          <span>
                            <span className="font-medium">{chosenPackage.name}</span>
                            {chosenPackage.summary ? <p className="mt-1 text-muted">{chosenPackage.summary}</p> : null}
                            <p className="mt-1 font-mono text-[13px] text-muted">{chosenPackage.services.join(" · ")}</p>
                          </span>
                          <Link to={`/applications/paquets/${chosenPackage.id}`} className="shrink-0 text-ink underline">
                            {text("package.open")}
                          </Link>
                        </div>
                        <div>
                          <TextButton type="button" onClick={cancelSelection}>
                            {text("common.clear")}
                          </TextButton>
                        </div>
                        {chosenPackage.inputs.map((input) => (
                          <Field key={input.key} label={input.label}>
                            <Input value={inputs[input.key] ?? ""} onChange={(e) => setInputs((current) => ({ ...current, [input.key]: e.target.value }))} required />
                          </Field>
                        ))}
                      </>
                    ) : (
                      <PackageChoice
                        onSelect={(row) => {
                          setChosenPackage(row);
                          setInputs({});
                          if (!nameTouched) setName(row.name);
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
                          placeholder={
                            sourceKind === "git"
                              ? "https://github.com/org/app@main"
                              : sourceKind === "image"
                                ? "ghcr.io/organisation/app:1.0.0"
                                : undefined
                          }
                        />
                      </Field>
                      {sourceKind === "image" ? (
                        <>
                          {source.trim() ? <ImageOrigin image={source.trim()} /> : null}
                          <LocalImagePicker value={source} onPick={setSource} />
                          <p className="text-faint">
                            {text("images.sourceHint")}{" "}
                            <Link to="/images" className="text-ink underline">
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
            ...(sourceKind === "catalogue" || sourceKind === "package"
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
                                <Code value={variables} onChange={setVariables} language="env" label={text("app.variables")} placeholder="NOM=valeur" minHeight="6rem" />
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
            <Button type="submit" disabled={creating || !placed || !picked}>
              {creating ? <Spinner /> : <Icon icon={Plus} />}
              {text("common.create")}
            </Button>
          }
        />
      </form>
      {picked ? (
        <aside aria-label={text("newApp.recap")} className="flex flex-col gap-3 rounded-md border border-line bg-surface p-4 lg:sticky lg:top-4 lg:w-[300px] lg:shrink-0">
          <h2>{text("newApp.recap")}</h2>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-[13px]">
            {(
              [
                [text("dnsrec.col.app"), sourceKind === "catalogue" && article ? `${article.name} ${tag}` : sourceKind === "package" && chosenPackage ? chosenPackage.name : source || "—"],
                [text("field.name"), name || "—"],
                [text("dnsrec.col.name"), names.filter((item) => item.subdomain.trim()).map((item) => `${item.subdomain}.${item.zone}`).join(", ") || "—"],
                [text("newApp.where"), destination === "server" ? servers.find((server) => server.id === serverId)?.name ?? "—" : clusters.find((cluster) => cluster.id === clusterId)?.name ?? "—"],
                [text("redundancy.copies"), String(destination === "cluster" ? copies : 1)],
              ] as [string, string][]
            ).map(([label, value]) => (
              <div key={label} className="contents">
                <dt className="text-muted">{label}</dt>
                <dd className="min-w-0 break-words">{value}</dd>
              </div>
            ))}
          </dl>
          <p className="text-[12px] text-muted">{text("newApp.noPurchase")}</p>
        </aside>
      ) : null}
      </div>
    </>
  );
}

// PackageChoice lists the person's packages, to install one.
function PackageChoice({ onSelect }: { onSelect: (row: PackageRow) => void }) {
  const [rows, setRows] = useState<PackageRow[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    api<PackageRow[]>("/api/packages")
      .then(setRows)
      .catch((err) => setError(err.message));
  }, []);
  if (error) return <ErrorText>{error}</ErrorText>;
  if (rows === null) return <LoadingLines lines={3} />;
  if (rows.length === 0) {
    return (
      <p className="text-muted">
        {text("package.noneYet")}{" "}
        <Link to="/applications/paquets" className="text-ink underline">
          {text("nav.packages")}
        </Link>
      </p>
    );
  }
  return (
    <ul className="grid grid-cols-1 gap-4 md:grid-cols-2">
      {rows.map((row) => (
        <li key={row.id}>
          <button type="button" onClick={() => onSelect(row)} className={`flex h-full w-full flex-col gap-2 rounded-md border border-line bg-surface p-4 text-left hover:bg-status ${focus}`}>
            <span className="font-medium">{row.name}</span>
            {row.summary ? <span className="text-muted">{row.summary}</span> : null}
            <span className="font-mono text-[13px] text-muted">{row.services.join(" · ")}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}

// PlanDetail says what the chosen element of the plan is, under the plan:
// a service's pods, the addresses behind the proxy, a disk's service.
function PlanDetail({ app, pods, selected }: { app: Application; pods: PodRuntime[]; selected: string | null }) {
  if (!selected) return null;
  const services = app.services ?? [];
  if (selected.startsWith("svc:")) {
    const name = selected.slice(4);
    const service = services.find((svc) => svc.name === name);
    const mine = pods.filter((pod) => serviceOfPod(services, pod.name) === name);
    return (
      <div className="mb-8 flex flex-col gap-3 rounded-md border border-line bg-surface p-4">
        {service ? <p className="break-all font-mono text-[13px] text-muted">{service.image.replace(/@sha256:[a-f0-9]+$/, "")}</p> : null}
        {(service?.exposed ?? []).length > 0 ? <p className="text-[13px] text-warn">{text("plan.link.direct")} · {(service?.exposed ?? []).join(", ")}</p> : null}
        <ul className="flex flex-col">
          {mine.map((pod) => {
            const state = podHealth(pod);
            return (
              <li key={pod.name} className="flex flex-wrap items-baseline justify-between gap-x-4 border-t border-line py-2 first:border-t-0">
                <span className="break-all font-mono text-[13px]">{pod.name}</span>
                <span className="text-[13px] text-muted">
                  {pod.server} · <span className={state.tone}>{state.text}</span>
                </span>
              </li>
            );
          })}
          {mine.length === 0 ? <li className="text-muted">{text("common.noPod")}</li> : null}
        </ul>
      </div>
    );
  }
  if (selected === "proxy" || selected === "internet") {
    const hosts = (app.domains ?? []).filter((domain) => domain.kind === "custom" || domain.kind === "default");
    return (
      <ul className="mb-8 flex flex-col rounded-md border border-line bg-surface p-4">
        {hosts.map((domain) => (
          <li key={domain.hostname} className="flex flex-wrap items-baseline justify-between gap-x-4 border-t border-line py-2 first:border-t-0">
            <span className="break-all font-mono text-[13px]">{domain.hostname}</span>
            <StatusBadge status={domain.status} />
          </li>
        ))}
        {hosts.length === 0 ? <li className="text-muted">{text("names.none")}</li> : null}
      </ul>
    );
  }
  return null;
}

// AppPart is what one tab of the application shows.
function AppPart({ on, children }: { on: boolean; children: ReactNode }) {
  return (
    <div role="tabpanel" hidden={!on}>
      {children}
    </div>
  );
}

function AppPods({ appId, pods, runtime }: { appId: string; pods: PodRuntime[]; runtime: AppRuntime | null }) {
  if (!runtime) return <LoadingLines lines={2} label={text("apps.instances")} />;
  if (pods.length === 0) return <p className="text-muted">{runtime.measured ? text("common.noPod") : text("common.noMeasure")}</p>;
  return (
    <ul className="flex flex-col">
      {pods.map((pod) => {
        const state = podHealth(pod);
        return (
          <li key={pod.name} className="border-t border-line py-4 first:border-t-0 first:pt-0">
            <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
              <span className="break-all font-mono text-[13px]">{pod.name}</span>
              <span className={state.tone}>{state.text}</span>
            </div>
            <p className="mt-1 text-muted">
              {pod.server} · {podCpu(pod.cpu)} · {size(pod.memory)} · {size(pod.disk)}
            </p>
            <PodActions appId={appId} name={pod.name} failed={podFailed(pod)} />
          </li>
        );
      })}
    </ul>
  );
}

const sameName = (a: AppName, b: AppName) => a.subdomain === b.subdomain && a.zone === b.zone && (a.service ?? "") === (b.service ?? "");
const hostOf = (name: AppName) => (name.subdomain ? `${name.subdomain}.${name.zone}` : name.zone);

// NameEditor adds or changes one address, in its place in the list. The server
// takes the whole list and drops what is missing from it: every other address
// goes with it as it is.
function NameEditor({
  app,
  from,
  service,
  zones,
  services,
  locked,
  onClose,
  onSaved,
}: {
  app: Application;
  // from is the address being changed; without it, a new one.
  from?: AppName;
  // service is what a new address leads to: the one the page is showing.
  service?: string;
  zones: DnsZone[];
  services: ReturnType<typeof publicServices>;
  locked: boolean;
  onClose: () => void;
  onSaved: (app: Application) => void;
}) {
  const [name, setName] = useState<AppName>(() => from ?? { subdomain: "", zone: zones[0]?.name ?? "", ...(service ? { service } : {}) });
  const [removing, setRemoving] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const list = namesOf(app.domains);
  const index = from ? list.findIndex((item) => sameName(item, from)) : list.length;
  const named = name.zone !== "" && (name.subdomain.trim() !== "" || from?.subdomain === "");
  const moved = Boolean(from) && hostOf(name) !== hostOf(from!);

  async function send(names: AppName[]) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      onSaved(await api<Application>(`/api/applications/${app.id}/names`, { method: "PUT", body: JSON.stringify({ names }) }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!named || locked) return;
    void send(from ? list.map((item) => (sameName(item, from) ? name : item)) : [...list, name]);
  }

  return (
    <form
      onSubmit={submit}
      onKeyDown={(event) => {
        if (event.key === "Escape" && !busy) onClose();
      }}
      className="flex flex-col gap-3"
    >
      {removing ? (
        <p className="text-warn">{text("names.removing", { names: hostOf(from!) })}</p>
      ) : (
        <>
          <NameRow item={name} index={index} zones={zones} onChange={(next) => setName((current) => ({ ...current, ...next }))} services={services} autoFocus />
          {moved ? <p className="text-warn">{text("names.removing", { names: hostOf(from!) })}</p> : null}
          <p className="text-[13px] text-faint">{text("names.hint")}</p>
        </>
      )}
      {error ? <ErrorText>{error}</ErrorText> : null}
      <div className="flex flex-wrap items-center gap-4">
        {removing ? (
          <Button type="button" variant="danger" disabled={busy || locked} onClick={() => void send(list.filter((item) => !sameName(item, from!)))}>
            {busy ? <Spinner /> : null}
            {text("common.remove")}
          </Button>
        ) : (
          <Button type="submit" disabled={busy || locked || !named || (from ? sameName(name, from) : false)}>
            {busy ? <Spinner /> : null}
            {text("common.save")}
          </Button>
        )}
        <TextButton type="button" onClick={() => (removing ? setRemoving(false) : onClose())} disabled={busy}>
          {text("common.cancel")}
        </TextButton>
        {from && !removing ? (
          <span className="ml-auto">
            <TextButton type="button" tone="danger" onClick={() => setRemoving(true)} disabled={busy}>
              <Icon icon={Trash2} />
              {text("common.remove")}
            </TextButton>
          </span>
        ) : null}
      </div>
    </form>
  );
}

function AppAddresses({
  app,
  domains,
  publics,
  service,
  editable,
  locked,
  onSaved,
}: {
  app: Application;
  domains: NonNullable<Application["domains"]>;
  publics: ReturnType<typeof publicServices>;
  service?: string;
  editable: boolean;
  locked: boolean;
  onSaved: (app: Application) => void;
}) {
  const [copied, setCopied] = useState("");
  // The address being changed, by its hostname; "" is a new one.
  const [editing, setEditing] = useState<string | null>(null);
  const [zones, setZones] = useState<DnsZone[] | null>(null);
  const [zonesError, setZonesError] = useState("");

  async function copy(record: string) {
    await navigator.clipboard.writeText(record);
    setCopied(record);
  }

  function edit(hostname: string) {
    setEditing(hostname);
    if (zones) return;
    api<DnsZone[]>("/api/dns/zones")
      .then(setZones)
      .catch((err) => {
        setZones([]);
        setZonesError(err instanceof Error ? err.message : text("common.error"));
      });
  }

  function editor(from?: AppName) {
    if (zonesError) return <ErrorText>{zonesError}</ErrorText>;
    if (zones === null) return <LoadingLines lines={2} label={text("apps.domains")} />;
    if (zones.length === 0) {
      return (
        <div className="flex flex-wrap items-center gap-4">
          <p className="text-muted">
            {text("apps.noDomain")}{" "}
            <Link to="/dns" className="text-ink underline">
              {text("apps.linkDns")}
            </Link>
          </p>
          <TextButton type="button" onClick={() => setEditing(null)}>
            {text("common.cancel")}
          </TextButton>
        </div>
      );
    }
    return (
      <NameEditor
        key={editing}
        app={app}
        from={from}
        service={service}
        zones={zones}
        services={publicServices(app.services, app.domains)}
        locked={locked}
        onClose={() => setEditing(null)}
        onSaved={(next) => {
          setEditing(null);
          onSaved(next);
        }}
      />
    );
  }

  return (
    <>
      {editable ? (
        <div className="mb-2 flex justify-end">
          <IconButton type="button" icon={Plus} label={text("field.addName")} onClick={() => edit("")} disabled={locked} />
        </div>
      ) : null}
      <ul className="flex flex-col">
        {domains.length === 0 && editing !== "" ? <li className="py-4 text-muted first:pt-0">{text("names.none")}</li> : null}
        {domains.map((domain) => (
          <li key={domain.hostname} className="border-t border-line py-4 first:border-t-0 first:pt-0">
            {editing === domain.hostname ? (
              editor(namesOf([domain])[0])
            ) : (
              <>
                <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1">
                  <span className="flex min-w-0 flex-wrap items-baseline gap-x-3">
                    <a href={`https://${domain.hostname}`} target="_blank" rel="noreferrer" className="break-all font-mono text-[13px] underline">
                      {domain.hostname}
                    </a>
                    {publics.others.length > 0 ? <span className="text-[13px] text-muted">{domain.service || publics.main}</span> : null}
                  </span>
                  <span className="flex items-center gap-4">
                    <StatusBadge status={domain.status} />
                    {editable && domain.kind === "custom" ? <IconButton type="button" icon={Pencil} label={`${text("names.edit")} ${domain.hostname}`} onClick={() => edit(domain.hostname)} disabled={locked} /> : null}
                  </span>
                </div>
                {domain.detail ? <p className={`mt-2 ${domain.status === "ready" ? "text-muted" : "text-bad"}`}>{domain.detail}</p> : null}
                {domain.record ? (
                  <div className="mt-3 flex flex-wrap items-center gap-4">
                    <code className="break-all font-mono text-[13px] text-muted">{domain.record}</code>
                    <IconButton icon={Copy} label={text("common.copy")} onClick={() => copy(domain.record!)} />
                    {copied === domain.record ? <span className="text-muted">{text("common.copied")}</span> : null}
                  </div>
                ) : null}
              </>
            )}
          </li>
        ))}
        {editing === "" ? <li className="border-t border-line py-4 first:border-t-0 first:pt-0">{editor()}</li> : null}
      </ul>
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
  const [confirm, setConfirm] = useState<"" | "delete" | "pause" | "restart">("");
  const [pausing, setPausing] = useState(false);
  const [planned, setPlanned] = useState<string | null>(null);
  // The part asked for; until then the page chooses, pods first if one fails.
  const [part, setPart] = useState<string | null>(null);
  const wide = useWide();
  const nav = useRef<HTMLDivElement>(null);
  const panels = useRef<HTMLDivElement>(null);
  const { updating, transferring } = useActivity();
  const { canDeploy } = useSession();

  // A new version is followed closely until it answers.
  useEffect(() => {
    if (app?.rollout !== "updating") return;
    const timer = window.setInterval(() => void appPoll.reload(), 4000);
    return () => window.clearInterval(timer);
  }, [app?.rollout, appPoll.reload]);

  // Another part starts at its top, right under the nav, however far the one
  // before was scrolled.
  useLayoutEffect(() => {
    if (!nav.current || !panels.current) return;
    const past = nav.current.getBoundingClientRect().bottom - panels.current.getBoundingClientRect().top;
    if (past > 0) nav.current.closest("main")?.scrollBy({ top: -past });
  }, [part, planned]);

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

  // The running application becomes one of the person's packages, to install again.
  async function keepAsPackage() {
    try {
      const row = await api<{ id: string }>(`/api/applications/${app!.id}/package`, { method: "POST" });
      navigate(`/applications/paquets/${row.id}`);
    } catch (err) {
      toast(err instanceof Error ? err.message : text("common.error"), "bad");
    }
  }

  const where = app.destinationKind === "cluster" ? app.clusterName : app.serverName;
  // With several public services, each shows its names, or the address
  // MyInfra gave it while it has none.
  const publics = publicServices(app.services, app.domains);
  const exposed = (app.services ?? []).filter((svc) => (svc.exposed ?? []).length > 0).map((svc) => ({ service: svc.name, ports: svc.exposed ?? [] }));
  const listedDomains = (app.domains ?? []).filter((domain) => domain.kind === "custom" || (domain.kind === "default" && Boolean(domain.service)));
  const address = app.domains?.find((domain) => domain.kind === "custom" && domain.status === "ready")?.hostname ?? app.domains?.find((domain) => domain.kind === "custom")?.hostname ?? "";
  const pods = runtime?.pods ?? [];
  const health = appHealth(app, runtime?.status, pods);
  const failing = pods.some((pod) => podFailed(pod));
  // During its update or that of its servers, the application only changes
  // once it is over; it can still be removed.
  const locked = app.rollout === "updating" || updating(app.serverId, app.clusterId) || transferring;
  const mutable = canDeploy && !app.guarded;
  const planShown = (app.services ?? []).length > 0 && !app.guarded;
  const model = appPlan(app, runtime);
  // On a phone, the chosen element opens in its row of the list.
  const element = (wide && planShown && model.nodes.find((node) => node.key === planned)) || null;
  const serviceName = element?.kind === "service" ? element.key.slice(4) : "";
  const entry = element?.kind === "internet" || element?.kind === "proxy";
  const shownPods = serviceName ? pods.filter((pod) => serviceOfPod(app.services ?? [], pod.name) === serviceName) : pods;
  const shownDomains = entry
    ? (app.domains ?? []).filter((domain) => domain.kind === "custom" || domain.kind === "default")
    : serviceName
      ? listedDomains.filter((domain) => (domain.service || publics.main) === serviceName)
      : listedDomains;
  const shownExposed = serviceName ? exposed.filter((item) => item.service === serviceName) : exposed;
  const podsPart = { key: "pods", label: `${text("apps.instances")} · ${shownPods.length}`, mark: shownPods.some((pod) => podFailed(pod)) };
  const addressesPart = { key: "addresses", label: text("apps.addresses") };
  const exposedPart = { key: "exposed", label: text("apps.exposed") };
  const backupsPart = { key: "backups", label: text("backup.title") };
  const linksPart = { key: "links", label: text("plan.part.links") };
  // The parts of the page follow the element chosen in the plan, as in a
  // template; without one, those of the whole application.
  const parts: { key: string; label: string; mark?: boolean }[] = !element
    ? [
        { key: "services", label: text("field.services") },
        podsPart,
        ...((app.domains ?? []).length > 0 || mutable ? [addressesPart] : []),
        ...(exposed.length > 0 ? [exposedPart] : []),
        ...(mutable ? [backupsPart, { key: "variables", label: text("app.variables") }, { key: "secrets", label: text("nav.secrets") }] : []),
      ]
    : serviceName
      ? [podsPart, ...(shownDomains.length > 0 ? [addressesPart] : []), ...(shownExposed.length > 0 ? [exposedPart] : []), linksPart]
      : entry
        ? [addressesPart, ...(element.kind === "internet" && exposed.length > 0 ? [exposedPart] : []), linksPart]
        : element.kind === "backup" && mutable
          ? [backupsPart, linksPart]
          : [linksPart];
  // A part common to two elements stays open from one to the other.
  const wanted = part ?? (failing ? "pods" : "services");
  const shown = (parts.find((item) => item.key === wanted) ?? parts[0]).key;

  // The head of the page: the state of the application and where it answers,
  // its measures beside them on a wide screen. The menu names it. Its actions
  // sit on the line of the nav, in sight while the page scrolls; a phone keeps
  // them under its title, the nav staying low. Below the plan, the settings
  // keep the width of a form.
  const actions = mutable ? (
    <div className="flex shrink-0 flex-wrap items-center gap-3 md:flex-nowrap">
      {address ? (
        <a href={`https://${address}`} target="_blank" rel="noreferrer" className={`inline-flex min-h-11 items-center gap-2 rounded-md bg-ink px-4 py-2 text-canvas sm:min-h-0 sm:py-1 ${focus}`}>
          <Icon icon={ExternalLink} />
          {text("apps.open")}
        </a>
      ) : null}
      <Button type="button" variant="secondary" className="sm:py-1!" onClick={() => (app.paused ? void setPaused(false) : setConfirm("pause"))} disabled={pausing || locked}>
        {pausing ? <Spinner /> : null}
        {app.paused ? text("home.resume") : text("apps.pauseAction")}
      </Button>
      <ActionsMenu label={text("apps.more")}>
        {app.sourceKind === "image" && !locked ? <MenuItem onClick={() => navigate(`/applications/${app.id}/services`)}>{text("appServices.edit")}</MenuItem> : null}
        {!app.paused && !locked ? <MenuItem onClick={() => setConfirm("restart")}>{text("apps.restart")}</MenuItem> : null}
        <MenuItem onClick={() => void keepAsPackage()}>{text("appServices.keep")}</MenuItem>
        <MenuItem tone="danger" onClick={() => setConfirm("delete")}>
          {text("common.delete")}
        </MenuItem>
      </ActionsMenu>
    </div>
  ) : null;

  return (
    <div>
      <PageTitle inTree title={app.name} mark={<AppLogo slug={app.logo ?? ""} name={app.name} size={48} />} action={wide ? undefined : actions} />
      <div className="flex flex-col gap-6 xl:flex-row xl:items-center xl:justify-between xl:gap-12">
        <div className="flex min-w-0 flex-col gap-1.5">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
            <StatusBadge status={health} />
            {address ? (
              <a href={`https://${address}`} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-2 break-all font-mono text-[13px] underline">
                {address}
                <Icon icon={ExternalLink} />
              </a>
            ) : null}
          </div>
          {health !== "running" && health !== "paused" && app.runDetail ? <p className="text-muted">{app.runDetail}</p> : null}
          <p className="break-all text-muted">
            {app.source.replace(/@sha256:[a-f0-9]+$/, "")} · {where}
          </p>
        </div>
        <div className="xl:w-[560px] xl:shrink-0">
          <UsageMeters sample={runtime} />
        </div>
      </div>
      {mutable ? <UpdateLock locked={locked && !transferring} /> : null}
      {mutable ? <AppUpdateNotice app={app} onStarted={() => void appPoll.reload()} /> : null}
      <div className="mt-8" />
      {planShown ? (
        <PlanPanel model={model} label={text("plan.label")} whole={text("plan.whole")} named={false} selected={planned} onSelect={setPlanned}>
          {wide ? null : <PlanDetail app={app} pods={pods} selected={planned} />}
        </PlanPanel>
      ) : null}
      {/* On a phone the plan is a list: the nav stays under the top bar. */}
      <div ref={nav} className={`${underPlan} mt-2 max-md:sticky max-md:top-12 max-md:z-[9] max-md:bg-canvas`}>
        {element && parts.length > 1 ? (
          <div className="py-1">
            <ChosenName node={element} />
          </div>
        ) : null}
        <div className="flex items-end gap-4 border-b border-line">
          <div className="-mb-px min-w-0 flex-1">
            {parts.length > 1 ? (
              <Tabs label={text("tpl.part.parts")} active={shown} onChange={setPart} small={wide} tabs={parts} />
            ) : element ? (
              <div className="py-1">
                <ChosenName node={element} />
              </div>
            ) : null}
          </div>
          {wide && actions ? <div className="pb-1">{actions}</div> : null}
        </div>
      </div>
      {/* However short a part, the page keeps its height under the nav: the
          plan stays folded and the nav in its place from one part to another. */}
      <div ref={panels} className="mt-4 max-w-[720px] md:min-h-[calc(100dvh-var(--plan-bottom,0px)-5rem)]">
        {/* A part that holds work under way stays mounted: an image or an
            address being typed, a restore being sent. */}
        <AppPart on={shown === "services"}>
          {app.guarded ? (
            <div className="flex flex-col gap-3">
              <p className="text-muted">{text("apps.selfCopies")}</p>
              {(app.services ?? []).map((service) => (
                <p key={service.name} className="font-mono text-[13px]">
                  {service.name} · {service.image} · {text(service.replicas === 1 ? "apps.copyOne" : "apps.copyMany", { count: service.replicas || 1 })}
                </p>
              ))}
            </div>
          ) : app.services && app.services.length > 0 ? (
            mutable ? <ImageEditor app={app} onChange={setApp} /> : (
              <ul className="flex flex-col gap-2">
                {app.services.map((service) => (
                  <li key={service.name} className="font-mono text-[13px] text-muted">
                    {service.name} · {service.image}
                  </li>
                ))}
              </ul>
            )
          ) : (
            <p className="text-muted">{text("apps.noService")}</p>
          )}
        </AppPart>
        {mutable ? (
          <AppPart on={shown === "backups"}>
            <AppBackups app={app} locked={locked} />
          </AppPart>
        ) : null}
        {shown === "pods" ? (
          <AppPart on>
            {serviceName ? <p className="mb-3 break-all font-mono text-[13px] text-muted">{(app.services ?? []).find((svc) => svc.name === serviceName)?.image.replace(/@sha256:[a-f0-9]+$/, "")}</p> : null}
            <AppPods appId={app.id} pods={shownPods} runtime={runtime} />
          </AppPart>
        ) : null}
        {mutable || shown === "addresses" ? (
          <AppPart on={shown === "addresses"}>
            <AppAddresses
              app={app}
              domains={shownDomains}
              publics={publics}
              service={publics.others.includes(serviceName) ? serviceName : undefined}
              editable={mutable}
              locked={locked}
              onSaved={(next) => {
                setApp(next);
                toast(text("names.saved", { name: next.name }));
              }}
            />
          </AppPart>
        ) : null}
        {shown === "exposed" ? (
          <AppPart on>
            <p className="text-muted">{text("apps.exposedHint")}</p>
            <ul className="mt-2 flex flex-col">
              {shownExposed.map((item) => (
                <li key={item.service} className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1 border-t border-line py-4">
                  <span className="font-mono text-[13px]">{item.ports.join(", ")}</span>
                  <span className="text-[13px] text-muted">{item.service}</span>
                </li>
              ))}
            </ul>
          </AppPart>
        ) : null}
        {shown === "variables" ? (
          <AppPart on>
            <AppVariables app={app} onSaved={() => void appPoll.reload()} />
          </AppPart>
        ) : null}
        {shown === "secrets" ? (
          <AppPart on>
            <AppSecretLinks appId={app.id} />
          </AppPart>
        ) : null}
        {shown === "links" && element ? (
          <AppPart on>
            <PlanLinks model={model} chosen={element.key} onSelect={setPlanned} />
          </AppPart>
        ) : null}
        {app.guarded && canDeploy ? (
          <div className="mt-8">
            <TextButton type="button" tone="danger" onClick={() => setConfirm("delete")}>
              <Icon icon={Trash2} />
              {text("common.delete")}
            </TextButton>
          </div>
        ) : null}
      </div>
      {confirm === "restart" ? (
        <ConfirmDialog
          title={text("apps.restartTitle", { name: app.name })}
          confirmLabel={text("apps.restart")}
          onConfirm={async () => {
            await api(`/api/applications/${app.id}/restart`, { method: "POST", body: "{}" });
            toast(text("apps.restarted", { name: app.name }));
            setConfirm("");
            void runtimePoll.reload();
          }}
          onClose={() => setConfirm("")}
        >
          {app.clusterId ? text("apps.restartHintCluster") : text("apps.restartHintMachine")}
        </ConfirmDialog>
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
  const { canAdmin } = useSession();
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
    <>
      <ul className="flex flex-col">
        {entries.map(([name, value]) => (
          <li key={name} className="flex flex-wrap items-center justify-between gap-6 border-t border-line py-3 first:border-t-0 first:pt-0">
            <span className="font-mono text-[13px]">{name}</span>
            <span className="flex min-w-0 items-center gap-3">
              <span className="truncate font-mono text-[13px] text-muted" title={followsAddress(value) ? value : undefined}>
                {fillAddress(shown[name] ?? value, app.address)}
              </span>
              {followsAddress(value) ? <span className="shrink-0 rounded border border-line px-1.5 text-[13px] text-faint">{text("address.follows")}</span> : null}
              {canAdmin && value === masked && shown[name] === undefined ? (
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
    </>
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
          <Code value={draft} onChange={setDraft} language="env" label={text("app.variables")} placeholder="NOM=valeur" minHeight="10rem" />
        </Field>
        {app.address ? <p className="text-faint">{text("address.hint", { address: app.address })}</p> : null}
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
