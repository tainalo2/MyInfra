import { FormEvent, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { LayoutTemplate, Plus } from "lucide-react";
import {
  api,
  peek,
  type Catalog,
  type Package,
  type PackageRow,
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
import { text, knownMessage } from "./i18n";
import { Code } from "./code-lazy";
import { OfferFields, OfferStorage, formatPrice } from "./offers";
import { ConfirmDialog, Sheet, Spinner, Steps, useToast } from "./kit";
import { usePoll } from "./poll";
import { AppLogo } from "./app-logo";
import { useI18n } from "./lang";
import type { MsgKey } from "./i18n";
import { NameList } from "./names";
import { ProviderSelect } from "./provider-select";
import { useSession } from "./session";
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
  TextButton,
} from "./ui";

const emptyServer = (): TemplateServer => ({ name: "", period: "hour", keyIds: [] });
const emptyApp = (): TemplateApp => ({ name: "", sourceKind: "image", source: "", replicas: 1, names: [], services: [] });

export function Templates() {
  const navigate = useNavigate();
  const { canDeploy } = useSession();
  const [rows, setRows] = useState<Template[] | null>(() => peek<Template[]>("/api/templates"));
  const [error, setError] = useState("");
  const [removing, setRemoving] = useState<Template | null>(null);
  const toast = useToast();

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

  async function remove(row: Template) {
    await api(`/api/templates/${row.id}`, { method: "DELETE" });
    setRows((current) => current?.filter((item) => item.id !== row.id) ?? current);
    setRemoving(null);
    toast(text("template.removed", { name: row.name }));
    void reload().catch(() => undefined);
  }

  return (
    <>
      <PageTitle
        title={text("nav.templates")}
        icon={LayoutTemplate}
        action={
          rows && rows.length > 0 && canDeploy ? (
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
        canDeploy ? (
          <div className="flex flex-col items-start gap-6">
            <EmptyList message={text("template.empty")} label={text("template.new")} onCreate={() => navigate("/templates/nouveau")} />
            <TextButton type="button" onClick={() => document.getElementById("template-file")?.click()}>
              {text("cluster.import")}
            </TextButton>
          </div>
        ) : (
          <p className="text-muted">{text("template.empty")}</p>
        )
      ) : (
        <ul className="flex flex-col gap-4">
          {rows.map((row) => (
            <li key={row.id} className="relative rounded-md border border-line hover:bg-status">
              <Link to={`/templates/${row.id}`} className="block p-4 pr-14 text-ink">
                <span>{row.name}</span>
                <p className="mt-2 text-muted">{summary(row.body)}</p>
              </Link>
              <div className="absolute right-3 top-3">
                <ActionsMenu label={text("apps.more")}>
                  {canDeploy ? <MenuItem onClick={() => navigate(`/templates/${row.id}/deploy`)}>{text("template.deploy")}</MenuItem> : null}
                  <MenuItem onClick={() => void download(row).catch((err) => toast(err instanceof Error ? err.message : text("common.error"), "bad"))}>
                    {text("template.export")}
                  </MenuItem>
                  {canDeploy ? (
                    <MenuItem tone="danger" onClick={() => setRemoving(row)}>
                      {text("common.delete")}
                    </MenuItem>
                  ) : null}
                </ActionsMenu>
              </div>
            </li>
          ))}
        </ul>
      )}
      <Starters />
      {removing ? (
        <ConfirmDialog
          title={text("server.removeNamed", { name: removing.name })}
          confirmLabel={text("common.delete")}
          danger
          phrase={removing.name}
          phraseLabel={text("template.name")}
          onConfirm={() => remove(removing)}
          onClose={() => setRemoving(null)}
        >
          {text("template.deleteHint")}
        </ConfirmDialog>
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
  const [saving, setSaving] = useState(false);
  const toast = useToast();

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
    if (saving) return;
    setError("");
    setSaving(true);
    const payload = { name, ...body };
    try {
      const saved = id
        ? await api<Template>(`/api/templates/${id}`, { method: "PUT", body: JSON.stringify(payload) })
        : await api<Template>("/api/templates", { method: "POST", body: JSON.stringify(payload) });
      // Saving keeps the editor open: deploying is a choice of its own.
      toast(text("template.saved", { name: saved.name }));
      if (!id) navigate(`/templates/${saved.id}`, { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setSaving(false);
    }
  }

  if (!ready) return <LoadingLines lines={4} />;
  const servers = body.servers ?? [];
  const apps = body.applications ?? [];
  const [picking, setPicking] = useState(false);

  return (
    <>
      <PageTitle title={id ? name || "Template" : text("crumb.newTemplate")} icon={LayoutTemplate} />
      <form onSubmit={save} className="flex max-w-[720px] flex-col gap-10">
        <Steps
          steps={[
            {
              title: text("steps.cluster"),
              content: (
                <>
                <Field label={text("template.name")}>
                  <Input value={name} onChange={(e) => setName(e.target.value)} required />
                </Field>
                <section className="flex flex-col gap-4">
                  <div className="hidden md:block">
                    <SectionTitle>{text("field.cluster")}</SectionTitle>
                  </div>
                  <Field label={text("cluster.name")}>
                    <Input
                      value={body.clusterName ?? ""}
                      placeholder={text("ui.videPourChoisirUnClusterAu")}
                      onChange={(e) => setBody({ ...body, clusterName: e.target.value })}
                    />
                  </Field>
                </section>
                </>
              ),
            },
            {
              title: text("steps.servers"),
              content: (
                <section className="flex flex-col gap-4">
                  <div className="hidden md:block">
                    <SectionTitle>{text("cluster.servers")}</SectionTitle>
                  </div>
                  {servers.map((server, index) => (
                    <div key={index} className="flex flex-col gap-4 rounded-md border border-line p-4">
                      <MachineFields
                        server={server}
                        providers={providers}
                        keys={keys}
                        onChange={(next) => setBody({ ...body, servers: servers.map((item, i) => (i === index ? next : item)) })}
                      />
                      <TextButton type="button" tone="danger" onClick={() => setBody({ ...body, servers: servers.filter((_, i) => i !== index) })}>
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
              ),
            },
            {
              title: text("steps.apps"),
              content: (
                <section className="flex flex-col gap-4">
                  <div className="hidden md:block">
                    <SectionTitle>{text("nav.apps")}</SectionTitle>
                  </div>
                  {apps.map((app, index) => (
                    <div key={index} className="flex flex-col gap-4 rounded-md border border-line p-4">
                      <AppFields
                        app={app}
                        zones={zones}
                        onChange={(next) => setBody({ ...body, applications: apps.map((item, i) => (i === index ? next : item)) })}
                      />
                      <TextButton type="button" tone="danger" onClick={() => setBody({ ...body, applications: apps.filter((_, i) => i !== index) })}>
                        {text("common.remove")}
                      </TextButton>
                    </div>
                  ))}
                  <div className="flex flex-wrap gap-6">
                    <TextButton type="button" onClick={() => setBody({ ...body, applications: [...apps, emptyApp()] })}>
                      <Icon icon={Plus} />
                      {text("ui.ajouterUneApplication")}
                    </TextButton>
                    <TextButton type="button" onClick={() => setPicking(true)}>
                      <Icon icon={Plus} />
                      {text("template.fromPackage")}
                    </TextButton>
                  </div>
                  {picking ? (
                    <PackagePick
                      onClose={() => setPicking(false)}
                      onPick={(pkg) => {
                        setPicking(false);
                        const pub = pkg.services.find((service) => service.public) ?? pkg.services[0];
                        setBody({ ...body, applications: [...apps, { name: pkg.name, sourceKind: "image", source: pub?.image ?? "", replicas: 1, package: pkg, inputs: {} }] });
                      }}
                    />
                  ) : null}
                </section>
              ),
            },
          ]}
          error={error ? <ErrorText>{error}</ErrorText> : null}
          submit={
            <Button type="submit" disabled={saving}>
              {saving ? <Spinner /> : null}
              {text("common.save")}
            </Button>
          }
        />
        {id ? (
          <p>
            <Link to={`/templates/${id}/deploy`} className="text-muted underline">
              {text("template.deploy")}
            </Link>
          </p>
        ) : null}
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
  const [launching, setLaunching] = useState(false);
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
    if (launching) return;
    setError("");
    setLaunching(true);
    try {
      const run = await api<TemplateRun>(`/api/templates/${id}/runs`, {
        method: "POST",
        body: JSON.stringify({ ...body, targetClusterId: body.clusterName ? "" : target }),
      });
      navigate(`/templates/suivis/${run.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setLaunching(false);
    }
  }

  if (!ready) return <LoadingLines lines={4} />;
  if (!template) return <ErrorText>{error || text("common.notFound")}</ErrorText>;
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
            <p>{text("field.cluster")} {choices.find((cluster) => cluster.id === target)?.name}</p>
          )}
          {servers.map((server, index) => {
            const marked = servers.some((item) => item.controlPlane);
            const control = server.controlPlane || (!marked && index === 0 && Boolean(body.clusterName));
            return (
            <div key={index} className="flex flex-col gap-2 border-t border-line pt-4">
              <Field label={control ? text("template.controlServer") : text("template.server")}>
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
                {(app.replicas ?? 1) > 1 ? ` · ${text("template.copies", { count: app.replicas ?? 1 })}` : ""}
              </p>
              {(app.services ?? []).map((service) => (
                <p key={service.name} className="text-muted">
                  {service.name} · {service.image}
                  {` · ${service.published ? text("template.published") : text("template.internal")}`}
                  {service.disk ? ` · ${text("template.disk")}` : ""}
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
            <Button type="button" onClick={() => void validate()} disabled={launching}>
              {launching ? <Spinner /> : null}
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
  const { data: run, setData: setRun, error: loadError, reload } = usePoll<TemplateRun>(`/api/template-runs/${id}`, 3000, {
    stopWhen: (next) => next.status === "done" || next.status === "failed" || next.status === "cancelled",
  });
  const [actionError, setError] = useState("");
  const error = actionError || loadError;

  async function resume() {
    setError("");
    try {
      setRun(await api<TemplateRun>(`/api/template-runs/${id}/resume`, { method: "POST" }));
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  if (!run && !error) return <LoadingLines lines={4} />;
  if (!run) return <ErrorText>{error}</ErrorText>;
  const label = run.status === "done" ? text("status.done") : run.status === "failed" ? text("status.failed") : run.status === "cancelled" ? text("deploy.cancelled") : text("ui.enCours");

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
        {text("server.controlPlaneLabel")}
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
  return (
    <OfferStorage
      offer={offer}
      period={server.period ?? "hour"}
      currency={currency}
      storageId={server.storageId ?? ""}
      storageGb={server.storageGb ? String(server.storageGb) : ""}
      onChange={(storageId, storageGb) => onChange({ ...server, storageId, storageGb: Number(storageGb) || 0 })}
    />
  );
}

function AppFields({ app, zones, onChange }: { app: TemplateApp; zones: DnsZone[]; onChange: (app: TemplateApp) => void }) {
  const names = app.names ?? [];
  const services = app.services ?? [];
  // The text is kept as typed: a line being written has no "=" yet, and
  // rebuilding it from the parsed values would erase it.
  const [variables, setVariables] = useState(() =>
    Object.entries(app.variables ?? {})
      .map(([key, value]) => `${key}=${value}`)
      .join("\n"),
  );
  return (
    <>
      <Field label={text("field.name")}>
        <Input value={app.name} onChange={(e) => onChange({ ...app, name: e.target.value })} required />
      </Field>
      {app.package ? (
        <>
          <div className="flex flex-col gap-1 rounded-md border border-line bg-surface p-3">
            <span>{text("template.packageNamed", { name: app.package.name })}</span>
            <span className="font-mono text-[13px] text-muted">{app.package.services.map((service) => service.name).join(" · ")}</span>
          </div>
          {(app.package.inputs ?? []).map((input) => (
            <Field key={input.key} label={input.label}>
              <Input value={app.inputs?.[input.key] ?? ""} onChange={(e) => onChange({ ...app, inputs: { ...app.inputs, [input.key]: e.target.value } })} required />
            </Field>
          ))}
          <NameList names={names} zones={zones} onChange={(next) => onChange({ ...app, names: next })} />
        </>
      ) : (
      <>
      <Field label={text("field.source")}>
        <Select value={app.sourceKind} onChange={(e) => onChange({ ...app, sourceKind: e.target.value })}>
          <option value="image">{text("common.image")}</option>
          {/* Kept for a template written before: no build from Git yet. */}
          {app.sourceKind === "git" ? <option value="git">{text("ui.depot")}</option> : null}
        </Select>
      </Field>
      <Field label={app.sourceKind === "git" ? text("ui.depot") : text("common.image")}>
        <Input value={app.source} onChange={(e) => onChange({ ...app, source: e.target.value })} required />
      </Field>
      <NameList names={names} zones={zones} onChange={(next) => onChange({ ...app, names: next })} />
      <Fold title={text("field.services")}>
        <div className="flex flex-col gap-3">
          {services.map((service, index) => (
            <div key={index} className="flex flex-col gap-2 rounded-md border border-line p-3">
              <Input value={service.name} placeholder={text("field.name")} aria-label={text("field.name")} onChange={(e) => onChange({ ...app, services: services.map((item, i) => (i === index ? { ...item, name: e.target.value } : item)) })} />
              <Input value={service.image} placeholder={text("common.image")} aria-label={text("common.image")} onChange={(e) => onChange({ ...app, services: services.map((item, i) => (i === index ? { ...item, image: e.target.value } : item)) })} />
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
      </>
      )}
      <Field label={text("field.redundancy")}>
        <Input
          value={String(app.replicas ?? 1)}
          inputMode="numeric"
          onChange={(e) => onChange({ ...app, replicas: Number(e.target.value) || 1 })}
        />
      </Field>
      <p className="text-faint">{text("app.replicasHint")}</p>
      <Field label={text("app.variables")}>
        <Code
          value={variables}
          language="env"
          label={text("app.variables")}
          placeholder="NOM=valeur"
          minHeight="6rem"
          onChange={(next) => {
            setVariables(next);
            onChange({ ...app, variables: parseVariables(next) });
          }}
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
        setQuote(found ? formatPrice(String(found.amount), found.period !== "month", found.currency) : "");
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
      {server.period === "month" ? ` · ${text("ui.mois")}` : server.period ? ` · ${text("ui.heure")}` : ""}
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
          {formatPrice(String(total.amount), total.period !== "month", total.currency)}
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
  const cluster = body.clusterName ? `${text("field.cluster")} ${body.clusterName}` : text("ui.clusterAuLancement");
  return `${cluster} · ${text(servers === 1 ? "template.serversOne" : "template.serversMany", { count: servers })} · ${text(apps === 1 ? "template.appsOne" : "template.appsMany", { count: apps })}`;
}

function stepText(status: string) {
  if (status === "cancelled") return text("deploy.cancelled");
  if (status === "done") return text("status.done");
  if (status === "failed") return text("status.failed");
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


async function download(row: Template) {
  const res = await fetch(`/api/templates/${row.id}/export`);
  if (!res.ok) throw new Error(knownMessage((await res.text()).trim()) || text("common.error"));
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `${row.name}.json`;
  link.click();
  URL.revokeObjectURL(url);
}


type Starter = {
  id: string;
  servers: number;
  controlPlanes: number;
  apps: { id: string; name: string; logo?: string }[];
};

// Starters are ready-made templates: a cluster and catalogue applications
// that go well together. Using one creates a template to complete with a
// provider, offers, keys and the zone of the names.
function Starters() {
  useI18n();
  const navigate = useNavigate();
  const { data, error } = usePoll<Starter[]>("/api/starters", 0);
  const [busy, setBusy] = useState("");
  const [failure, setFailure] = useState("");

  async function use(starter: Starter) {
    setBusy(starter.id);
    setFailure("");
    try {
      const created = await api<Template>(`/api/starters/${starter.id}`, {
        method: "POST",
        body: JSON.stringify({ name: text(`starter.${starter.id}.name` as MsgKey) }),
      });
      navigate(`/templates/${created.id}`);
    } catch (err) {
      setFailure(err instanceof Error ? err.message : text("common.error"));
      setBusy("");
    }
  }

  if (error && !data) return null;
  return (
    <section className="mt-12">
      <h2 className="mb-2 text-lg">{text("starter.title")}</h2>
      <p className="mb-6 max-w-[720px] text-muted">{text("starter.intro")}</p>
      {failure ? (
        <div className="mb-6">
          <ErrorText>{failure}</ErrorText>
        </div>
      ) : null}
      {!data ? (
        <LoadingLines lines={3} />
      ) : (
        <ul className="grid gap-4 md:grid-cols-2">
          {data.map((starter) => (
            <li key={starter.id} className="flex flex-col gap-3 rounded-md border border-line p-4">
              <div>
                <p className="font-medium">{text(`starter.${starter.id}.name` as MsgKey)}</p>
                <p className="mt-1 text-muted">{text(`starter.${starter.id}.summary` as MsgKey)}</p>
              </div>
              <ul className="flex flex-wrap gap-x-4 gap-y-2" aria-label={text("starter.apps")}>
                {starter.apps.map((app) => (
                  <li key={app.id} className="flex items-center gap-2 text-[13px]">
                    <AppLogo slug={app.logo ?? ""} name={app.name} size={24} />
                    {app.name}
                  </li>
                ))}
              </ul>
              <p className="text-[13px] text-faint">
                {starter.servers > 1 ? text("starter.servers", { count: String(starter.servers) }) : text("starter.server")}
                {" · "}
                {text(`starter.${starter.id}.sizing` as MsgKey)}
              </p>
              <div className="mt-auto">
                <Button type="button" variant="secondary" onClick={() => void use(starter)} disabled={busy !== ""}>
                  {busy === starter.id ? <Spinner /> : null}
                  {text("starter.use")}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// PackagePick chooses one of the person's packages, to carry it whole in
// a template.
function PackagePick({ onClose, onPick }: { onClose: () => void; onPick: (pkg: Package) => void }) {
  const [rows, setRows] = useState<PackageRow[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    api<PackageRow[]>("/api/packages")
      .then(setRows)
      .catch((err) => setError(err.message));
  }, []);

  async function pick(row: PackageRow) {
    try {
      const full = await api<PackageRow>(`/api/packages/${row.id}`);
      if (full.package) onPick(full.package);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  return (
    <Sheet title={text("template.fromPackage")} onClose={onClose}>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {rows === null && !error ? <LoadingLines lines={3} /> : null}
      {rows && rows.length === 0 ? (
        <p className="text-muted">
          {text("package.noneYet")}{" "}
          <Link to="/applications/paquets" className="text-ink underline">
            {text("nav.packages")}
          </Link>
        </p>
      ) : null}
      <ul className="flex flex-col gap-3">
        {(rows ?? []).map((row) => (
          <li key={row.id}>
            <button type="button" onClick={() => void pick(row)} className="flex w-full flex-col gap-1 rounded-md border border-line p-3 text-left hover:bg-status">
              <span>{row.name}</span>
              <span className="font-mono text-[13px] text-muted">{row.services.join(" · ")}</span>
            </button>
          </li>
        ))}
      </ul>
    </Sheet>
  );
}
