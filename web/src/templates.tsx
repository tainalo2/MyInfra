import { FormEvent, useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Link, useBlocker, useNavigate, useParams, type BlockerFunction } from "react-router-dom";
import { LayoutTemplate, Plus, Rocket, Trash2 } from "lucide-react";
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
import { currentLang, text, knownMessage } from "./i18n";
import { Code } from "./code-lazy";
import { OfferFields, OfferStorage, formatPrice } from "./offers";
import { ConfirmDialog, Sheet, Spinner, StatusBadge, Tabs, useToast, useWide } from "./kit";
import { ChosenName, PlanPanel, templatePlan, underPlan } from "./plan";
import { ProgressSteps } from "./progress";
import { usePoll } from "./poll";
import { AppLogo } from "./app-logo";
import { CommunityTag, LibraryTag } from "./library-tag";
import { useI18n } from "./lang";
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
  IconButton,
  Input,
  LoadingLines,
  MenuItem,
  PageTitle,
  SectionTitle,
  Select,
  TextButton,
  cardGrid,
  focus,
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
        phoneAction={rows && rows.length > 0 && canDeploy ? { icon: Plus, label: text("crumb.newTemplate"), onClick: () => navigate("/templates/nouveau") } : undefined}
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
      <p className="mb-6 max-w-[720px] text-muted">{text("template.intro")}</p>
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
        <section>
          <h2 className="mb-3">
            {text("template.mine")} · {rows.length}
          </h2>
          <ul className={cardGrid}>
            {rows.map((row) => (
              <li key={row.id} className="flex h-full flex-col gap-3 rounded-md border border-line bg-surface p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <Link to={`/templates/${row.id}`} className={`font-medium text-ink ${focus}`}>
                      {row.name}
                    </Link>
                    <LibraryTag mark={row.library} />
                    <p className="text-[13px] text-muted">{summary(row.body)}</p>
                  </div>
                  <ActionsMenu label={text("apps.actionsOf", { name: row.name })}>
                    <MenuItem onClick={() => navigate(`/templates/${row.id}`)}>{text("template.edit")}</MenuItem>
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
                {(row.body.applications ?? []).length > 0 ? (
                  <ul className="flex flex-wrap gap-1.5" aria-label={text("starter.apps")}>
                    {(row.body.applications ?? []).map((app, index) => (
                      <li key={`${app.name}-${index}`} title={app.name}>
                        <AppLogo slug={app.package?.about?.logo ?? ""} name={app.name} size={24} />
                      </li>
                    ))}
                  </ul>
                ) : null}
                {canDeploy ? (
                  <div className="mt-auto border-t border-line pt-3">
                    <TextButton type="button" onClick={() => navigate(`/templates/${row.id}/deploy`)}>
                      <Icon icon={Rocket} />
                      {text("template.deploy")}
                    </TextButton>
                  </div>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
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

// snapshot is what a template's editor compares to tell whether it changed.
function snapshot(name: string, body: TemplateBody) {
  return JSON.stringify({ name, clusterName: body.clusterName ?? "", servers: body.servers ?? [], applications: body.applications ?? [] });
}

// LeaveDialog holds a navigation back while there is something to save. A
// failed save keeps it open: leaving then would lose the changes.
function LeaveDialog({ onSave, onLeave, onClose }: { onSave: () => Promise<unknown>; onLeave: () => void; onClose: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save() {
    setBusy(true);
    setError("");
    try {
      await onSave();
      onLeave();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  return (
    <Sheet title={text("template.leaveTitle")} onClose={onClose} busy={busy}>
      <p className="mb-6 text-muted">{text("template.leaveHint")}</p>
      {error ? (
        <div className="mb-6">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-4">
        <Button type="button" onClick={() => void save()} disabled={busy} data-autofocus>
          {busy ? <Spinner /> : null}
          {text("common.save")}
        </Button>
        <TextButton type="button" tone="danger" onClick={onLeave} disabled={busy}>
          {text("template.leaveQuit")}
        </TextButton>
        <TextButton type="button" onClick={onClose} disabled={busy}>
          {text("common.cancel")}
        </TextButton>
      </div>
    </Sheet>
  );
}

export function TemplateEditor() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { canDeploy } = useSession();
  const [name, setName] = useState("");
  const [body, setBody] = useState<TemplateBody>({ servers: [], applications: [] });
  const [providers, setProviders] = useState<Secret[]>([]);
  const [keys, setKeys] = useState<Secret[]>([]);
  const [zones, setZones] = useState<DnsZone[]>([]);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(!id);
  const [saving, setSaving] = useState(false);
  const [picking, setPicking] = useState(false);
  // The element of the plan whose settings show, and which of its parts.
  const [chosen, setChosen] = useState<string | null>(null);
  const [part, setPart] = useState("general");
  const toast = useToast();
  // Refs, so that a navigation made right after saving sees it at once.
  const draft = useRef({ name, body });
  draft.current = { name, body };
  const savedAs = useRef(snapshot("", {}));
  // Someone who cannot save has nothing to be offered.
  const changed = useCallback(() => canDeploy && snapshot(draft.current.name, draft.current.body) !== savedAs.current, [canDeploy]);
  const blocker = useBlocker(useCallback<BlockerFunction>(({ currentLocation, nextLocation }) => currentLocation.pathname !== nextLocation.pathname && changed(), [changed]));

  // Closing the tab cannot show a page of ours: the browser asks by itself.
  useEffect(() => {
    function warn(event: BeforeUnloadEvent) {
      if (changed()) event.preventDefault();
    }
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [changed]);

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
        const next = {
          clusterName: row.body.clusterName ?? "",
          servers: row.body.servers ?? [],
          applications: row.body.applications ?? [],
        };
        setName(row.name);
        setBody(next);
        savedAs.current = snapshot(row.name, next);
      })
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
  }, [id]);

  async function persist() {
    const sent = snapshot(name, body);
    const payload = { name, ...body };
    const saved = id
      ? await api<Template>(`/api/templates/${id}`, { method: "PUT", body: JSON.stringify(payload) })
      : await api<Template>("/api/templates", { method: "POST", body: JSON.stringify(payload) });
    savedAs.current = sent;
    toast(text("template.saved", { name: saved.name }));
    return saved;
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    if (saving) return;
    setError("");
    setSaving(true);
    try {
      // Saving keeps the editor open: deploying is a choice of its own.
      const saved = await persist();
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
  // What is left open here is completed at deployment: only a missing name
  // stops the template from being saved.
  const model = templatePlan(body, {
    cluster: body.clusterName ?? "",
    clusterLater: true,
    serverReady: (server) => server.name.trim() !== "",
    appReady: (app) => app.name.trim() !== "",
  });
  const picked = model.nodes.find((node) => node.key === chosen);
  const [kind, raw] = (picked?.key ?? "").split(":");
  const index = Number(raw);
  const planes = servers.filter((server) => server.controlPlane).length;
  const setServer = (at: number, next: TemplateServer) => setBody({ ...body, servers: servers.map((item, i) => (i === at ? next : item)) });
  const setApp = (at: number, next: TemplateApp) => setBody({ ...body, applications: apps.map((item, i) => (i === at ? next : item)) });

  // A new element is chosen at once, so that its settings open.
  function open(key: string, tab: string) {
    setChosen(key);
    setPart(tab);
  }

  function addServer() {
    setBody({ ...body, servers: [...servers, { ...emptyServer(), controlPlane: !servers.some((server) => server.controlPlane) }] });
    open(`server:${servers.length}`, "machine");
  }

  function addApp(app: TemplateApp) {
    setBody({ ...body, applications: [...apps, app] });
    open(`app:${apps.length}`, "general");
  }

  function remove() {
    setChosen(null);
    if (kind === "server") setBody({ ...body, servers: servers.filter((_, i) => i !== index) });
    else setBody({ ...body, applications: apps.filter((_, i) => i !== index) });
  }

  const clusterField = (
    <Field label={text("cluster.name")}>
      <Input value={body.clusterName ?? ""} placeholder={text("ui.videPourChoisirUnClusterAu")} onChange={(e) => setBody({ ...body, clusterName: e.target.value })} />
    </Field>
  );

  return (
    <>
      <PageTitle inTree title={id ? name || "Template" : text("crumb.newTemplate")} icon={LayoutTemplate} />
      <PlanPanel
        model={model}
        adds={
          canDeploy
            ? {
                apps: { label: text("ui.ajouterUneApplication"), onClick: () => addApp(emptyApp()) },
                machines: { label: text("ui.ajouterUnServeur"), onClick: addServer },
              }
            : undefined
        }
        label={text("plan.labelTemplate")}
        whole={text("plan.wholeTemplate")}
        summary={text("plan.templateSummary", { apps: apps.length, servers: servers.length })}
        named={false}
        selected={picked ? picked.key : null}
        onSelect={(key) => {
          setChosen(key);
          setPart("general");
        }}
      />
      <div className="mt-2" />
      {/* Fields out of sight must not stop the form: the server says what is
          wrong, and the page shows it. */}
      <form onSubmit={save} noValidate className="flex flex-col gap-3">
        <div className={underPlan}>
          <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1 border-b border-line py-1">
            <div className="min-w-0">
              {picked ? (
                <span className="flex min-w-0 items-center gap-3">
                  <ChosenName node={picked} />
                  {canDeploy && (kind === "server" || kind === "app") ? <IconButton type="button" icon={Trash2} label={text("common.remove")} onClick={remove} /> : null}
                </span>
              ) : null}
            </div>
            {canDeploy ? (
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                <TextButton type="button" onClick={() => setPicking(true)}>
                  <Icon icon={Plus} />
                  {text("template.fromPackage")}
                </TextButton>
                {id ? (
                  <Button type="button" variant="secondary" className="sm:py-1!" onClick={() => navigate(`/templates/${id}/deploy`)}>
                    {text("template.deployment")}
                  </Button>
                ) : null}
              </div>
            ) : null}
          </div>
        </div>
        {picked ? (
          <ElementSettings
            chosen={picked.key}
            part={part}
            onPart={setPart}
            body={body}
            setServer={setServer}
            setApp={setApp}
            cluster={clusterField}
            providers={providers}
            keys={keys}
            zones={zones}
          />
        ) : (
          <div className="mb-6 flex max-w-[720px] flex-col gap-4">
            <Field label={text("template.name")}>
              <Input value={name} onChange={(e) => setName(e.target.value)} required />
            </Field>
            {clusterField}
          </div>
        )}
        {kind === "server" ? <p className="max-w-[720px] text-faint">{text("template.planesHint")}</p> : null}
        {planes > 0 && planes % 2 === 0 ? <p className="max-w-[720px] text-warn">{text("server.evenPlanes")}</p> : null}
        {error ? <ErrorText>{error}</ErrorText> : null}
        {canDeploy ? (
          <div>
            <Button type="submit" disabled={saving}>
              {saving ? <Spinner /> : null}
              {text("common.save")}
            </Button>
          </div>
        ) : null}
      </form>
      {picking ? (
        <PackagePick
          onClose={() => setPicking(false)}
          onPick={(pkg) => {
            setPicking(false);
            const pub = pkg.services.find((service) => service.public) ?? pkg.services[0];
            addApp({ name: pkg.name, sourceKind: "image", source: pub?.image ?? "", replicas: 1, package: pkg, inputs: {} });
          }}
        />
      ) : null}
      {blocker.state === "blocked" ? <LeaveDialog onSave={persist} onLeave={() => blocker.proceed()} onClose={() => blocker.reset()} /> : null}
    </>
  );
}

export function TemplateLaunch() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [template, setTemplate] = useState<Template | null>(null);
  const [body, setBody] = useState<TemplateBody>({});
  const [target, setTarget] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [providers, setProviders] = useState<Secret[]>([]);
  const [keys, setKeys] = useState<Secret[]>([]);
  const [zones, setZones] = useState<DnsZone[]>([]);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);
  // The element of the plan whose settings show, and which of its parts.
  const [chosen, setChosen] = useState<string | null>(null);
  const [part, setPart] = useState("general");
  const wide = useWide();

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
      })
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
  }, [id]);

  // A failure stays in the dialog that asked for it.
  async function launch() {
    const run = await api<TemplateRun>(`/api/templates/${id}/runs`, {
      method: "POST",
      body: JSON.stringify({ ...body, targetClusterId: body.clusterName ? "" : target }),
    });
    navigate(`/templates/suivis/${run.id}`);
  }

  if (!ready) return <LoadingLines lines={4} />;
  if (!template) return <ErrorText>{error || text("common.notFound")}</ErrorText>;
  const servers = body.servers ?? [];
  const apps = body.applications ?? [];
  const missing = hasGaps(body, zones);
  const choices = clusters.filter((cluster) => (servers.length > 0 ? cluster.origin !== "imported" : true));

  const clusterLabel = body.clusterName || choices.find((cluster) => cluster.id === target)?.name || "";
  const model = templatePlan(body, {
    cluster: clusterLabel,
    serverReady,
    appReady: (app) => !(app.names ?? []).some((name) => name.subdomain.trim() !== "" && !knownZone(name.zone, zones)),
  });
  const canContinue = !missing && (!needsCluster(body) || Boolean(target));
  const setServer = (index: number, next: TemplateServer) => setBody({ ...body, servers: servers.map((item, i) => (i === index ? next : item)) });
  const setApp = (index: number, next: TemplateApp) => setBody({ ...body, applications: apps.map((item, i) => (i === index ? next : item)) });
  const clusterField = body.clusterName ? (
    <Field label={text("template.createdCluster")}>
      <Input value={body.clusterName} onChange={(e) => setBody({ ...body, clusterName: e.target.value })} required />
    </Field>
  ) : choices.length === 0 ? (
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
  );

  const picked = model.nodes.find((node) => node.key === chosen);
  const settings = picked ? (
    <ElementSettings
      chosen={picked.key}
      part={part}
      onPart={setPart}
      body={body}
      setServer={setServer}
      setApp={setApp}
      cluster={clusterField}
      providers={providers}
      keys={keys}
      zones={zones}
    />
  ) : null;

  return (
    <>
      <PageTitle inTree title={template.name} icon={LayoutTemplate} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      <PlanPanel
        model={model}
        label={text("plan.labelTemplate")}
        whole={text("plan.wholeTemplate")}
        summary={text("plan.templateSummary", { apps: apps.length, servers: servers.length })}
        named={false}
        selected={picked ? picked.key : null}
        onSelect={(key) => {
          setChosen(key);
          setPart("general");
        }}
      >
        {wide ? null : settings}
      </PlanPanel>
      <div className="mt-2" />
      {/* Deploying stays in reach wherever the page is: under the plan on a
          wide screen, above the tabs on a phone. */}
      <div className={`${underPlan} max-md:fixed max-md:inset-x-0 max-md:bottom-[calc(4rem+env(safe-area-inset-bottom))] max-md:z-20 max-md:border-t max-md:border-line max-md:bg-canvas max-md:px-4 max-md:py-2`}>
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1 md:border-b md:border-line md:py-1">
          <div className="min-w-0 max-md:hidden">{picked ? <ChosenName node={picked} /> : null}</div>
          <div className="flex items-center gap-4 max-md:w-full max-md:justify-end">
            {canContinue ? null : <span className="text-[13px] text-warn">{text("plan.missing")}</span>}
            <Button type="button" className="sm:py-1!" disabled={!canContinue} onClick={() => setConfirming(true)}>
              <Icon icon={Rocket} />
              {text("template.deploy")}
            </Button>
          </div>
        </div>
      </div>
      <div className="mt-3" />
      {wide ? settings : null}
      {picked ? null : (
        <div className="flex max-w-[720px] flex-col gap-8">
          {missing || needsCluster(body) ? (
            <>
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
                    <MachineFields server={server} providers={providers} keys={keys} onChange={(next) => setServer(index, next)} />
                  </div>
                ),
              )}
              {apps.map((app, index) => {
                const open = (app.names ?? []).some((name) => name.subdomain && !knownZone(name.zone, zones));
                if (!open) return null;
                return <AppFields key={index} app={app} zones={zones} onChange={(next) => setApp(index, next)} />;
              })}
            </>
          ) : null}
          {canContinue ? (
            <>
              <SectionTitle>{text("template.summary")}</SectionTitle>
              {body.clusterName ? (
                <Field label={text("template.createdCluster")}>
                  <Input value={body.clusterName} onChange={(e) => setBody({ ...body, clusterName: e.target.value })} required />
                </Field>
              ) : null}
              {servers.map((server, index) => {
                const marked = servers.some((item) => item.controlPlane);
                const control = server.controlPlane || (!marked && index === 0 && Boolean(body.clusterName));
                return (
                  <div key={index} className="flex flex-col gap-2 border-t border-line pt-4">
                    <Field label={control ? text("template.controlServer") : text("template.server")}>
                      <Input value={server.name} onChange={(e) => setServer(index, { ...server, name: e.target.value })} required />
                    </Field>
                    <ServerLine server={server} providers={providers} keys={keys} />
                  </div>
                );
              })}
              {apps.map((app, index) => (
                <div key={index} className="flex flex-col gap-2 border-t border-line pt-4">
                  <Field label={text("crumb.application")}>
                    <Input value={app.name} onChange={(e) => setApp(index, { ...app, name: e.target.value })} required />
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
            </>
          ) : null}
        </div>
      )}
      {/* The bar of a phone sits over the end of the page. */}
      <div aria-hidden className="h-16 md:hidden" />
      {confirming ? (
        <ConfirmDialog title={text("template.deployTitle", { name: template.name })} confirmLabel={text("template.deploy")} onConfirm={launch} onClose={() => setConfirming(false)}>
          <div className="flex flex-col gap-2">
            {clusterLabel ? (
              <p>
                {text("field.cluster")} {clusterLabel}
              </p>
            ) : null}
            <p>{text("plan.templateSummary", { apps: apps.length, servers: servers.length })}</p>
            <Cost servers={servers} />
          </div>
        </ConfirmDialog>
      ) : null}
    </>
  );
}

// ElementSettings shows the settings of one element of a template's plan;
// an element with several kinds of settings has a nav to go from one to the other.
function ElementSettings({
  chosen,
  part,
  onPart,
  body,
  setServer,
  setApp,
  cluster,
  providers,
  keys,
  zones,
}: {
  chosen: string;
  part: string;
  onPart: (part: string) => void;
  body: TemplateBody;
  setServer: (index: number, server: TemplateServer) => void;
  setApp: (index: number, app: TemplateApp) => void;
  // cluster is what the page offers for the cluster: naming it or choosing one.
  cluster: ReactNode;
  providers: Secret[];
  keys: Secret[];
  zones: DnsZone[];
}) {
  const [kind, raw] = chosen.split(":");
  const index = Number(raw);
  let parts: { key: string; label: string; body: ReactNode }[] = [];
  if (kind === "cluster") {
    parts = [{ key: "general", label: text("tpl.part.general"), body: cluster }];
  } else if (kind === "server") {
    const servers = body.servers ?? [];
    const server = servers[index];
    if (!server) return null;
    const marked = servers.some((item) => item.controlPlane);
    const pilot = server.controlPlane || (!marked && index === 0 && Boolean(body.clusterName));
    parts = [
      {
        key: "general",
        label: text("tpl.part.general"),
        body: (
          <>
            <p className="text-muted">{pilot ? text("tpl.pilotHint") : text("tpl.appsHint")}</p>
            <ServerLine server={server} providers={providers} keys={keys} />
            {serverReady(server) ? null : <p className="text-[13px] text-warn">{text("plan.missing")}</p>}
          </>
        ),
      },
      {
        key: "machine",
        label: text("tpl.part.machine"),
        body: <MachineFields server={server} providers={providers} keys={keys} onChange={(next) => setServer(index, next)} />,
      },
    ];
  } else if (kind === "app") {
    const app = (body.applications ?? [])[index];
    if (!app) return null;
    const change = (next: TemplateApp) => setApp(index, next);
    parts = [
      { key: "general", label: text("tpl.part.general"), body: <AppGeneral app={app} onChange={change} /> },
      { key: "addresses", label: text("tpl.part.addresses"), body: <AppNames app={app} zones={zones} onChange={change} /> },
      ...(app.package ? [] : [{ key: "services", label: text("tpl.part.services"), body: <AppServicesFields app={app} onChange={change} /> }]),
      { key: "copies", label: text("tpl.part.copies"), body: <AppCopies app={app} onChange={change} /> },
      { key: "variables", label: text("tpl.part.variables"), body: <AppVariables key={chosen} app={app} onChange={change} /> },
    ];
  } else {
    const names = (body.applications ?? []).flatMap((app) => (app.names ?? []).filter((name) => name.subdomain.trim()).map((name) => ({ app: app.name, host: `${name.subdomain}.${name.zone}` })));
    parts = [
      {
        key: "general",
        label: text("tpl.part.addresses"),
        body:
          names.length === 0 ? (
            <p className="text-muted">{text("names.none")}</p>
          ) : (
            <ul className="overflow-hidden rounded-md border border-line bg-surface">
              {names.map((name) => (
                <li key={name.host} className="flex flex-wrap items-baseline justify-between gap-x-4 border-t border-line px-4 py-2 first:border-t-0">
                  <span className="break-all font-mono text-[13px]">{name.host}</span>
                  <span className="text-[13px] text-muted">{name.app}</span>
                </li>
              ))}
            </ul>
          ),
      },
    ];
  }
  const shown = parts.find((item) => item.key === part) ?? parts[0];
  return (
    <div className="mb-6 flex max-w-[720px] flex-col gap-4">
      {parts.length > 1 ? (
        <Tabs label={text("tpl.part.parts")} active={shown.key} onChange={onPart} tabs={parts.map((item) => ({ key: item.key, label: item.label }))} />
      ) : null}
      <div role={parts.length > 1 ? "tabpanel" : undefined} className="flex flex-col gap-4">
        {shown.body}
      </div>
    </div>
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
      <PageTitle
        inTree
        title={text("template.follow")}
        mark={null}
        note={<StatusBadge status={run.status === "done" ? "running" : run.status === "running" ? "deploying" : run.status} />}
      />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      <p className={`mb-6 ${run.status === "failed" ? "text-bad" : "text-muted"}`}>
        {label}
        {run.status === "running" ? ` · ${text("progress.leave")}` : ""}
      </p>
      <div className="max-w-[720px] rounded-md border border-line bg-surface p-4 md:p-6">
        <ProgressSteps
          steps={run.steps.map((step, index) => ({
            key: String(index),
            status: step.status,
            title:
              step.kind === "server" && step.resourceId ? (
                <Link to={`/serveurs/${step.resourceId}`} className={`text-ink ${focus}`}>
                  {step.name}
                </Link>
              ) : step.kind === "application" && step.resourceId ? (
                <Link to={`/applications/${step.resourceId}`} className={`text-ink ${focus}`}>
                  {step.name}
                </Link>
              ) : (
                step.name
              ),
            detail: step.detail ? knownMessage(step.detail) : undefined,
            children:
              step.deploy && step.deploy.length > 0 ? (
                <ul className="mt-1 flex flex-col gap-0.5 text-[13px]">
                  {step.deploy.map((item) => (
                    <li key={item.id} className={item.status === "failed" ? "text-bad" : item.status === "done" ? "text-ok" : "text-muted"}>
                      {item.label}
                      {item.detail ? ` · ${knownMessage(item.detail)}` : ""}
                    </li>
                  ))}
                </ul>
              ) : null,
          }))}
        />
      </div>
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

type AppPart = { app: TemplateApp; onChange: (app: TemplateApp) => void };

// AppFields is every setting of an application of a template, in one form.
// Its parts also show one at a time, under the plan of the template.
function AppFields({ app, zones, onChange }: { app: TemplateApp; zones: DnsZone[]; onChange: (app: TemplateApp) => void }) {
  return (
    <>
      <AppGeneral app={app} onChange={onChange} />
      <AppNames app={app} zones={zones} onChange={onChange} />
      {app.package ? null : (
        <Fold title={text("field.services")}>
          <AppServicesFields app={app} onChange={onChange} />
        </Fold>
      )}
      <AppCopies app={app} onChange={onChange} />
      <AppVariables app={app} onChange={onChange} />
    </>
  );
}

function AppGeneral({ app, onChange }: AppPart) {
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
        </>
      )}
    </>
  );
}

function AppNames({ app, zones, onChange }: AppPart & { zones: DnsZone[] }) {
  return <NameList names={app.names ?? []} zones={zones} onChange={(next) => onChange({ ...app, names: next })} />;
}

function AppServicesFields({ app, onChange }: AppPart) {
  const services = app.services ?? [];
  return (
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
  );
}

function AppCopies({ app, onChange }: AppPart) {
  return (
    <>
      <Field label={text("field.redundancy")}>
        <Input value={String(app.replicas ?? 1)} inputMode="numeric" onChange={(e) => onChange({ ...app, replicas: Number(e.target.value) || 1 })} />
      </Field>
      <p className="text-faint">{text("app.replicasHint")}</p>
    </>
  );
}

function AppVariables({ app, onChange }: AppPart) {
  // The text is kept as typed: a line being written has no "=" yet, and
  // rebuilding it from the parsed values would erase it.
  const [variables, setVariables] = useState(() =>
    Object.entries(app.variables ?? {})
      .map(([key, value]) => `${key}=${value}`)
      .join("\n"),
  );
  return (
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


type StarterWords = { name: string; summary: string; sizing: string };

type Starter = StarterWords & {
  id: string;
  version: string;
  translations?: Record<string, StarterWords>;
  servers: number;
  controlPlanes: number;
  apps: { id: string; name: string; logo?: string }[];
  community?: boolean;
};

// starterWords gives a template of the library in the language of the
// screen: the library carries its words.
function starterWords(starter: Starter): StarterWords {
  const words = starter.translations?.[currentLang()];
  return {
    name: words?.name || starter.name,
    summary: words?.summary || starter.summary,
    sizing: words?.sizing || starter.sizing,
  };
}

// Starters are the templates of the library: a cluster and applications
// that go well together. Using one creates a template to complete with a
// provider, offers, keys and the zone of the names.
function Starters() {
  useI18n();
  const navigate = useNavigate();
  const { data, error } = usePoll<Starter[]>("/api/starters", 0);
  const [busy, setBusy] = useState("");
  const [failure, setFailure] = useState("");
  const [community, setCommunity] = useState(false);
  const shared = (data ?? []).some((starter) => starter.community);
  const shown = (data ?? []).filter((starter) => !community || starter.community);

  async function use(starter: Starter) {
    setBusy(starter.id);
    setFailure("");
    try {
      const created = await api<Template>(`/api/starters/${starter.id}`, {
        method: "POST",
        body: JSON.stringify({ name: starterWords(starter).name }),
      });
      navigate(`/templates/${created.id}`);
    } catch (err) {
      setFailure(err instanceof Error ? err.message : text("common.error"));
      setBusy("");
    }
  }

  if (error && !data) return null;
  return (
    <section className="mt-8">
      <h2 className="mb-1">{text("starter.title")}</h2>
      <p className="mb-4 max-w-[720px] text-[13px] text-muted">{text("starter.intro")}</p>
      {shared ? (
        <div className="mb-6 flex flex-wrap gap-2">
          <button
            type="button"
            aria-pressed={community}
            onClick={() => setCommunity(!community)}
            className={`rounded-md border px-3 py-1 ${community ? "border-ink bg-status text-ink" : "border-line bg-surface text-muted"}`}
          >
            {text("library.community")}
          </button>
        </div>
      ) : null}
      {failure ? (
        <div className="mb-6">
          <ErrorText>{failure}</ErrorText>
        </div>
      ) : null}
      {!data ? (
        <LoadingLines lines={3} />
      ) : (
        <ul className={cardGrid}>
          {shown.map((starter) => (
            <li key={starter.id} className="flex flex-col gap-3 rounded-md border border-line bg-surface p-4">
              <div>
                <p className="flex flex-wrap items-baseline gap-2">
                  <span className="font-medium">{starterWords(starter).name}</span>
                  <span className="text-[13px] text-faint">v{starter.version}</span>
                  {starter.community ? <CommunityTag /> : null}
                </p>
                <p className="mt-1 text-muted">{starterWords(starter).summary}</p>
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
                {starterWords(starter).sizing}
              </p>
              <div className="mt-auto border-t border-line pt-3">
                <TextButton type="button" onClick={() => void use(starter)} disabled={busy !== ""}>
                  {busy === starter.id ? <Spinner /> : null}
                  {text("starter.use")}
                </TextButton>
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
