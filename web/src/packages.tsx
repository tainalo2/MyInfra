import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ArrowRight, Package as PackageIcon, Plus, Trash2 } from "lucide-react";
import { ApiError, api, peek, type Package, type PackageFile, type PackageProblem, type PackageRow, type PackageService } from "./api";
import { Code, languageOf } from "./code-lazy";
import { knownMessage, text } from "./i18n";
import { useI18n } from "./lang";
import { ConfirmDialog, Sheet, Tabs, useToast } from "./kit";
import { useSession } from "./session";
import {
  ActionsMenu,
  Button,
  ErrorText,
  Field,
  Group,
  Icon,
  IconButton,
  Input,
  LoadingLines,
  MenuItem,
  PageTitle,
  Select,
  TextButton,
} from "./ui";

const blank: Package = {
  type: "myinfra-package",
  version: 1,
  name: "",
  services: [{ name: "web", image: "", public: true, port: 80 }],
};

type Check = { package: Package; problems: PackageProblem[]; readable: boolean };

// Problems found on a package: shown in the form by path, and underlined in
// the YAML by line.
function problemsUnder(problems: PackageProblem[], prefix: string) {
  return problems.filter((problem) => problem.path === prefix || problem.path.startsWith(prefix + ".") || problem.path.startsWith(prefix + "["));
}

async function download(row: { id: string; name: string }, format: "yaml" | "json") {
  const res = await fetch(`/api/packages/${row.id}/export?format=${format}`);
  if (!res.ok) throw new Error(knownMessage((await res.text()).trim()) || text("common.error"));
  const url = URL.createObjectURL(await res.blob());
  const link = document.createElement("a");
  link.href = url;
  link.download = `${row.name}.${format}`;
  link.click();
  URL.revokeObjectURL(url);
}

