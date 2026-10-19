import { FormEvent, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { LayoutTemplate, Plus } from "lucide-react";
import {
  api,
  peek,
  type Catalog,
  type Cluster,
  type DnsZone,
  type Project,
  type Secret,
  type Template,
  type TemplateApp,
  type TemplateBody,
  type TemplateRun,
  type TemplateServer,
} from "./api";
import { CancelDeployment } from "./deployments";
import { text, currentLocale, knownMessage } from "./i18n";
import { OfferFields } from "./offers";
import { ProviderSelect } from "./provider-select";
import { ToolInstall } from "./tools";
import {
  ActionsMenu,
  Button,
  EmptyList,
  ErrorText,
  Field,
  Fold,
  Icon,
  Input,
  LoadingLines,
  MenuItem,
  PageTitle,
  SectionTitle,
  Select,
  TextArea,
  TextButton,
} from "./ui";

const emptyServer = (): TemplateServer => ({ name: "", period: "hour", keyIds: [] });
const emptyApp = (): TemplateApp => ({ name: "", sourceKind: "image", source: "", replicas: 1, names: [], services: [] });

export function Templates() {
  const navigate = useNavigate();
  const [rows, setRows] = useState<Template[] | null>(() => peek<Template[]>("/api/templates"));
  const [error, setError] = useState("");
  const [removing, setRemoving] = useState<Template | null>(null);
  const [typed, setTyped] = useState("");

  function reload() {
    return api<Template[]>("/api/templates").then(setRows);
  }

  useEffect(() => {
    reload().catch((err) => setError(err.message));
  }, []);

  async function upload(file: File) {
    setError("");
    try {
      const doc = JSON.parse(await file.text()) as unknown;
      const created = await api<Template>("/api/templates/import", { method: "POST", body: JSON.stringify(doc) });
      navigate(`/templates/${created.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("ui.ceFichierEstIllisible"));
    }
  }

  async function remove(event: FormEvent) {
    event.preventDefault();
    if (!removing || typed !== removing.name) return;
    await api(`/api/templates/${removing.id}`, { method: "DELETE" });
    setRemoving(null);
    await reload();
  }

  return (
    <>
      <PageTitle
        title={text("nav.templates")}
        icon={LayoutTemplate}
        action={
          rows && rows.length > 0 ? (
            <span className="flex items-center gap-4">
              <TextButton type="button" onClick={() => document.getElementById("template-file")?.click()}>
                {text("cluster.import")}
              </TextButton>
              <Button type="button" onClick={() => navigate("/templates/nouveau")}>
                <Icon icon={Plus} />
                {text("crumb.newTemplate")}
              </Button>
            </span>
          ) : null
        }
      />
      <input
        id="template-file"
        type="file"
        accept="application/json,.json"
        className="hidden"
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) void upload(file);
        }}
      />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {rows === null ? (
        <LoadingLines lines={4} />
      ) : rows.length === 0 ? (
        <div className="flex flex-col items-start gap-6">
          <EmptyList message={text("template.empty")} label={text("template.new")} onCreate={() => navigate("/templates/nouveau")} />
          <TextButton type="button" onClick={() => document.getElementById("template-file")?.click()}>
            {text("cluster.import")}
          </TextButton>
        </div>
      ) : (
        <ul className="flex flex-col gap-4">
          {rows.map((row) => (
            <li key={row.id} className="relative rounded-md border border-line hover:bg-status">
              <Link to={`/templates/${row.id}`} className="block p-4 pr-14 text-ink">
                <span>{row.name}</span>
                <p className="mt-2 text-muted">{summary(row.body)}</p>
              </Link>
              <div className="absolute right-3 top-3">
                <ActionsMenu label={`Actions de ${row.name}`}>
                  <MenuItem onClick={() => navigate(`/templates/${row.id}/deploy`)}>{text("template.deploy")}</MenuItem>
                  <MenuItem onClick={() => void download(row)}>{text("template.export")}</MenuItem>
                  <MenuItem
                    onClick={() => {
                      setTyped("");
                      setRemoving(row);
                    }}
                  >
                    {text("common.delete")}
                  </MenuItem>
                </ActionsMenu>
              </div>
            </li>
          ))}
        </ul>
      )}
      {removing ? (
        <div className="fixed inset-0 z-30 flex items-center justify-center bg-ink/40 px-6" role="dialog" aria-modal="true">
          <form onSubmit={remove} className="w-full max-w-md rounded-md border border-line bg-surface p-6">
            <h2 className="mb-3 text-lg">Supprimer {removing.name}</h2>
            <p className="mb-6 text-muted">{text("template.deleteHint")}</p>
            <Field label={text("template.name")}>
              <Input value={typed} autoFocus onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
            </Field>
            <div className="mt-6 flex items-center gap-4">
              <Button type="submit" disabled={typed !== removing.name}>
                {text("common.delete")}
              </Button>
              <TextButton type="button" onClick={() => setRemoving(null)}>
                {text("common.cancel")}
              </TextButton>
            </div>
          </form>
        </div>
      ) : null}
    </>
  );
}

export function TemplateEditor() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [body, setBody] = useState<TemplateBody>({ servers: [], applications: [] });
  const [providers, setProviders] = useState<Secret[]>([]);
  const [keys, setKeys] = useState<Secret[]>([]);
  const [zones, setZones] = useState<DnsZone[]>([]);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(!id);

  useEffect(() => {
    api<Secret[]>("/api/secrets")
      .then((secrets) => {
        setProviders(secrets.filter((secret) => secret.kind === "provider_token"));
        setKeys(secrets.filter((secret) => secret.kind === "ssh_key"));
      })
      .catch((err) => setError(err.message));
    api<DnsZone[]>("/api/dns/zones")
      .then(setZones)
      .catch(() => setZones([]));
  }, []);

  useEffect(() => {
    if (!id) return;
    api<Template>(`/api/templates/${id}`)
      .then((row) => {
        setName(row.name);
        setBody({
          clusterName: row.body.clusterName ?? "",
          servers: row.body.servers ?? [],
          applications: row.body.applications ?? [],
        });
      })
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
  }, [id]);

  async function save(event: FormEvent) {
    event.preventDefault();
    setError("");
    const payload = { name, ...body };
    try {
      const saved = id
        ? await api<Template>(`/api/templates/${id}`, { method: "PUT", body: JSON.stringify(payload) })
        : await api<Template>("/api/templates", { method: "POST", body: JSON.stringify(payload) });
      navigate(`/templates/${saved.id}/deploy`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  if (!ready) return <LoadingLines lines={4} />;
  const servers = body.servers ?? [];
  const apps = body.applications ?? [];

  return (
    <>
      <PageTitle title={id ? name || "Template" : text("crumb.newTemplate")} icon={LayoutTemplate} />
      <form onSubmit={save} className="flex max-w-[720px] flex-col gap-10">
        <Field label={text("template.name")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <section className="flex flex-col gap-4">
          <SectionTitle>{text("field.cluster")}</SectionTitle>
          <Field label={text("cluster.name")}>
            <Input
              value={body.clusterName ?? ""}
              placeholder={text("ui.videPourChoisirUnClusterAu")}
              onChange={(e) => setBody({ ...body, clusterName: e.target.value })}
            />
          </Field>
        </section>
        <section className="flex flex-col gap-4">
          <SectionTitle>{text("cluster.servers")}</SectionTitle>
          {servers.map((server, index) => (
            <div key={index} className="flex flex-col gap-4 rounded-md border border-line p-4">
              <MachineFields
                server={server}
                providers={providers}
                keys={keys}
                onChange={(next) => setBody({ ...body, servers: servers.map((item, i) => (i === index ? next : item)) })}
              />
              <TextButton type="button" onClick={() => setBody({ ...body, servers: servers.filter((_, i) => i !== index) })}>
                {text("common.remove")}
              </TextButton>
            </div>
          ))}
          <div>
            <TextButton
              type="button"
              onClick={() => setBody({ ...body, servers: [...servers, { ...emptyServer(), controlPlane: !servers.some((server) => server.controlPlane) }] })}
            >
              <Icon icon={Plus} />
              {text("ui.ajouterUnServeur")}
            </TextButton>
          </div>
          {servers.length > 0 ? <p className="text-faint">{text("template.planesHint")}</p> : null}
          {servers.filter((server) => server.controlPlane).length % 2 === 0 && servers.some((server) => server.controlPlane) ? (
            <p className="text-warn">{text("server.evenPlanes")}</p>
          ) : null}
        </section>
        <section className="flex flex-col gap-4">
          <SectionTitle>{text("nav.apps")}</SectionTitle>
          {apps.map((app, index) => (
            <div key={index} className="flex flex-col gap-4 rounded-md border border-line p-4">
              <AppFields
                app={app}
                zones={zones}
                onChange={(next) => setBody({ ...body, applications: apps.map((item, i) => (i === index ? next : item)) })}
              />
              <TextButton type="button" onClick={() => setBody({ ...body, applications: apps.filter((_, i) => i !== index) })}>
                {text("common.remove")}
              </TextButton>
            </div>
          ))}
          <div>
            <TextButton type="button" onClick={() => setBody({ ...body, applications: [...apps, emptyApp()] })}>
              <Icon icon={Plus} />
              {text("ui.ajouterUneApplication")}
            </TextButton>
          </div>
        </section>
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div>
          <Button type="submit">{text("common.save")}</Button>
        </div>
      </form>
    </>
  );
}

export function TemplateLaunch() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [template, setTemplate] = useState<Template | null>(null);
  const [body, setBody] = useState<TemplateBody>({});
  const [target, setTarget] = useState("");
  const [phase, setPhase] = useState<"fill" | "summary">("fill");
  const [providers, setProviders] = useState<Secret[]>([]);
  const [keys, setKeys] = useState<Secret[]>([]);
  const [zones, setZones] = useState<DnsZone[]>([]);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);

  useEffect(() => {
    Promise.all([
      api<Template>(`/api/templates/${id}`),
      api<Secret[]>("/api/secrets"),
      api<DnsZone[]>("/api/dns/zones").catch(() => [] as DnsZone[]),
      api<Cluster[]>("/api/clusters"),
    ])
      .then(([row, secrets, nextZones, nextClusters]) => {
        const next = {
          clusterName: row.body.clusterName ?? "",
          servers: row.body.servers ?? [],
          applications: row.body.applications ?? [],
        };
        setTemplate(row);
        setBody(next);
        setProviders(secrets.filter((secret) => secret.kind === "provider_token"));
        setKeys(secrets.filter((secret) => secret.kind === "ssh_key"));
        setZones(nextZones);
        setClusters(nextClusters);
        if (!hasGaps(next, nextZones) && !needsCluster(next)) setPhase("summary");
      })
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
  }, [id]);

  async function validate() {
    setError("");
    try {
      const run = await api<TemplateRun>(`/api/templates/${id}/runs`, {
        method: "POST",
        body: JSON.stringify({ ...body, targetClusterId: body.clusterName ? "" : target }),
      });
      navigate(`/templates/suivis/${run.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  if (!ready) return <LoadingLines lines={4} />;
  if (!template) return <ErrorText>{error || "Introuvable."}</ErrorText>;
  const servers = body.servers ?? [];
  const apps = body.applications ?? [];
  const missing = hasGaps(body, zones);
  const choices = clusters.filter((cluster) => (servers.length > 0 ? cluster.origin !== "imported" : true));

  return (
    <>
      <PageTitle title={template.name} icon={LayoutTemplate} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {phase === "fill" ? (
        <form
          className="flex max-w-[720px] flex-col gap-8"
          onSubmit={(event) => {
            event.preventDefault();
            if (!missing && (body.clusterName || target)) setPhase("summary");
          }}
        >
          <SectionTitle>{text("template.complete")}</SectionTitle>
          {needsCluster(body) ? (
            choices.length === 0 ? (
              <p className="text-muted">{text("template.noCluster")}</p>
            ) : (
              <Field label={text("field.cluster")}>
                <Select value={target} onChange={(e) => setTarget(e.target.value)} required>
                  <option value="">{text("common.choose")}</option>
                  {choices.map((cluster) => (
                    <option key={cluster.id} value={cluster.id}>
                      {cluster.name}
                    </option>
                  ))}
                </Select>
              </Field>
            )
          ) : null}
          {servers.map((server, index) =>
            serverReady(server) ? null : (
              <div key={index} className="flex flex-col gap-4 rounded-md border border-line p-4">
                <MachineFields
                  server={server}
                  providers={providers}
                  keys={keys}
                  onChange={(next) => setBody({ ...body, servers: servers.map((item, i) => (i === index ? next : item)) })}
                />
              </div>
            ),
          )}
          {apps.map((app, index) => {
            const open = (app.names ?? []).some((name) => name.subdomain && !knownZone(name.zone, zones));
            if (!open) return null;
            return (
              <AppFields
                key={index}
                app={app}
                zones={zones}
                onChange={(next) => setBody({ ...body, applications: apps.map((item, i) => (i === index ? next : item)) })}
              />
            );
          })}
          <div>
            <Button type="submit" disabled={missing || (needsCluster(body) && !target)}>
              {text("auth.continue")}
            </Button>
          </div>
        </form>
      ) : (
        <div className="flex max-w-[720px] flex-col gap-8">
          <SectionTitle>{text("template.summary")}</SectionTitle>
          {body.clusterName ? (
            <Field label={text("template.createdCluster")}>
              <Input value={body.clusterName} onChange={(e) => setBody({ ...body, clusterName: e.target.value })} required />
            </Field>
          ) : (
            <p>Cluster {choices.find((cluster) => cluster.id === target)?.name}</p>
          )}
          {servers.map((server, index) => {
            const marked = servers.some((item) => item.controlPlane);
            const control = server.controlPlane || (!marked && index === 0 && Boolean(body.clusterName));
            return (
            <div key={index} className="flex flex-col gap-2 border-t border-line pt-4">
              <Field label={control ? "Control plane" : "Serveur"}>
                <Input
                  value={server.name}
                  onChange={(e) => setBody({ ...body, servers: servers.map((item, i) => (i === index ? { ...item, name: e.target.value } : item)) })}
                  required
                />
              </Field>
              <ServerLine server={server} providers={providers} keys={keys} />
            </div>
            );
          })}
          {apps.map((app, index) => (
            <div key={index} className="flex flex-col gap-2 border-t border-line pt-4">
              <Field label={text("crumb.application")}>
                <Input
                  value={app.name}
                  onChange={(e) =>
                    setBody({ ...body, applications: apps.map((item, i) => (i === index ? { ...item, name: e.target.value } : item)) })
                  }
                  required
                />
              </Field>
              <p className="text-muted">
                {app.sourceKind === "git" ? text("ui.depot") : text("common.image")} {app.source}
                {(app.replicas ?? 1) > 1 ? ` · Redondance ${app.replicas}` : ""}
              </p>
              {(app.services ?? []).map((service) => (
                <p key={service.name} className="text-muted">
                  {service.name} · {service.image}
                  {service.published ? " · publié" : " · interne"}
                  {service.disk ? " · disque" : ""}
                </p>
              ))}
              {(app.names ?? []).map((name, nameIndex) => (
                <p key={nameIndex} className="text-muted">
                  {name.subdomain}.{name.zone}
                </p>
              ))}
            </div>
          ))}
          <Cost servers={servers} />
          <ToolInstall
            ansible={servers.length > 0}
            tofu={servers.some((server) => {
              const kind = server.provider || providers.find((item) => item.id === server.providerId)?.provider;
              return kind === "hetzner" || kind === "scaleway";
            })}
          />
          <div className="flex items-center gap-4">
            <Button type="button" onClick={() => void validate()}>
              {text("ui.valider")}
            </Button>
            {missing || needsCluster(body) ? (
              <TextButton type="button" onClick={() => setPhase("fill")}>
                {text("ui.retour")}
              </TextButton>
            ) : null}
          </div>
        </div>
      )}
    </>
  );
}

export function TemplateRunPage() {
  const { id = "" } = useParams();
  const [run, setRun] = useState<TemplateRun | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let stop = false;
    const load = () => {
      api<TemplateRun>(`/api/template-runs/${id}`)
        .then((next) => {
          if (!stop) setRun(next);
        })
        .catch((err) => {
          if (!stop) setError(err.message);
        });
    };
    load();
    const timer = window.setInterval(load, 3000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [id]);

  async function resume() {
    setError("");
    try {
      setRun(await api<TemplateRun>(`/api/template-runs/${id}/resume`, { method: "POST" }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  if (!run && !error) return <LoadingLines lines={4} />;
  if (!run) return <ErrorText>{error}</ErrorText>;
  const label = run.status === "done" ? "Terminé" : run.status === "failed" ? "Échec" : run.status === "cancelled" ? text("deploy.cancelled") : text("ui.enCours");

  return (
    <>
      <PageTitle title={text("template.follow")} icon={LayoutTemplate} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      <p className={`mb-8 ${run.status === "failed" ? "text-bad" : run.status === "done" ? "text-ok" : "text-muted"}`}>{label}</p>
      <ol className="max-w-[720px]">
        {run.steps.map((step, index) => (
          <li key={index} className="border-t border-line py-4">
            <div className="flex items-baseline justify-between gap-4">
              <span>
                {step.kind === "server" && step.resourceId ? (
                  <Link to={`/serveurs/${step.resourceId}`} className="underline">
                    {step.name}
                  </Link>
                ) : step.kind === "application" && step.resourceId ? (
                  <Link to={`/applications/${step.resourceId}`} className="underline">
                    {step.name}
                  </Link>
                ) : (
                  step.name
                )}
              </span>
              <span className={step.status === "failed" ? "text-bad" : step.status === "done" ? "text-ok" : "text-muted"}>{stepText(step.status)}</span>
            </div>
            {step.detail ? <p className="mt-1 text-faint">{knownMessage(step.detail)}</p> : null}
            {step.deploy && step.deploy.length > 0 ? (
              <ul className="mt-3 flex flex-col gap-1">
                {step.deploy.map((item) => (
                  <li key={item.id} className={item.status === "failed" ? "text-bad" : item.status === "done" ? "text-ok" : "text-muted"}>
                    {item.label}
                    {item.detail ? ` · ${knownMessage(item.detail)}` : ""}
                  </li>
                ))}
              </ul>
            ) : null}
          </li>
        ))}
      </ol>
      {run.status === "failed" ? (
        <div className="mt-8">
          <Button type="button" onClick={() => void resume()}>
            {text("home.resume")}
          </Button>
        </div>
      ) : (
        <CancelDeployment kind="template" id={id} active={run.status === "running"} />
      )}
    </>
  );
}

function MachineFields({
  server,
  providers,
  keys,
  onChange,
}: {
  server: TemplateServer;
  providers: Secret[];
  keys: Secret[];
  onChange: (server: TemplateServer) => void;
}) {
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsReady, setProjectsReady] = useState(false);
  const [offersLoading, setOffersLoading] = useState(false);
  const [error, setError] = useState("");
  const serverRef = useRef(server);
  const changeRef = useRef(onChange);
  serverRef.current = server;
  changeRef.current = onChange;

  useEffect(() => {
    // Another account has other places: do not show the previous ones meanwhile.
    setCatalog(null);
    if (!server.providerId) {
      setProjects([]);
      setProjectsReady(true);
      return;
    }
    let cancel = false;
    setProjectsReady(false);
    api<Project[]>(`/api/providers/${server.providerId}/projects`)
      .then((rows) => {
        if (cancel) return;
        setProjects(rows);
        const current = serverRef.current;
        if (rows.length > 0 && !rows.some((item) => item.id === current.projectId)) {
          changeRef.current({ ...current, projectId: rows[0].id });
        }
      })
      .catch((err) => {
        if (!cancel) {
          setProjects([]);
          setError(err instanceof Error ? err.message : text("common.error"));
        }
      })
      .finally(() => {
        if (!cancel) setProjectsReady(true);
      });
    return () => {
      cancel = true;
    };
  }, [server.providerId]);

  useEffect(() => {
    if (!server.providerId) {
      setCatalog(null);
      setOffersLoading(false);
      return;
    }
    if (!projectsReady || (projects.length > 0 && !server.projectId)) {
      setOffersLoading(true);
      return;
    }
    let cancel = false;
    setOffersLoading(true);
    const params = new URLSearchParams();
    if (server.location) params.set("location", server.location);
    if (server.projectId) params.set("project", server.projectId);
    const query = params.toString() ? `?${params}` : "";
    api<Catalog>(`/api/providers/${server.providerId}/offers${query}`)
      .then((next) => {
        if (cancel) return;
        setCatalog(next);
        setError("");
        const current = serverRef.current;
        if (!current.location && next.location) changeRef.current({ ...current, location: next.location });
      })
      .catch((err) => {
        if (!cancel) setError(err instanceof Error ? err.message : text("common.error"));
      })
      .finally(() => {
        if (!cancel) setOffersLoading(false);
      });
    return () => {
      cancel = true;
    };
  }, [server.providerId, server.location, server.projectId, projectsReady, projects.length]);

  const offer = catalog?.offers.find((item) => item.id === server.offerId || item.monthlyId === server.offerId);
  const selectedKeys = server.keyIds ?? [];

  return (
    <>
      <Field label={text("field.name")}>
        <Input value={server.name} onChange={(e) => onChange({ ...server, name: e.target.value })} required />
      </Field>
      <label className="flex items-center gap-2">
        <input type="checkbox" checked={Boolean(server.controlPlane)} onChange={(e) => onChange({ ...server, controlPlane: e.target.checked })} />
        Control plane
      </label>
      <ProviderSelect
        label={text("ui.provider")}
        value={server.providerId ?? ""}
        options={providers.map((provider) => ({ value: provider.id, label: provider.name, logo: provider.provider }))}
        onChange={(value) => {
          const account = providers.find((item) => item.id === value);
          onChange({ ...server, providerId: value, provider: account?.provider, location: "", projectId: "", offerId: "", storageId: "", storageGb: 0 });
        }}
      />
      {error ? <ErrorText>{error}</ErrorText> : null}
      {projectsReady && projects.length > 0 ? (
        <Field label={text("provider.project")}>
          <Select value={server.projectId ?? ""} onChange={(e) => onChange({ ...server, projectId: e.target.value, offerId: "" })}>
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
          <Select value={server.location || catalog.location} onChange={(e) => onChange({ ...server, location: e.target.value, offerId: "" })}>
            {catalog.locations.map((item) => (
              <option key={item.id} value={item.id}>
                {item.label}
              </option>
            ))}
          </Select>
        </Field>
      ) : null}
      {server.providerId && (offersLoading || !projectsReady) ? (
        <LoadingLines lines={3} label={text("transfer.offers")} />
      ) : catalog ? (
        <>
          <OfferFields
            offers={catalog.offers}
            value={offer?.id ?? ""}
            currency={catalog.currency}
            allowEmpty
            onChange={(id) => {
              const chosen = catalog.offers.find((item) => item.id === id);
              const disk = chosen?.storage?.find((item) => item.required);
              onChange({
                ...server,
                offerId: id,
                location: server.location || catalog.location,
                storageId: disk?.id ?? "",
                storageGb: disk?.minGb ?? 0,
              });
            }}
          />
          <Field label={text("field.period")}>
            <Select value={server.period || catalog.periods[0] || "hour"} onChange={(e) => onChange({ ...server, period: e.target.value })}>
              {catalog.periods.map((period) => (
                <option key={period} value={period}>
                  {period === "month" ? text("ui.mois") : text("ui.heure")}
                </option>
              ))}
            </Select>
          </Field>
          {offer ? <StorageFields offer={offer} server={{ ...server, location: server.location || catalog.location }} currency={catalog.currency} onChange={onChange} /> : null}
        </>
      ) : null}
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-muted">{text("nav.ssh")}</legend>
        {keys.map((key) => (
          <label key={key.id} className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={selectedKeys.includes(key.id)}
              onChange={(e) =>
                onChange({
                  ...server,
                  keyIds: e.target.checked ? [...selectedKeys, key.id] : selectedKeys.filter((item) => item !== key.id),
                  keyNames: undefined,
                })
              }
            />
            {key.name}
          </label>
        ))}
      </fieldset>
    </>
  );
}

function StorageFields({
  offer,
  server,
  currency,
  onChange,
}: {
  offer: Catalog["offers"][number];
  server: TemplateServer;
  currency: string;
  onChange: (server: TemplateServer) => void;
}) {
  const required = offer.storage?.find((item) => item.required);
  const optional = (offer.storage ?? []).filter((item) => !item.required);
  const selected = required ?? offer.storage?.find((item) => item.id === server.storageId);
  if (!required && optional.length === 0) return null;
  return (
    <>
      {required ? null : (
        <Field label={text("field.extraStorage")}>
          <Select
            value={server.storageId ?? ""}
            onChange={(e) => {
              const next = optional.find((item) => item.id === e.target.value);
              onChange({ ...server, storageId: e.target.value, storageGb: next?.minGb ?? 0 });
            }}
          >
            <option value="">{text("common.none")}</option>
            {optional.map((item) => (
              <option key={item.id} value={item.id}>
                {item.label}
              </option>
            ))}
          </Select>
        </Field>
      )}
      {selected ? (
        <Field label={text("field.sizeGb")}>
          <Input
            type="number"
            min={selected.minGb}
            max={selected.maxGb || undefined}
            value={server.storageGb || selected.minGb}
            onChange={(e) => onChange({ ...server, storageId: selected.id, storageGb: Number(e.target.value) })}
            required
          />
          <span className="text-muted">
            {server.period === "month" && selected.monthlyPerGb ? price(selected.monthlyPerGb, false, currency) + "/Go" : ""}
            {server.period !== "month" && selected.hourlyPerGb ? price(selected.hourlyPerGb, true, currency) + "/Go" : ""}
          </span>
        </Field>
      ) : null}
    </>
  );
}

function AppFields({ app, zones, onChange }: { app: TemplateApp; zones: DnsZone[]; onChange: (app: TemplateApp) => void }) {
  const names = app.names ?? [];
  const services = app.services ?? [];
  const variables = Object.entries(app.variables ?? {})
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
  return (
    <>
      <Field label={text("field.name")}>
        <Input value={app.name} onChange={(e) => onChange({ ...app, name: e.target.value })} required />
      </Field>
      <Field label={text("field.source")}>
        <Select value={app.sourceKind} onChange={(e) => onChange({ ...app, sourceKind: e.target.value })}>
          <option value="image">{text("common.image")}</option>
          <option value="git">{text("ui.depot")}</option>
        </Select>
      </Field>
      <Field label={app.sourceKind === "git" ? text("ui.depot") : text("common.image")}>
        <Input value={app.source} onChange={(e) => onChange({ ...app, source: e.target.value })} required />
      </Field>
      {names.map((name, index) => (
        <div key={index} className="flex items-stretch">
          <input
            value={name.subdomain}
            placeholder="sous-domaine"
            onChange={(e) => onChange({ ...app, names: names.map((item, i) => (i === index ? { ...item, subdomain: e.target.value } : item)) })}
            className="min-w-0 flex-1 rounded-l-md rounded-r-none border border-line bg-surface px-3 py-2 outline-none focus:border-focus"
          />
          <span className="flex items-center border-y border-line bg-surface px-2 text-muted">.</span>
          <select
            value={name.zone}
            onChange={(e) => onChange({ ...app, names: names.map((item, i) => (i === index ? { ...item, zone: e.target.value } : item)) })}
            className="max-w-[55%] rounded-l-none rounded-r-md border border-l-0 border-line bg-surface px-3 py-2 outline-none focus:border-focus"
          >
            <option value="">{text("field.domain")}</option>
            {zones.map((zone) => (
              <option key={zone.accountId + zone.name} value={zone.name}>
                {zone.name}
              </option>
            ))}
          </select>
          <span className="ml-3 self-center">
            <TextButton type="button" onClick={() => onChange({ ...app, names: names.filter((_, i) => i !== index) })}>
              {text("common.remove")}
            </TextButton>
          </span>
        </div>
      ))}
      <div>
        <TextButton type="button" onClick={() => onChange({ ...app, names: [...names, { subdomain: "", zone: zones[0]?.name ?? "" }] })}>
          <Icon icon={Plus} />
          {text("field.addName")}
        </TextButton>
      </div>
      <Fold title={text("field.services")}>
        <div className="flex flex-col gap-3">
          {services.map((service, index) => (
            <div key={index} className="flex flex-col gap-2 rounded-md border border-line p-3">
              <Input value={service.name} placeholder="nom" onChange={(e) => onChange({ ...app, services: services.map((item, i) => (i === index ? { ...item, name: e.target.value } : item)) })} />
              <Input value={service.image} placeholder="image" onChange={(e) => onChange({ ...app, services: services.map((item, i) => (i === index ? { ...item, image: e.target.value } : item)) })} />
              <label className="flex items-center gap-2 text-muted">
                <input type="checkbox" checked={service.published} onChange={() => onChange({ ...app, services: services.map((item, i) => ({ ...item, published: i === index })) })} />
                {text("status.ready")}
              </label>
              <label className="flex items-center gap-2 text-muted">
                <input
                  type="checkbox"
                  checked={service.disk}
                  onChange={(e) => onChange({ ...app, services: services.map((item, i) => (i === index ? { ...item, disk: e.target.checked } : item)) })}
                />
                {text("common.disk")}
              </label>
            </div>
          ))}
          <TextButton type="button" onClick={() => onChange({ ...app, services: [...services, { name: "", image: "", published: services.length === 0, disk: false }] })}>
            <Icon icon={Plus} />
            {text("ui.ajouterUnService")}
          </TextButton>
        </div>
      </Fold>
      <Field label={text("field.redundancy")}>
        <Input
          value={String(app.replicas ?? 1)}
          inputMode="numeric"
          onChange={(e) => onChange({ ...app, replicas: Number(e.target.value) || 1 })}
        />
      </Field>
      <p className="text-faint">{text("app.replicasHint")}</p>
      <Field label={text("app.variables")}>
        <TextArea
          value={variables}
          placeholder={"NOM=valeur"}
          onChange={(e) => onChange({ ...app, variables: parseVariables(e.target.value) })}
        />
      </Field>
    </>
  );
}

function ServerLine({ server, providers, keys }: { server: TemplateServer; providers: Secret[]; keys: Secret[] }) {
  const provider = providers.find((item) => item.id === server.providerId);
  const named = (server.keyIds ?? []).map((id, index) => keys.find((key) => key.id === id)?.name ?? server.keyNames?.[index] ?? "");
  const [quote, setQuote] = useState("");

  useEffect(() => {
    if (!server.providerId || !server.offerId) return;
    let cancel = false;
    const params = new URLSearchParams();
    if (server.location) params.set("location", server.location);
    if (server.projectId) params.set("project", server.projectId);
    const query = params.toString() ? `?${params}` : "";
    api<Catalog>(`/api/providers/${server.providerId}/offers${query}`)
      .then((catalog) => {
        if (cancel) return;
        const found = priceOf(catalog, server);
        setQuote(found ? price(String(found.amount), found.period !== "month", found.currency) : "");
      })
      .catch(() => {
        if (!cancel) setQuote("");
      });
    return () => {
      cancel = true;
    };
  }, [server]);

  return (
    <p className="text-muted">
      {provider?.name || server.providerName || text("ui.provider")}
      {server.location ? ` · ${server.location}` : ""}
      {server.offerId ? ` · ${server.offerId}` : ""}
      {server.period === "month" ? " · mois" : server.period ? " · heure" : ""}
      {quote ? ` · ${quote}` : ""}
      {named.filter(Boolean).length > 0 ? ` · ${named.filter(Boolean).join(", ")}` : ""}
    </p>
  );
}

function Cost({ servers }: { servers: TemplateServer[] }) {
  const [totals, setTotals] = useState<{ currency: string; period: string; amount: number }[] | null>(null);
  const [error, setError] = useState("");

  const signature = servers.map((server) => [server.providerId, server.location, server.projectId, server.offerId, server.period, server.storageId, server.storageGb].join(":")).join("|");

  useEffect(() => {
    if (servers.length === 0) {
      setTotals([]);
      return;
    }
    let cancel = false;
    const groups = new Map<string, { providerId: string; location: string; projectId: string }>();
    for (const server of servers) {
      if (!server.providerId) continue;
      groups.set(`${server.providerId}|${server.location ?? ""}|${server.projectId ?? ""}`, {
        providerId: server.providerId,
        location: server.location ?? "",
        projectId: server.projectId ?? "",
      });
    }
    Promise.all(
      [...groups.values()].map((group) => {
        const params = new URLSearchParams();
        if (group.location) params.set("location", group.location);
        if (group.projectId) params.set("project", group.projectId);
        const query = params.toString() ? `?${params}` : "";
        return api<Catalog>(`/api/providers/${group.providerId}/offers${query}`).then((catalog) => ({ ...group, catalog }));
      }),
    )
      .then((catalogs) => {
        if (cancel) return;
        const sums = new Map<string, { currency: string; period: string; amount: number }>();
        for (const server of servers) {
          const found = catalogs.find((item) => item.providerId === server.providerId && item.location === (server.location ?? "") && item.projectId === (server.projectId ?? ""));
          const quote = found ? priceOf(found.catalog, server) : null;
          if (!quote) {
            setError(text("ui.lePrixDUneMachineEst"));
            setTotals(null);
            return;
          }
          const key = `${quote.currency}|${quote.period}`;
          const current = sums.get(key) ?? { currency: quote.currency, period: quote.period, amount: 0 };
          current.amount += quote.amount;
          sums.set(key, current);
        }
        setError("");
        setTotals([...sums.values()]);
      })
      .catch((err) => {
        if (!cancel) setError(err instanceof Error ? err.message : text("common.error"));
      });
    return () => {
      cancel = true;
    };
  }, [signature]);

  if (servers.length === 0) return <p className="text-muted">{text("template.noMachine")}</p>;
  if (error) return <ErrorText>{error}</ErrorText>;
  if (!totals) return <LoadingLines lines={1} label={text("template.estimate")} />;
  return (
    <div>
      <p className="mb-2">{text("template.estimate")}</p>
      {totals.map((total) => (
        <p key={total.currency + total.period} className="text-muted">
          {price(String(total.amount), total.period !== "month", total.currency)}
        </p>
      ))}
      <p className="mt-2 text-faint">{text("template.price")}</p>
    </div>
  );
}

function priceOf(catalog: Catalog, server: TemplateServer) {
  const offer = catalog.offers.find((item) => item.id === server.offerId || item.monthlyId === server.offerId);
  if (!offer) return null;
  const month = server.period === "month";
  const base = Number(month ? offer.monthly : offer.hourly);
  if (!Number.isFinite(base)) return null;
  const storage = offer.storage?.find((item) => item.id === server.storageId);
  const per = storage ? Number(month ? storage.monthlyPerGb : storage.hourlyPerGb) : 0;
  const extra = storage && server.storageGb && Number.isFinite(per) ? per * server.storageGb : 0;
  return { amount: base + extra, currency: catalog.currency, period: month ? "month" : "hour" };
}

function hasGaps(body: TemplateBody, zones: DnsZone[]) {
  const servers = body.servers ?? [];
  const apps = body.applications ?? [];
  if (servers.some((server) => !serverReady(server))) return true;
  return apps.some((app) => (app.names ?? []).some((name) => name.subdomain.trim() !== "" && !knownZone(name.zone, zones)));
}

function needsCluster(body: TemplateBody) {
  return !(body.clusterName ?? "").trim() && ((body.servers ?? []).length > 0 || (body.applications ?? []).length > 0);
}

function serverReady(server: TemplateServer) {
  const ids = server.keyIds ?? [];
  const names = server.keyNames ?? [];
  const needs = server.provider === "ovh" || server.provider === "digitalocean";
  return Boolean(server.providerId && server.offerId && server.location && server.period && (!needs || server.projectId) && ids.length > 0 && names.length <= ids.length);
}

function knownZone(zone: string, zones: DnsZone[]) {
  if (!zone) return false;
  return zones.some((item) => item.name.toLowerCase() === zone.toLowerCase());
}

function summary(body: TemplateBody) {
  const servers = body.servers?.length ?? 0;
  const apps = body.applications?.length ?? 0;
  const cluster = body.clusterName ? `Cluster ${body.clusterName}` : text("ui.clusterAuLancement");
  return `${cluster} · ${servers} serveur${servers > 1 ? "s" : ""} · ${apps} application${apps > 1 ? "s" : ""}`;
}

function stepText(status: string) {
  if (status === "cancelled") return text("deploy.cancelled");
  if (status === "done") return "Fait";
  if (status === "failed") return "Échec";
  if (status === "running") return text("ui.enCours");
  return text("status.pending");
}

function parseVariables(value: string) {
  const vars: Record<string, string> = {};
  for (const line of value.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const cut = trimmed.indexOf("=");
    if (cut <= 0) continue;
    vars[trimmed.slice(0, cut)] = trimmed.slice(cut + 1);
  }
  return vars;
}

function price(raw: string | undefined, hour: boolean, currency = "EUR") {
  if (!raw) return "";
  const value = Number(raw);
  if (!Number.isFinite(value)) return raw;
  const digits = value < 0.001 ? 6 : hour && value < 1 ? 4 : 2;
  const text = value.toLocaleString(currentLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
  const unit = currency === "USD" ? "$" : "€";
  return hour ? `${text} ${unit}/h` : `${text} ${unit}/mois`;
}

async function download(row: Template) {
  const res = await fetch(`/api/templates/${row.id}/export`);
  if (!res.ok) return;
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `${row.name}.json`;
  link.click();
  URL.revokeObjectURL(url);
}