export function Packages() {
  useI18n();
  const navigate = useNavigate();
  const { canDeploy } = useSession();
  const [rows, setRows] = useState<PackageRow[] | null>(() => peek<PackageRow[]>("/api/packages"));
  const [error, setError] = useState("");
  const [importing, setImporting] = useState(false);
  const [removing, setRemoving] = useState<PackageRow | null>(null);
  const toast = useToast();

  function reload() {
    return api<PackageRow[]>("/api/packages").then(setRows);
  }

  useEffect(() => {
    reload().catch((err) => setError(err.message));
  }, []);

  async function remove(row: PackageRow) {
    await api(`/api/packages/${row.id}`, { method: "DELETE" });
    setRows((current) => current?.filter((item) => item.id !== row.id) ?? current);
    setRemoving(null);
    toast(text("package.removed", { name: row.name }));
  }

  function exported(row: PackageRow, format: "yaml" | "json") {
    void download(row, format).catch((err) => toast(err instanceof Error ? err.message : text("common.error"), "bad"));
  }

  return (
    <>
      <PageTitle
        title={text("nav.packages")}
        icon={PackageIcon}
        action={
          canDeploy ? (
            <span className="flex items-center gap-4">
              <TextButton type="button" onClick={() => setImporting(true)}>
                {text("cluster.import")}
              </TextButton>
              <Button type="button" onClick={() => navigate("/paquets/nouveau")}>
                <Icon icon={Plus} />
                {text("package.new")}
              </Button>
            </span>
          ) : null
        }
      />
      <p className="mb-8 max-w-[720px] text-muted">{text("package.intro")}</p>
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {rows === null ? (
        <LoadingLines lines={4} />
      ) : rows.length === 0 ? (
        <p className="text-muted">{text("package.empty")}</p>
      ) : (
        <ul className="flex flex-col gap-4">
          {rows.map((row) => (
            <li key={row.id} className="relative rounded-md border border-line hover:bg-status">
              <Link to={`/paquets/${row.id}`} className="block p-4 pr-14 text-ink">
                <span>{row.name}</span>
                {row.summary ? <p className="mt-1 text-muted">{row.summary}</p> : null}
                <p className="mt-2 flex flex-wrap gap-2">
                  {row.services.map((service) => (
                    <span key={service} className="rounded border border-line px-1.5 font-mono text-[13px] text-muted">
                      {service}
                    </span>
                  ))}
                </p>
              </Link>
              <div className="absolute right-3 top-3">
                <ActionsMenu label={text("apps.more")}>
                  {canDeploy ? <MenuItem onClick={() => navigate(`/nouveau?package=${row.id}`)}>{text("catalogue.install")}</MenuItem> : null}
                  <MenuItem onClick={() => exported(row, "yaml")}>{text("package.exportYaml")}</MenuItem>
                  <MenuItem onClick={() => exported(row, "json")}>{text("package.exportJson")}</MenuItem>
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
      {importing ? <ImportSheet onClose={() => setImporting(false)} onDone={(row) => navigate(`/paquets/${row.id}`)} /> : null}
      {removing ? (
        <ConfirmDialog
          title={text("server.removeNamed", { name: removing.name })}
          confirmLabel={text("common.delete")}
          danger
          onConfirm={() => remove(removing)}
          onClose={() => setRemoving(null)}
        >
          {text("package.deleteHint")}
        </ConfirmDialog>
      ) : null}
    </>
  );
}

// ImportSheet brings a package from a file, a pasted text or a link. What is
// wrong in it shows with its line, in the text itself.
function ImportSheet({ onClose, onDone }: { onClose: () => void; onDone: (row: PackageRow) => void }) {
  const [how, setHow] = useState<"file" | "paste" | "url">("file");
  const [pasted, setPasted] = useState("");
  const [url, setUrl] = useState("");
  const [problems, setProblems] = useState<PackageProblem[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function send(path: string, body: unknown) {
    setBusy(true);
    setError("");
    setProblems([]);
    try {
      onDone(await api<PackageRow>(path, { method: "POST", body: JSON.stringify(body) }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      // The server lists every problem: the pasted text shows them at their line.
      if (how !== "url") {
        const check = await api<Check>("/api/packages/check", { method: "POST", body: JSON.stringify(body) }).catch(() => null);
        if (check) setProblems(check.problems);
      }
    } finally {
      setBusy(false);
    }
  }

  async function onFile(file: File) {
    const content = await file.text();
    setPasted(content);
    await send("/api/packages/import", { text: content });
    setHow("paste");
  }

  return (
    <Sheet title={text("package.importTitle")} onClose={onClose} busy={busy} wide>
      <div className="flex flex-col gap-5">
        <Tabs
          label={text("package.importTitle")}
          active={how}
          onChange={setHow}
          tabs={[
            { key: "file", label: text("package.fromFile") },
            { key: "paste", label: text("package.fromText") },
            { key: "url", label: text("package.fromUrl") },
          ]}
        />
        <p className="text-muted">{text("package.importHint")}</p>
        {how === "file" ? (
          <Field label={text("package.file")}>
            <input
              type="file"
              accept=".yaml,.yml,.json,application/json,application/yaml,text/yaml"
              disabled={busy}
              onChange={(event) => {
                const file = event.target.files?.[0];
                event.target.value = "";
                if (file) void onFile(file);
              }}
            />
          </Field>
        ) : null}
        {how === "paste" ? (
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void send("/api/packages/import", { text: pasted });
            }}
          >
            <Code value={pasted} onChange={setPasted} language="yaml" problems={lined(problems)} label={text("package.fromText")} minHeight="14rem" maxHeight="50dvh" />
            <div>
              <Button type="submit" disabled={busy || !pasted.trim()}>
                {text("cluster.import")}
              </Button>
            </div>
          </form>
        ) : null}
        {how === "url" ? (
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void send("/api/packages/import-url", { url });
            }}
          >
            <Field label={text("package.url")}>
              <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…/package.yaml" required autoCapitalize="off" spellCheck={false} />
            </Field>
            <div>
              <Button type="submit" disabled={busy}>
                {text("cluster.import")}
              </Button>
            </div>
          </form>
        ) : null}
        {error ? <ErrorText>{error}</ErrorText> : null}
        {problems.length > 1 ? <ProblemList problems={problems} /> : null}
      </div>
    </Sheet>
  );
}

// toYaml writes a package as the advanced view shows it; null when the
// server cannot be reached, and the view stays as it is.
async function toYaml(pkg: Package): Promise<string | null> {
  try {
    const answer = await api<{ yaml: string }>("/api/packages/format", { method: "POST", body: JSON.stringify({ package: pkg }) });
    return answer.yaml;
  } catch {
    return null;
  }
}

function lined(problems: PackageProblem[]) {
  return problems.filter((problem) => problem.line).map((problem) => ({ line: problem.line!, message: problem.message }));
}

function ProblemList({ problems }: { problems: PackageProblem[] }) {
  return (
    <ul className="flex flex-col gap-1 text-bad" role="alert">
      {problems.map((problem, index) => (
        <li key={index}>
          {problem.line ? <span className="font-mono text-[13px]">{text("package.line", { line: problem.line })} · </span> : null}
          {problem.path ? <span className="font-mono text-[13px]">{problem.path} · </span> : null}
          {problem.message}
        </li>
      ))}
    </ul>
  );
}

export function PackageEditor() {
  useI18n();
  const { id } = useParams();
  const navigate = useNavigate();
  const { canDeploy } = useSession();
  const toast = useToast();
  const [pkg, setPkg] = useState<Package>(blank);
  const [yamlText, setYamlText] = useState("");
  const [mode, setMode] = useState<"visual" | "yaml">("visual");
  const [problems, setProblems] = useState<PackageProblem[]>([]);
  const [readable, setReadable] = useState(true);
  const [ready, setReady] = useState(!id);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  // Each edit is checked by the server once typing pauses; an older answer
  // never replaces a newer one.
  const asked = useRef(0);
  const yamlTimer = useRef(0);

  useEffect(() => {
    if (!id) return;
    api<PackageRow>(`/api/packages/${id}`)
      .then((row) => {
        if (row.package) setPkg(row.package);
        setYamlText(row.yaml ?? "");
      })
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
  }, [id]);

  // The form is the source in the visual view: the YAML follows it.
  useEffect(() => {
    if (!ready || mode !== "visual") return;
    const turn = ++asked.current;
    const timer = setTimeout(() => {
      api<{ yaml: string; problems: PackageProblem[] }>("/api/packages/format", { method: "POST", body: JSON.stringify({ package: pkg }) })
        .then((answer) => {
          if (turn !== asked.current) return;
          setYamlText(answer.yaml);
          setProblems(answer.problems);
          setReadable(true);
        })
        .catch(() => undefined);
    }, 250);
    return () => clearTimeout(timer);
  }, [pkg, mode, ready]);

  // The text is the source in the YAML view: the form follows it while it reads.
  function onYaml(next: string) {
    setYamlText(next);
    const turn = ++asked.current;
    window.clearTimeout(yamlTimer.current);
    yamlTimer.current = window.setTimeout(() => {
      api<Check>("/api/packages/check", { method: "POST", body: JSON.stringify({ text: next }) })
        .then((answer) => {
          if (turn !== asked.current) return;
          setProblems(answer.problems);
          setReadable(answer.readable);
          if (answer.readable) setPkg(answer.package);
        })
        .catch(() => undefined);
    }, 300);
  }

  async function switchTo(next: "visual" | "yaml") {
    if (next === mode) return;
    if (next === "visual" && !readable) {
      setError(text("package.fixYamlFirst"));
      return;
    }
    setError("");
    if (next === "yaml") {
      // The form was the source: the text is written from it now, not from
      // an answer still on its way, then checked to place the problems.
      const written = await toYaml(pkg);
      if (written === null) return;
      onYaml(written);
    }
    setMode(next);
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      const body = JSON.stringify({ text: mode === "yaml" ? yamlText : JSON.stringify(pkg) });
      const row = id
        ? await api<PackageRow>(`/api/packages/${id}`, { method: "PUT", body })
        : await api<PackageRow>("/api/packages", { method: "POST", body });
      if (row.package) setPkg(row.package);
      if (mode === "visual") setYamlText(row.yaml ?? "");
      toast(text("package.saved", { name: row.name }));
      if (!id) navigate(`/paquets/${row.id}`, { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setSaving(false);
    }
  }

  if (!ready) return <LoadingLines lines={6} />;

  const general = problems.filter((problem) => !problem.path.startsWith("services["));

  return (
    <>
      <PageTitle
        title={id ? pkg.name || text("nav.packages") : text("package.new")}
        icon={PackageIcon}
        action={
          id && canDeploy ? (
            <span className="flex items-center gap-4">
              <TextButton type="button" onClick={() => void download({ id, name: pkg.name }, "yaml").catch((err) => toast(err.message, "bad"))}>
                {text("package.exportYaml")}
              </TextButton>
              <Button type="button" variant="secondary" onClick={() => navigate(`/nouveau?package=${id}`)}>
                {text("catalogue.install")}
              </Button>
            </span>
          ) : null
        }
      />
      <form onSubmit={save} className="flex flex-col gap-8">
        <Tabs
          label={text("package.view")}
          active={mode}
          onChange={(next) => void switchTo(next)}
          tabs={[
            { key: "visual", label: text("package.visual") },
            { key: "yaml", label: text("package.advanced"), mark: !readable },
          ]}
        />
        {mode === "yaml" ? (
          <div className="flex flex-col gap-4">
            <p className="max-w-[720px] text-muted">{text("package.yamlHint")}</p>
            <Code value={yamlText} onChange={onYaml} language="yaml" problems={lined(problems)} label={text("package.advanced")} minHeight="24rem" maxHeight="70dvh" readOnly={!canDeploy} />
            {problems.length > 0 ? <ProblemList problems={problems} /> : <p className="text-ok">{text("package.valid")}</p>}
          </div>
        ) : (
          <VisualEditor pkg={pkg} onChange={setPkg} problems={problems} general={general} />
        )}
        {error ? <ErrorText>{error}</ErrorText> : null}
        {canDeploy ? (
          <div className="flex items-center gap-4">
            <Button type="submit" disabled={saving}>
              {id ? text("common.save") : text("package.create")}
            </Button>
            {problems.length > 0 ? <span className="text-muted">{text("package.problemCount", { count: problems.length })}</span> : null}
          </div>
        ) : null}
      </form>
    </>
  );
}

function VisualEditor({
  pkg,
  onChange,
  problems,
  general,
}: {
  pkg: Package;
  onChange: (pkg: Package) => void;
  problems: PackageProblem[];
  general: PackageProblem[];
}) {
  function set<K extends keyof Package>(key: K, value: Package[K]) {
    onChange({ ...pkg, [key]: value });
  }

  return (
    <div className="flex flex-col gap-10">
      <section className="flex max-w-[720px] flex-col gap-4">
        <Field label={text("package.name")}>
          <Input value={pkg.name} onChange={(e) => set("name", e.target.value)} required />
        </Field>
        <Field label={text("package.summary")}>
          <Input value={pkg.summary ?? ""} onChange={(e) => set("summary", e.target.value || undefined)} />
        </Field>
        <Field label={text("package.category")}>
          <Input value={pkg.category ?? ""} onChange={(e) => set("category", e.target.value || undefined)} />
        </Field>
        <Inputs pkg={pkg} onChange={onChange} />
        <Field label={text("package.sharedEnv")} hint={text("package.sharedEnvHint")}>
          <EnvEditor value={pkg.env} onChange={(env) => set("env", env)} label={text("package.sharedEnv")} />
        </Field>
        {general.length > 0 ? <ProblemList problems={general} /> : null}
      </section>
      <ServicesEditor services={pkg.services} onChange={(services) => set("services", services)} problems={problems} />
    </div>
  );
}

// ServicesEditor edits the services of a package or of a running
// application: one card each, who waits for whom above them.
function ServicesEditor({
  services,
  onChange,
  problems,
}: {
  services: PackageService[];
  onChange: (services: PackageService[]) => void;
  problems: PackageProblem[];
}) {
  function setService(index: number, next: PackageService) {
    const updated = services.map((service, at) => {
      if (at === index) return next;
      // One public service only: choosing one leaves the others.
      if (next.public && service.public) return { ...service, public: false };
      return service;
    });
    // A renamed service stays the one the others wait for.
    const before = services[index]?.name;
    const renamed = before && before !== next.name;
    onChange(renamed ? updated.map((service) => ({ ...service, dependsOn: service.dependsOn?.map((name) => (name === before ? next.name : name)) })) : updated);
  }

  function addService() {
    let n = services.length + 1;
    while (services.some((service) => service.name === `service-${n}`)) n++;
    onChange([...services, { name: `service-${n}`, image: "" }]);
  }

  function removeService(index: number) {
    const gone = services[index].name;
    onChange(services.filter((_, at) => at !== index).map((service) => ({ ...service, dependsOn: service.dependsOn?.filter((name) => name !== gone) })));
  }

  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h2 className="text-base">{text("field.services")}</h2>
        <TextButton type="button" onClick={addService}>
          {text("package.addService")}
        </TextButton>
      </div>
      <Dependencies services={services} />
      {services.map((service, index) => (
        <ServiceCard
          key={index}
          service={service}
          others={services.filter((_, at) => at !== index).map((other) => other.name)}
          problems={problemsUnder(problems, `services[${index}]`).map((problem) => ({ ...problem, path: problem.path.slice(`services[${index}]`.length + 1) }))}
          onChange={(next) => setService(index, next)}
          onRemove={services.length > 1 ? () => removeService(index) : undefined}
        />
      ))}
    </section>
  );
}

// Dependencies draws who waits for whom, so that the order of start reads
// at a glance.
function Dependencies({ services }: { services: PackageService[] }) {
  const links = services.filter((service) => service.dependsOn?.length);
  if (links.length === 0) return null;
  return (
    <ul className="flex flex-col gap-1 rounded-md border border-line bg-surface p-3 font-mono text-[13px]" aria-label={text("package.dependencies")}>
      {links.map((service) => (
        <li key={service.name} className="flex flex-wrap items-center gap-2">
          <span>{service.name}</span>
          <span className="text-faint">{text("package.waitsFor")}</span>
          <Icon icon={ArrowRight} className="text-faint" />
          {service.dependsOn!.map((name) => (
            <span key={name} className="rounded border border-line px-1.5">
              {name}
            </span>
          ))}
        </li>
      ))}
    </ul>
  );
}

function Inputs({ pkg, onChange }: { pkg: Package; onChange: (pkg: Package) => void }) {
  const inputs = pkg.inputs ?? [];
  function setInputs(next: typeof inputs) {
    onChange({ ...pkg, inputs: next.length ? next : undefined });
  }
  return (
    <Group label={text("package.inputs")} hint={text("package.inputsHint")}>
      <div className="flex flex-col gap-3">
        {inputs.map((input, index) => (
          <div key={index} className="flex items-center gap-3">
            <Input
              value={input.key}
              placeholder="admin-email"
              aria-label={text("package.inputKey")}
              onChange={(e) => setInputs(inputs.map((item, at) => (at === index ? { ...item, key: e.target.value } : item)))}
            />
            <Input
              value={input.label}
              placeholder={text("package.inputLabel")}
              aria-label={text("package.inputLabel")}
              onChange={(e) => setInputs(inputs.map((item, at) => (at === index ? { ...item, label: e.target.value } : item)))}
            />
            <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => setInputs(inputs.filter((_, at) => at !== index))} />
          </div>
        ))}
        <div>
          <TextButton type="button" onClick={() => setInputs([...inputs, { key: "", label: "" }])}>
            {text("package.addInput")}
          </TextButton>
        </div>
      </div>
    </Group>
  );
}

type ServiceTab = "general" | "network" | "storage" | "files" | "env" | "health" | "resources";

const tabOf: Record<string, ServiceTab> = {
  name: "general",
  image: "general",
  public: "general",
  port: "general",
  replicas: "general",
  command: "general",
  args: "general",
  dependsOn: "general",
  ports: "network",
  volumes: "storage",
  files: "files",
  env: "env",
  health: "health",
  resources: "resources",
};

function ServiceCard({
  service,
  others,
  problems,
  onChange,
  onRemove,
}: {
  service: PackageService;
  others: string[];
  problems: PackageProblem[];
  onChange: (service: PackageService) => void;
  onRemove?: () => void;
}) {
  const [tab, setTab] = useState<ServiceTab>("general");
  const marked = useMemo(() => new Set(problems.map((problem) => tabOf[problem.path.split(/[.[]/)[0]] ?? "general")), [problems]);
  const shown = problems.filter((problem) => (tabOf[problem.path.split(/[.[]/)[0]] ?? "general") === tab);

  function set<K extends keyof PackageService>(key: K, value: PackageService[K]) {
    const next = { ...service, [key]: value };
    // An empty list is left out of the package, as one would write it.
    if (Array.isArray(value) && value.length === 0) delete next[key];
    if (value === undefined) delete next[key];
    onChange(next);
  }

  const tabs: { key: ServiceTab; label: string; mark?: boolean }[] = [
    { key: "general", label: text("package.tabGeneral") },
    { key: "network", label: text("package.tabNetwork") },
    { key: "storage", label: text("package.tabStorage") },
    { key: "files", label: text("package.tabFiles") },
    { key: "env", label: text("package.tabEnv") },
    { key: "health", label: text("package.tabHealth") },
    { key: "resources", label: text("package.tabResources") },
  ];

  return (
    <article className="flex flex-col gap-5 rounded-md border border-line bg-surface p-4">
      <header className="flex items-center justify-between gap-4">
        <h3 className="flex min-w-0 items-center gap-2">
          <span className="truncate font-mono">{service.name || "…"}</span>
          {service.public ? <span className="rounded border border-line px-1.5 text-[13px] text-muted">{text("common.published")}</span> : null}
        </h3>
        {onRemove ? <IconButton type="button" icon={Trash2} label={text("package.removeService", { name: service.name })} onClick={onRemove} /> : null}
      </header>
      <Tabs label={service.name} active={tab} onChange={setTab} tabs={tabs.map((item) => ({ ...item, mark: marked.has(item.key) }))} />
      {tab === "general" ? <GeneralTab service={service} others={others} set={set} onChange={onChange} /> : null}
      {tab === "network" ? <NetworkTab service={service} set={set} /> : null}
      {tab === "storage" ? <StorageTab service={service} set={set} /> : null}
      {tab === "files" ? <FilesTab service={service} set={set} /> : null}
      {tab === "env" ? (
        <Field label={text("package.tabEnv")} hint={text("package.serviceEnvHint")}>
          <EnvEditor value={service.env} onChange={(env) => set("env", env)} label={text("package.tabEnv")} />
        </Field>
      ) : null}
      {tab === "health" ? <HealthTab service={service} set={set} /> : null}
      {tab === "resources" ? <ResourcesTab service={service} set={set} /> : null}
      {shown.length > 0 ? <ProblemList problems={shown} /> : null}
    </article>
  );
}

type Setter = <K extends keyof PackageService>(key: K, value: PackageService[K]) => void;

function numberOrUndefined(value: string) {
  const n = Number(value);
  return value.trim() === "" || !Number.isFinite(n) ? undefined : Math.trunc(n);
}

// words splits a command as a shell would read it: spaces apart, quotes
// keeping a phrase whole.
export function words(line: string): string[] {
  const out: string[] = [];
  let current = "";
  let quote = "";
  let started = false;
  for (const char of line) {
    if (quote) {
      if (char === quote) quote = "";
      else current += char;
    } else if (char === '"' || char === "'") {
      quote = char;
      started = true;
    } else if (/\s/.test(char)) {
      if (started || current) out.push(current);
      current = "";
      started = false;
    } else {
      current += char;
    }
  }
  if (started || current) out.push(current);
  return out;
}

export function joinWords(list: string[] = []) {
  return list.map((word) => (word === "" || /[\s"']/.test(word) ? `"${word}"` : word)).join(" ");
}

// WordsInput keeps what the person types and gives the words once the line reads.
function WordsInput({ value, onChange, placeholder, label }: { value?: string[]; onChange: (value: string[]) => void; placeholder?: string; label: string }) {
  const [line, setLine] = useState(() => joinWords(value));
  return (
    <Input
      value={line}
      aria-label={label}
      placeholder={placeholder}
      autoCapitalize="off"
      spellCheck={false}
      className="font-mono"
      onChange={(e) => {
        setLine(e.target.value);
        onChange(words(e.target.value));
      }}
    />
  );
}

function GeneralTab({ service, others, set, onChange }: { service: PackageService; others: string[]; set: Setter; onChange: (service: PackageService) => void }) {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      <Field label={text("package.serviceName")}>
        <Input value={service.name} onChange={(e) => set("name", e.target.value)} required autoCapitalize="off" spellCheck={false} />
      </Field>
      <Field label={text("common.image")}>
        <Input value={service.image} onChange={(e) => set("image", e.target.value)} required placeholder="ghcr.io/acme/app:1.0" autoCapitalize="off" spellCheck={false} />
      </Field>
      <Field label={text("package.port")} hint={text("package.portHint")}>
        <Input inputMode="numeric" value={service.port ?? ""} onChange={(e) => set("port", numberOrUndefined(e.target.value))} />
      </Field>
      <Field label={text("package.replicas")}>
        <Input inputMode="numeric" value={service.replicas ?? ""} placeholder="1" onChange={(e) => set("replicas", numberOrUndefined(e.target.value))} />
      </Field>
      <label className="flex items-center gap-3 md:col-span-2">
        <input type="checkbox" checked={Boolean(service.public)} disabled={service.noPort} onChange={(e) => set("public", e.target.checked || undefined)} />
        <span>{text("package.public")}</span>
      </label>
      <label className="flex items-center gap-3 md:col-span-2">
        <input
          type="checkbox"
          checked={Boolean(service.noPort)}
          disabled={service.public}
          onChange={(e) => onChange({ ...service, noPort: e.target.checked || undefined, port: e.target.checked ? undefined : service.port, ports: e.target.checked ? undefined : service.ports })}
        />
        <span>{text("package.noPort")}</span>
      </label>
      <Field label={text("package.command")} hint={text("package.commandHint")}>
        <WordsInput value={service.command} onChange={(value) => set("command", value)} placeholder="/bin/app serve" label={text("package.command")} />
      </Field>
      <Field label={text("package.args")}>
        <WordsInput value={service.args} onChange={(value) => set("args", value)} placeholder="--port=8080" label={text("package.args")} />
      </Field>
      {others.length > 0 ? (
        <div className="md:col-span-2">
          <Group label={text("package.dependsOn")} hint={text("package.dependsOnHint")}>
            <div className="flex flex-wrap gap-4">
              {others.map((name) => (
                <label key={name} className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={service.dependsOn?.includes(name) ?? false}
                    onChange={(e) =>
                      set("dependsOn", e.target.checked ? [...(service.dependsOn ?? []), name] : (service.dependsOn ?? []).filter((item) => item !== name))
                    }
                  />
                  <span className="font-mono">{name}</span>
                </label>
              ))}
            </div>
          </Group>
        </div>
      ) : null}
    </div>
  );
}

function NetworkTab({ service, set }: { service: PackageService; set: Setter }) {
  const ports = service.ports ?? [];
  return (
    <Group label={text("package.ports")} hint={text("package.portsHint")}>
      <div className="flex flex-col gap-3">
        {ports.map((port, index) => (
          <div key={index} className="flex items-center gap-3">
            <Input
              value={port.name ?? ""}
              placeholder="metrics"
              aria-label={text("package.portName")}
              onChange={(e) => set("ports", ports.map((item, at) => (at === index ? { ...item, name: e.target.value || undefined } : item)))}
            />
            <Input
              inputMode="numeric"
              value={port.port || ""}
              placeholder="9100"
              aria-label={text("package.port")}
              onChange={(e) => set("ports", ports.map((item, at) => (at === index ? { ...item, port: numberOrUndefined(e.target.value) ?? 0 } : item)))}
            />
            <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => set("ports", ports.filter((_, at) => at !== index))} />
          </div>
        ))}
        <div>
          <TextButton type="button" onClick={() => set("ports", [...ports, { port: 0 }])}>
            {text("package.addPort")}
          </TextButton>
        </div>
      </div>
    </Group>
  );
}

function StorageTab({ service, set }: { service: PackageService; set: Setter }) {
  const volumes = service.volumes ?? [];
  return (
    <Group label={text("package.volumes")} hint={text("package.volumesHint")}>
      <div className="flex flex-col gap-3">
        {volumes.map((volume, index) => (
          <div key={index} className="flex flex-col gap-2">
            <span className="text-muted">{index === 0 ? text("package.mainDisk") : text("package.extraDisk", { n: index })}</span>
            <div className="flex items-center gap-3">
              <Input
                value={volume.path}
                placeholder="/data"
                aria-label={text("package.diskPath")}
                className="font-mono"
                onChange={(e) => set("volumes", volumes.map((item, at) => (at === index ? { ...item, path: e.target.value } : item)))}
              />
              <span className="flex shrink-0 items-center gap-2">
                <span className="w-20">
                  <Input
                    inputMode="numeric"
                    value={volume.sizeGi ?? ""}
                    placeholder="1"
                    aria-label={text("package.diskSize")}
                    onChange={(e) => set("volumes", volumes.map((item, at) => (at === index ? { ...item, sizeGi: numberOrUndefined(e.target.value) } : item)))}
                  />
                </span>
                <span className="text-muted">Gio</span>
              </span>
              <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => set("volumes", volumes.filter((_, at) => at !== index))} />
            </div>
          </div>
        ))}
        <div>
          <TextButton type="button" onClick={() => set("volumes", [...volumes, { path: "", sizeGi: 1 }])}>
            {text("package.addDisk")}
          </TextButton>
        </div>
      </div>
    </Group>
  );
}

function FilesTab({ service, set }: { service: PackageService; set: Setter }) {
  const files = service.files ?? [];
  function setFile(index: number, next: PackageFile) {
    set("files", files.map((item, at) => (at === index ? next : item)));
  }
  return (
    <Group label={text("package.files")} hint={text("package.filesHint")}>
      <div className="flex flex-col gap-6">
        {files.map((file, index) => (
          <div key={index} className="flex flex-col gap-2">
            <div className="flex items-center gap-3">
              <Input value={file.path} placeholder="/etc/app/config.ini" aria-label={text("package.filePath")} className="font-mono" onChange={(e) => setFile(index, { ...file, path: e.target.value })} />
              <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => set("files", files.filter((_, at) => at !== index))} />
            </div>
            <Code value={file.content} onChange={(content) => setFile(index, { ...file, content })} language={languageOf(file.path)} label={file.path || text("package.fileContent")} />
          </div>
        ))}
        <div>
          <TextButton type="button" onClick={() => set("files", [...files, { path: "", content: "" }])}>
            {text("package.addFile")}
          </TextButton>
        </div>
      </div>
    </Group>
  );
}

function HealthTab({ service, set }: { service: PackageService; set: Setter }) {
  const kind = !service.health ? "none" : service.health.http ? "http" : "tcp";
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      <Field label={text("package.health")} hint={text("package.healthHint")}>
        <Select
          value={kind}
          onChange={(e) => {
            const next = e.target.value;
            if (next === "none") set("health", undefined);
            else if (next === "http") set("health", { ...service.health, http: service.health?.http || "/health" });
            else set("health", { port: service.health?.port });
          }}
        >
          <option value="none">{text("package.healthDefault")}</option>
          <option value="http">HTTP</option>
          <option value="tcp">TCP</option>
        </Select>
      </Field>
      {kind === "http" ? (
        <Field label={text("package.healthPath")}>
          <Input value={service.health?.http ?? ""} className="font-mono" onChange={(e) => set("health", { ...service.health, http: e.target.value })} />
        </Field>
      ) : null}
      {kind !== "none" ? (
        <Field label={text("package.healthPort")} hint={text("package.healthPortHint")}>
          <Input inputMode="numeric" value={service.health?.port ?? ""} onChange={(e) => set("health", { ...service.health, port: numberOrUndefined(e.target.value) })} />
        </Field>
      ) : null}
    </div>
  );
}

function ResourcesTab({ service, set }: { service: PackageService; set: Setter }) {
  function setResource(key: "cpu" | "memory", value: string) {
    const next = { ...service.resources, [key]: value || undefined };
    set("resources", next.cpu || next.memory ? next : undefined);
  }
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      <Field label={text("package.cpu")} hint={text("package.cpuHint")}>
        <Input value={service.resources?.cpu ?? ""} placeholder="500m" className="font-mono" onChange={(e) => setResource("cpu", e.target.value)} />
      </Field>
      <Field label={text("package.memory")} hint={text("package.memoryHint")}>
        <Input value={service.resources?.memory ?? ""} placeholder="512Mi" className="font-mono" onChange={(e) => setResource("memory", e.target.value)} />
      </Field>
    </div>
  );
}

// EnvEditor writes variables as KEY=value lines, coloured, and gives them
// back as a map once each line reads.
function EnvEditor({ value, onChange, label }: { value?: Record<string, string>; onChange: (value: Record<string, string> | undefined) => void; label: string }) {
  const [lines, setLines] = useState(() =>
    Object.entries(value ?? {})
      .map(([key, item]) => `${key}=${item}`)
      .join("\n"),
  );
  const [bad, setBad] = useState<number[]>([]);

  function onText(next: string) {
    setLines(next);
    const map: Record<string, string> = {};
    const wrong: number[] = [];
    next.split("\n").forEach((line, index) => {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) return;
      const cut = trimmed.indexOf("=");
      if (cut <= 0) {
        wrong.push(index + 1);
        return;
      }
      map[trimmed.slice(0, cut).trim()] = trimmed.slice(cut + 1);
    });
    setBad(wrong);
    onChange(Object.keys(map).length ? map : undefined);
  }

  return (
    <Code
      value={lines}
      onChange={onText}
      language="env"
      label={label}
      minHeight="5rem"
      problems={bad.map((line) => ({ line, message: text("ui.uneVariableSEcritCleValeur") }))}
    />
  );
}

type AppPackage = { package: Package; yaml: string; problems: PackageProblem[]; address?: string };

// AppServices changes the services of a running application, as a package
// is edited: add one, remove one, give it disks, files or a health check.
// What would erase data is asked first.
export function AppServices() {
  useI18n();
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const toast = useToast();
  const { canDeploy } = useSession();
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [services, setServices] = useState<PackageService[]>([]);
  const [yamlText, setYamlText] = useState("");
  const [mode, setMode] = useState<"visual" | "yaml">("visual");
  const [problems, setProblems] = useState<PackageProblem[]>([]);
  const [readable, setReadable] = useState(true);
  const [ready, setReady] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [loss, setLoss] = useState("");
  const asked = useRef(0);
  const yamlTimer = useRef(0);

  useEffect(() => {
    api<AppPackage>(`/api/applications/${id}/package`)
      .then((answer) => {
        setName(answer.package.name);
        setAddress(answer.address ?? "");
        setServices(answer.package.services);
      })
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
  }, [id]);

  // Only the services change here: the variables keep their own screen.
  const shown: Package = useMemo(() => ({ type: "myinfra-package", version: 1, name, services }), [name, services]);

  useEffect(() => {
    if (!ready || mode !== "visual") return;
    const turn = ++asked.current;
    const timer = setTimeout(() => {
      api<{ yaml: string; problems: PackageProblem[] }>("/api/packages/format", { method: "POST", body: JSON.stringify({ package: shown }) })
        .then((answer) => {
          if (turn !== asked.current) return;
          setYamlText(answer.yaml);
          setProblems(answer.problems.filter((problem) => problem.path.startsWith("services")));
          setReadable(true);
        })
        .catch(() => undefined);
    }, 250);
    return () => clearTimeout(timer);
  }, [shown, mode, ready]);

  function onYaml(next: string) {
    setYamlText(next);
    const turn = ++asked.current;
    window.clearTimeout(yamlTimer.current);
    yamlTimer.current = window.setTimeout(() => {
      api<Check>("/api/packages/check", { method: "POST", body: JSON.stringify({ text: next }) })
        .then((answer) => {
          if (turn !== asked.current) return;
          setProblems(answer.problems);
          setReadable(answer.readable);
          if (answer.readable) setServices(answer.package.services ?? []);
        })
        .catch(() => undefined);
    }, 300);
  }

  async function switchTo(next: "visual" | "yaml") {
    if (next === mode) return;
    if (next === "visual" && !readable) {
      setError(text("package.fixYamlFirst"));
      return;
    }
    setError("");
    if (next === "yaml") {
      const written = await toYaml(shown);
      if (written === null) return;
      onYaml(written);
    }
    setMode(next);
  }

  async function save(confirm: boolean) {
    setSaving(true);
    setError("");
    try {
      await api(`/api/applications/${id}/services`, { method: "PUT", body: JSON.stringify({ services, confirm }) });
      toast(text("appServices.saved"));
      navigate(`/applications/${id}`);
    } catch (err) {
      if (err instanceof ApiError && err.code === "services_data_loss") {
        setLoss(err.message);
      } else {
        setError(err instanceof Error ? err.message : text("common.error"));
      }
    } finally {
      setSaving(false);
    }
  }

  async function keep() {
    try {
      const row = await api<PackageRow>(`/api/applications/${id}/package`, { method: "POST" });
      navigate(`/paquets/${row.id}`);
    } catch (err) {
      toast(err instanceof Error ? err.message : text("common.error"), "bad");
    }
  }

  if (!ready) return <LoadingLines lines={6} />;

  return (
    <>
      <PageTitle
        title={text("appServices.title", { name })}
        icon={PackageIcon}
        action={
          canDeploy ? (
            <TextButton type="button" onClick={() => void keep()}>
              {text("appServices.keep")}
            </TextButton>
          ) : null
        }
      />
      <form
        className="flex flex-col gap-8"
        onSubmit={(event) => {
          event.preventDefault();
          void save(false);
        }}
      >
        <p className="max-w-[720px] text-muted">{text("appServices.intro")}</p>
        {address ? <p className="-mt-4 max-w-[720px] text-faint">{text("address.hint", { address })}</p> : null}
        <Tabs
          label={text("package.view")}
          active={mode}
          onChange={(next) => void switchTo(next)}
          tabs={[
            { key: "visual", label: text("package.visual") },
            { key: "yaml", label: text("package.advanced"), mark: !readable },
          ]}
        />
        {mode === "yaml" ? (
          <div className="flex flex-col gap-4">
            <Code value={yamlText} onChange={onYaml} language="yaml" problems={lined(problems)} label={text("package.advanced")} minHeight="24rem" maxHeight="70dvh" readOnly={!canDeploy} />
            {problems.length > 0 ? <ProblemList problems={problems} /> : null}
          </div>
        ) : (
          <ServicesEditor services={services} onChange={setServices} problems={problems} />
        )}
        {error ? <ErrorText>{error}</ErrorText> : null}
        {canDeploy ? (
          <div className="flex items-center gap-4">
            <Button type="submit" disabled={saving || problems.length > 0}>
              {text("appServices.apply")}
            </Button>
            <TextButton type="button" onClick={() => navigate(`/applications/${id}`)}>
              {text("common.cancel")}
            </TextButton>
          </div>
        ) : null}
      </form>
      {loss ? (
        <ConfirmDialog
          title={text("appServices.lossTitle")}
          confirmLabel={text("appServices.lossConfirm")}
          danger
          phrase={name}
          phraseLabel={text("field.name")}
          onConfirm={async () => {
            setLoss("");
            await save(true);
          }}
          onClose={() => setLoss("")}
        >
          {loss}
        </ConfirmDialog>
      ) : null}
    </>
  );
}
