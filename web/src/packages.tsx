import { FormEvent, Suspense, lazy, useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { ArrowRight, BookOpen, Copy, LayoutGrid, Package as PackageIcon, Plus, Ship, Trash2, Upload } from "lucide-react";
import { ApiError, api, peek, type Package, type PackageAbout, type PackageDisk, type PackageFile, type PackageProblem, type PackageRow, type PackageService } from "./api";
import { Code, languageOf } from "./code-lazy";
import { knownMessage, text, type MsgKey } from "./i18n";
import { useI18n } from "./lang";
import { AppsTabs, ConfirmDialog, Sheet, Tabs, useToast } from "./kit";
import { useSession } from "./session";
import { ToolInstall } from "./tools";
import { AppLogo } from "./app-logo";
import {
  ActionsMenu,
  Button,
  ErrorText,
  Field,
  Fold,
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

// A source the editor opens before it is kept: a docker-compose file
// converted, or a package that still has problems.
type Warning = { service?: string; message: string };
type Converted = { format: string; package: Package; yaml: string; warnings: Warning[]; problems: PackageProblem[] };
type Draft = { yaml: string; warnings: Warning[]; format: string };

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

// examples are packages to start from and learn by: each shows a part of
// what a package holds, and installs as it is.
const examples: { key: string; title: () => string; summary: () => string; pkg: () => Package }[] = [
  {
    key: "umami",
    title: () => text("package.exampleUmami"),
    summary: () => text("package.exampleUmamiHint"),
    pkg: (): Package => ({
      type: "myinfra-package",
      version: 1,
      name: "Umami",
      summary: text("package.exampleUmamiHint"),
      env: {
        APP_SECRET: "{{secret:app-secret}}",
        DATABASE_URL: "postgresql://umami:{{secret:db-password}}@db:5432/umami",
        POSTGRES_PASSWORD: "{{secret:db-password}}",
      },
      services: [
        {
          name: "web",
          image: "ghcr.io/umami-software/umami:postgresql-latest",
          public: true,
          port: 3000,
          env: { DATABASE_TYPE: "postgresql" },
          health: { http: "/api/heartbeat" },
          dependsOn: ["db"],
        },
        {
          name: "db",
          image: "postgres:16-alpine",
          port: 5432,
          volumes: [{ path: "/var/lib/postgresql/data", sizeGi: 5 }],
          env: { POSTGRES_DB: "umami", POSTGRES_USER: "umami" },
          resources: { memory: "256Mi" },
        },
      ],
    }),
  },
  {
    key: "site",
    title: () => text("package.exampleSite"),
    summary: () => text("package.exampleSiteHint"),
    pkg: (): Package => ({
      type: "myinfra-package",
      version: 1,
      name: text("package.exampleSite"),
      summary: text("package.exampleSiteHint"),
      services: [
        {
          name: "web",
          image: "nginx:1.27-alpine",
          public: true,
          port: 80,
          files: [
            { path: "/usr/share/nginx/html/index.html", content: "<!doctype html>\n<h1>{{host}}</h1>\n<p>Hello!</p>\n" },
            {
              path: "/etc/nginx/conf.d/default.conf",
              content: 'server {\n  listen 80;\n  root /usr/share/nginx/html;\n  location = /healthz { return 200 "ok\\n"; }\n}\n',
            },
          ],
          health: { http: "/healthz" },
          resources: { cpu: "100m", memory: "64Mi" },
        },
      ],
    }),
  },
];

export function Packages() {
  useI18n();
  const navigate = useNavigate();
  const { canDeploy } = useSession();
  const [rows, setRows] = useState<PackageRow[] | null>(() => peek<PackageRow[]>("/api/packages"));
  const [error, setError] = useState("");
  const [starting, setStarting] = useState(false);
  const [importing, setImporting] = useState<"file" | "helm" | null>(null);
  const [copying, setCopying] = useState(false);
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

  function start(choice: StartChoice) {
    setStarting(false);
    if (choice === "blank") navigate("/applications/paquets/nouveau");
    else if (choice === "file" || choice === "helm") setImporting(choice);
    else if (choice === "catalogue") setCopying(true);
    else navigate("/applications/paquets/nouveau", { state: { example: choice } });
  }

  return (
    <>
      <PageTitle
        title={text("nav.apps")}
        icon={LayoutGrid}
        action={
          canDeploy && rows && rows.length > 0 ? (
            <Button type="button" onClick={() => setStarting(true)}>
              <Icon icon={Plus} />
              {text("package.new")}
            </Button>
          ) : null
        }
      />
      <AppsTabs active="packages" />
      <PackagesIntro open={rows !== null && rows.length === 0} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {rows === null ? (
        <LoadingLines lines={4} />
      ) : rows.length === 0 ? (
        canDeploy ? (
          <section className="flex flex-col gap-4">
            <h2 className="text-base">{text("package.startTitle")}</h2>
            <StartChoices onChoose={start} />
          </section>
        ) : (
          <p className="text-muted">{text("package.empty")}</p>
        )
      ) : (
        <ul className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {rows.map((row) => (
            <li key={row.id} className="flex flex-col gap-3 rounded-md border border-line bg-surface p-4">
              <div className="flex items-start justify-between gap-3">
                <Link to={`/applications/paquets/${row.id}`} className="flex min-w-0 items-start gap-3 text-ink">
                  <AppLogo slug={row.logo ?? ""} name={row.name} size={36} />
                  <span className="min-w-0">
                    <span className="font-medium">{row.name}</span>
                    {row.summary ? <p className="mt-1 text-muted">{row.summary}</p> : null}
                  </span>
                </Link>
                <ActionsMenu label={text("apps.more")}>
                  <MenuItem onClick={() => exported(row, "yaml")}>{text("package.exportYaml")}</MenuItem>
                  <MenuItem onClick={() => exported(row, "json")}>{text("package.exportJson")}</MenuItem>
                  {canDeploy ? (
                    <MenuItem tone="danger" onClick={() => setRemoving(row)}>
                      {text("common.delete")}
                    </MenuItem>
                  ) : null}
                </ActionsMenu>
              </div>
              <p className="flex flex-wrap gap-2" aria-label={text("field.services")}>
                {row.services.map((service) => (
                  <span key={service} className="rounded border border-line px-1.5 font-mono text-[13px] text-muted">
                    {service}
                  </span>
                ))}
              </p>
              <div className="mt-auto flex flex-wrap items-center gap-4">
                {canDeploy ? (
                  <Button type="button" variant="secondary" onClick={() => navigate(`/nouveau?package=${row.id}`)}>
                    {text("package.installCta")}
                  </Button>
                ) : null}
                <Link to={`/applications/paquets/${row.id}`} className="text-ink underline">
                  {canDeploy ? text("package.edit") : text("package.open")}
                </Link>
              </div>
            </li>
          ))}
        </ul>
      )}
      {starting ? (
        <Sheet title={text("package.startTitle")} onClose={() => setStarting(false)} wide>
          <StartChoices onChoose={start} />
        </Sheet>
      ) : null}
      {importing ? (
        <ImportSheet
          initial={importing}
          onClose={() => setImporting(null)}
          onDone={(row) => navigate(`/applications/paquets/${row.id}`, { state: { saved: true } })}
          onDraft={(draft) => navigate("/applications/paquets/nouveau", { state: { draft } })}
        />
      ) : null}
      {copying ? <CopyArticleSheet onClose={() => setCopying(false)} onDone={(row) => navigate(`/applications/paquets/${row.id}`, { state: { saved: true } })} /> : null}
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

// PackagesIntro tells in three steps what a package is for: it stays open
// until the first package is kept.
function PackagesIntro({ open }: { open: boolean }) {
  const steps = [
    { title: text("package.step1"), body: text("package.step1Hint") },
    { title: text("package.step2"), body: text("package.step2Hint") },
    { title: text("package.step3"), body: text("package.step3Hint") },
  ];
  return (
    <details className="mb-8 max-w-[960px] rounded-md border border-line bg-surface p-4" open={open}>
      <summary className="cursor-pointer font-medium">{text("package.whatTitle")}</summary>
      <p className="mt-3 text-muted">{text("package.whatHint")}</p>
      <ol className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-3">
        {steps.map((step, index) => (
          <li key={index} className="flex gap-3">
            <span className="flex size-7 shrink-0 items-center justify-center rounded-full border border-line font-mono text-[13px]" aria-hidden>
              {index + 1}
            </span>
            <span>
              <span className="block">{step.title}</span>
              <span className="block text-muted">{step.body}</span>
            </span>
          </li>
        ))}
      </ol>
    </details>
  );
}

type StartChoice = "blank" | "file" | "helm" | "catalogue" | string;

// StartChoices are the ways to begin a package, the simplest first.
function StartChoices({ onChoose }: { onChoose: (choice: StartChoice) => void }) {
  const choices: { key: StartChoice; title: string; body: string; icon: typeof Plus }[] = [
    ...examples.map((example) => ({ key: example.key, title: example.title(), body: example.summary(), icon: BookOpen })),
    { key: "catalogue", title: text("package.fromCatalogue"), body: text("package.fromCatalogueHint"), icon: Copy },
    { key: "file", title: text("package.fromCompose"), body: text("package.fromComposeHint"), icon: Upload },
    { key: "helm", title: text("package.fromHelm"), body: text("package.fromHelmChoice"), icon: Ship },
    { key: "blank", title: text("package.fromBlank"), body: text("package.fromBlankHint"), icon: Plus },
  ];
  return (
    <ul className="grid grid-cols-1 gap-3 md:grid-cols-2">
      {choices.map((choice) => (
        <li key={choice.key}>
          <button
            type="button"
            onClick={() => onChoose(choice.key)}
            className="flex h-full w-full items-start gap-3 rounded-md border border-line bg-surface p-4 text-left hover:bg-status focus-visible:outline focus-visible:outline-2 focus-visible:outline-focus"
          >
            <Icon icon={choice.icon} className="mt-1 shrink-0" />
            <span>
              <span className="block font-medium">{choice.title}</span>
              <span className="block text-muted">{choice.body}</span>
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

// CopyArticleSheet copies an application of the catalogue into a package,
// to change what the catalogue does not let change.
function CopyArticleSheet({ onClose, onDone }: { onClose: () => void; onDone: (row: PackageRow) => void }) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <Sheet title={text("package.fromCatalogue")} onClose={onClose} busy={busy} wide>
      <div className="flex flex-col gap-4">
        <p className="text-muted">{text("package.fromCatalogueHint")}</p>
        {error ? <ErrorText>{error}</ErrorText> : null}
        <Suspense fallback={<LoadingLines lines={3} />}>
          <CatalogueBrowser
            selected=""
            onSelect={(card) => {
              setBusy(true);
              api<PackageRow>(`/api/catalogue/${card.id}/copy`, { method: "POST" })
                .then(onDone)
                .catch((err) => {
                  setError(err instanceof Error ? err.message : text("common.error"));
                  setBusy(false);
                });
            }}
          />
        </Suspense>
      </div>
    </Sheet>
  );
}

const CatalogueBrowser = lazy(() => import("./catalogue").then((module) => ({ default: module.CatalogueBrowser })));

// ImportSheet brings a package from a file, a pasted text or a link. What is
// wrong in it shows with its line, in the text itself.
function ImportSheet({
  initial = "file",
  onClose,
  onDone,
  onDraft,
}: {
  initial?: "file" | "helm";
  onClose: () => void;
  onDone: (row: PackageRow) => void;
  onDraft: (draft: Draft) => void;
}) {
  const [how, setHow] = useState<"file" | "paste" | "url" | "helm">(initial);
  const [chart, setChart] = useState({ repo: "", chart: "", version: "", values: "" });
  const [needsHelm, setNeedsHelm] = useState(false);
  const [pasted, setPasted] = useState("");
  const [url, setUrl] = useState("");
  const [problems, setProblems] = useState<PackageProblem[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  // Each source is converted first: a valid package is kept at once, a
  // docker-compose file or a package with problems opens in the editor.
  async function send(body: { text?: string; url?: string; name?: string; helm?: typeof chart }) {
    setBusy(true);
    setError("");
    setProblems([]);
    setNeedsHelm(false);
    try {
      const converted = await api<Converted>("/api/packages/convert", { method: "POST", body: JSON.stringify(body) });
      if (converted.format === "package" && converted.problems.length === 0) {
        onDone(await api<PackageRow>("/api/packages/import", { method: "POST", body: JSON.stringify({ text: converted.yaml }) }));
      } else {
        onDraft({ yaml: converted.yaml, warnings: converted.warnings, format: converted.format });
      }
    } catch (err) {
      if (err instanceof ApiError && err.code === "helm_missing") {
        setNeedsHelm(true);
      }
      setError(err instanceof Error ? err.message : text("common.error"));
      // A text that does not read shows its problems at their line.
      if (body.text) {
        const check = await api<Check>("/api/packages/check", { method: "POST", body: JSON.stringify({ text: body.text }) }).catch(() => null);
        if (check) setProblems(check.problems);
      }
    } finally {
      setBusy(false);
    }
  }

  async function onFile(file: File) {
    const content = await file.text();
    setPasted(content);
    setHow("paste");
    await send({ text: content, name: nameOfFile(file.name) });
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
            { key: "helm", label: text("package.fromHelm") },
          ]}
        />
        {how !== "helm" ? <p className="text-muted">{text("package.importHint")}</p> : null}
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
              void send({ text: pasted });
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
              void send({ url });
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
        {how === "helm" ? (
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void send({ helm: chart });
            }}
          >
            <p className="text-muted">{text("package.helmHint")}</p>
            <Field label={text("package.helmRepo")}>
              <Input value={chart.repo} onChange={(e) => setChart({ ...chart, repo: e.target.value })} placeholder="https://charts.js.wiki" autoCapitalize="off" spellCheck={false} />
            </Field>
            <Field label={text("package.helmChart")} hint={text("package.helmChartHint")}>
              <Input value={chart.chart} onChange={(e) => setChart({ ...chart, chart: e.target.value })} placeholder="wiki" required autoCapitalize="off" spellCheck={false} />
            </Field>
            <Field label={text("package.helmVersion")}>
              <Input value={chart.version} onChange={(e) => setChart({ ...chart, version: e.target.value })} placeholder="2.3.1" autoCapitalize="off" spellCheck={false} />
            </Field>
            <Field label={text("package.helmValues")}>
              <Code value={chart.values} onChange={(values) => setChart({ ...chart, values })} language="yaml" label={text("package.helmValues")} placeholder={"persistence:\n  size: 4Gi"} minHeight="6rem" maxHeight="40dvh" />
            </Field>
            {needsHelm ? <ToolInstall helm onChange={(tools) => tools.helm && void send({ helm: chart })} /> : null}
            <div>
              <Button type="submit" disabled={busy || !chart.chart.trim()}>
                {busy ? text("package.helmRendering") : text("package.helmRender")}
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

// nameOfFile names a package after its file: wordpress.compose.yaml is
// wordpress, docker-compose.yml nothing (the file may name itself).
function nameOfFile(file: string) {
  return file
    .replace(/\.(ya?ml|json)$/i, "")
    .replace(/[._-]?(docker-)?compose$/i, "")
    .replace(/^(docker-)?compose[._-]?/i, "")
    .trim();
}

function lined(problems: PackageProblem[]) {
  return problems.filter((problem) => problem.line).map((problem) => ({ line: problem.line!, message: problem.message }));
}

// WarningList shows what an import left out, for the person to check
// before keeping the package.
function WarningList({ warnings }: { warnings: Warning[] }) {
  return (
    <section className="flex max-w-[720px] flex-col gap-2 rounded-md border border-line bg-surface p-4" aria-label={text("package.importWarnings")}>
      <h2 className="text-base text-warn">{text("package.importWarnings")}</h2>
      <ul className="flex list-disc flex-col gap-1 pl-5">
        {warnings.map((warning, index) => (
          <li key={index}>
            {warning.service ? <span className="font-mono text-[13px]">{warning.service} · </span> : null}
            {warning.message}
          </li>
        ))}
      </ul>
    </section>
  );
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
  const location = useLocation();
  const [warnings, setWarnings] = useState<Warning[]>([]);
  const [guide, setGuide] = useState(false);
  const [saved, setSaved] = useState(false);
  // revision rebuilds the form when a whole package replaces the one shown:
  // its fields keep what is typed, and would keep the old text otherwise.
  const [revision, setRevision] = useState(0);

  // An import opens here in the YAML view, with what it could not keep; an
  // example opens in the form, to learn by changing it.
  useEffect(() => {
    const state = location.state as { draft?: Draft; example?: string; saved?: boolean } | null;
    if (id) {
      setSaved(Boolean(state?.saved));
      return;
    }
    const example = examples.find((item) => item.key === state?.example);
    if (example) {
      setPkg(example.pkg());
      setRevision((current) => current + 1);
    }
    if (!state?.draft) return;
    setWarnings(state.draft.warnings);
    setMode("yaml");
    onYaml(state.draft.yaml);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, location.state]);

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
    // The turn is taken when the call leaves: a call cancelled before never
    // makes an answer already asked for look old.
    const timer = setTimeout(() => {
      const turn = ++asked.current;
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
      setWarnings([]);
      if (!id) navigate(`/applications/paquets/${row.id}`, { replace: true, state: { saved: true } });
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
          <span className="flex items-center gap-4">
            <TextButton type="button" onClick={() => setGuide(true)}>
              <span className="inline-flex items-center gap-2">
                <Icon icon={BookOpen} />
                {text("package.guide")}
              </span>
            </TextButton>
            {id ? (
              <TextButton type="button" onClick={() => void download({ id, name: pkg.name }, "yaml").catch((err) => toast(err.message, "bad"))}>
                {text("package.exportYaml")}
              </TextButton>
            ) : null}
            {id && canDeploy ? (
              <Button type="button" variant="secondary" onClick={() => navigate(`/nouveau?package=${id}`)}>
                {text("package.installCta")}
              </Button>
            ) : null}
          </span>
        }
      />
      {saved && id && canDeploy ? (
        <section className="mb-8 flex max-w-[960px] flex-wrap items-center justify-between gap-4 rounded-md border border-line bg-surface p-4" role="status">
          <span>
            <span className="block font-medium">{text("package.readyTitle")}</span>
            <span className="block text-muted">{text("package.readyHint")}</span>
          </span>
          <span className="flex items-center gap-4">
            <Button type="button" onClick={() => navigate(`/nouveau?package=${id}`)}>
              {text("package.installNow")}
            </Button>
            <TextButton type="button" onClick={() => setSaved(false)}>
              {text("package.later")}
            </TextButton>
          </span>
        </section>
      ) : null}
      {!id ? <p className="-mt-4 mb-8 max-w-[720px] text-muted md:-mt-8">{text("package.newHint")}</p> : null}
      {guide ? <PackageGuide onClose={() => setGuide(false)} /> : null}
      <form onSubmit={save} className="flex flex-col gap-8">
        {warnings.length > 0 ? <WarningList warnings={warnings} /> : null}
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
          <VisualEditor key={revision} pkg={pkg} onChange={setPkg} problems={problems} general={general} />
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
        <AboutFields about={pkg.about} onChange={(about) => set("about", about)} />
        <Inputs pkg={pkg} onChange={onChange} />
        <Field label={text("package.sharedEnv")} hint={text("package.sharedEnvHint")}>
          <EnvEditor value={pkg.env} onChange={(env) => set("env", env)} label={text("package.sharedEnv")} snippets={sharedSnippets()} />
        </Field>
        <SharedDisks disks={pkg.disks ?? []} onChange={(disks) => set("disks", disks.length > 0 ? disks : undefined)} />
        {general.length > 0 ? <ProblemList problems={general} /> : null}
      </section>
      <ServicesEditor services={pkg.services} onChange={(services) => set("services", services)} problems={problems} disks={(pkg.disks ?? []).map((disk) => disk.name).filter(Boolean)} />
    </div>
  );
}

// SharedDisks declares the disks several services mount: a name and a size,
// once for the package.
function SharedDisks({ disks, onChange }: { disks: PackageDisk[]; onChange: (disks: PackageDisk[]) => void }) {
  function setDisk(index: number, next: PackageDisk) {
    onChange(disks.map((disk, at) => (at === index ? next : disk)));
  }
  return (
    <Group label={text("package.sharedDisks")} hint={text("package.sharedDisksHint")}>
      <div className="flex flex-col gap-3">
        {disks.map((disk, index) => (
          <div key={index} className="flex items-center gap-3">
            <Input
              value={disk.name}
              placeholder="media"
              aria-label={text("package.diskName")}
              className="font-mono"
              autoCapitalize="off"
              spellCheck={false}
              onChange={(e) => setDisk(index, { ...disk, name: e.target.value })}
            />
            <span className="flex shrink-0 items-center gap-2">
              <span className="w-20">
                <Input inputMode="numeric" value={disk.sizeGi || ""} placeholder="10" aria-label={text("package.diskSize")} onChange={(e) => setDisk(index, { ...disk, sizeGi: numberOrUndefined(e.target.value) ?? 0 })} />
              </span>
              <span className="text-muted">Gio</span>
            </span>
            <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => onChange(disks.filter((_, at) => at !== index))} />
          </div>
        ))}
        <div>
          <TextButton type="button" onClick={() => onChange([...disks, { name: disks.length === 0 ? "media" : `disk-${disks.length + 1}`, sizeGi: 10 }])}>
            {text("package.addSharedDisk")}
          </TextButton>
        </div>
      </div>
    </Group>
  );
}

// ServicesEditor edits the services of a package or of a running
// application: one card each, who waits for whom above them.
function ServicesEditor({
  services,
  onChange,
  problems,
  disks,
}: {
  services: PackageService[];
  onChange: (services: PackageService[]) => void;
  problems: PackageProblem[];
  // disks are the shared disks a volume may mount; without, those the
  // services mount already.
  disks?: string[];
}) {
  const shared = disks ?? [...new Set(services.flatMap((service) => (service.volumes ?? []).flatMap((volume) => (volume.disk ? [volume.disk] : []))))];
  function setService(index: number, next: PackageService) {
    // Several services may be public: each gets its own names.
    const updated = services.map((service, at) => (at === index ? next : service));
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
          disks={shared}
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

// AboutFields present a package: its site, its logo and a longer
// description. Translations are written in the advanced view.
function AboutFields({ about, onChange }: { about?: PackageAbout; onChange: (about: PackageAbout | undefined) => void }) {
  function set(key: "site" | "logo" | "description", value: string) {
    const next: PackageAbout = { ...about, [key]: value || undefined };
    const empty = !next.site && !next.logo && !next.description && !next.translations;
    onChange(empty ? undefined : next);
  }
  return (
    <Fold title={text("package.about")} defaultOpen={Boolean(about)}>
      <div className="flex flex-col gap-4">
        <p className="text-faint">{text("package.aboutHint")}</p>
        <Field label={text("package.site")}>
          <Input value={about?.site ?? ""} onChange={(e) => set("site", e.target.value)} placeholder="https://umami.is" autoCapitalize="off" spellCheck={false} />
        </Field>
        <Field label={text("package.logo")} hint={text("package.logoHint")}>
          <span className="flex items-center gap-3">
            <AppLogo slug={about?.logo ?? ""} name={about?.logo || "?"} size={36} />
            <Input value={about?.logo ?? ""} onChange={(e) => set("logo", e.target.value.toLowerCase())} placeholder="umami" autoCapitalize="off" spellCheck={false} />
          </span>
        </Field>
        <Field label={text("package.description")}>
          <Input value={about?.description ?? ""} onChange={(e) => set("description", e.target.value)} />
        </Field>
      </div>
    </Fold>
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

// tabTip gives each tab a concrete example, for a first package.
const tabTip: Record<ServiceTab, MsgKey> = {
  general: "package.tipGeneral",
  network: "package.tipNetwork",
  storage: "package.tipStorage",
  files: "package.tipFiles",
  env: "package.tipEnv",
  health: "package.tipHealth",
  resources: "package.tipResources",
};

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
  disks,
  problems,
  onChange,
  onRemove,
}: {
  service: PackageService;
  others: string[];
  disks: string[];
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
      <p className="text-faint">{text(tabTip[tab])}</p>
      {tab === "general" ? <GeneralTab service={service} others={others} set={set} onChange={onChange} /> : null}
      {tab === "network" ? <NetworkTab service={service} set={set} /> : null}
      {tab === "storage" ? <StorageTab service={service} set={set} disks={disks} /> : null}
      {tab === "files" ? <FilesTab service={service} set={set} /> : null}
      {tab === "env" ? (
        <Field label={text("package.tabEnv")} hint={text("package.serviceEnvHint")}>
          <EnvEditor value={service.env} onChange={(env) => set("env", env)} label={text("package.tabEnv")} snippets={serviceSnippets()} />
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

function StorageTab({ service, set, disks }: { service: PackageService; set: Setter; disks: string[] }) {
  const volumes = service.volumes ?? [];
  const own = volumes.filter((volume) => !volume.disk);
  return (
    <Group label={text("package.volumes")} hint={text("package.volumesHint")}>
      <div className="flex flex-col gap-3">
        {volumes.map((volume, index) => (
          <div key={index} className="flex flex-col gap-2">
            <span className="text-muted">
              {volume.disk ? text("package.sharedDiskOf", { name: volume.disk }) : own.indexOf(volume) === 0 ? text("package.mainDisk") : text("package.extraDisk", { n: own.indexOf(volume) })}
            </span>
            <div className="flex items-center gap-3">
              <Input
                value={volume.path}
                placeholder="/data"
                aria-label={text("package.diskPath")}
                className="font-mono"
                onChange={(e) => set("volumes", volumes.map((item, at) => (at === index ? { ...item, path: e.target.value } : item)))}
              />
              {disks.length > 0 ? (
                <span className="w-40 shrink-0">
                  <Select
                    value={volume.disk ?? ""}
                    aria-label={text("package.diskKind")}
                    onChange={(e) =>
                      set(
                        "volumes",
                        volumes.map((item, at) => (at === index ? (e.target.value ? { path: item.path, disk: e.target.value } : { path: item.path, sizeGi: 1 }) : item)),
                      )
                    }
                  >
                    <option value="">{text("package.ownDisk")}</option>
                    {disks.map((name) => (
                      <option key={name} value={name}>
                        {name}
                      </option>
                    ))}
                  </Select>
                </span>
              ) : null}
              {volume.disk ? null : (
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
              )}
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
function EnvEditor({
  value,
  onChange,
  label,
  snippets = [],
}: {
  value?: Record<string, string>;
  onChange: (value: Record<string, string> | undefined) => void;
  label: string;
  snippets?: { label: string; line: string }[];
}) {
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
    <span className="flex flex-col gap-2">
      <Code
        value={lines}
        onChange={onText}
        language="env"
        label={label}
        placeholder="NOM=valeur"
        minHeight="5rem"
        problems={bad.map((line) => ({ line, message: text("ui.uneVariableSEcritCleValeur") }))}
      />
      {snippets.length > 0 ? (
        <span className="flex flex-wrap items-center gap-2">
          <span className="text-faint">{text("package.insert")}</span>
          {snippets.map((snippet) => (
            <button
              key={snippet.line}
              type="button"
              title={snippet.line}
              onClick={() => onText(lines.trim() ? `${lines.replace(/\n*$/, "")}\n${snippet.line}` : snippet.line)}
              className="rounded border border-line px-2 py-0.5 text-[13px] text-muted hover:text-ink focus-visible:outline focus-visible:outline-2 focus-visible:outline-focus"
            >
              + {snippet.label}
            </button>
          ))}
        </span>
      ) : null}
    </span>
  );
}

// sharedSnippets and serviceSnippets start the lines a first package needs.
// A secret belongs to the common variables: they are sealed.
function sharedSnippets() {
  return [
    { label: text("package.snippetSecret"), line: "PASSWORD={{secret:password}}" },
    { label: text("package.snippetUrl"), line: "PUBLIC_URL={{url}}" },
    { label: text("package.snippetHost"), line: "PUBLIC_HOST={{host}}" },
  ];
}

function serviceSnippets() {
  return [
    { label: text("package.snippetUrl"), line: "PUBLIC_URL={{url}}" },
    { label: text("package.snippetHost"), line: "PUBLIC_HOST={{host}}" },
  ];
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
  const [guide, setGuide] = useState(false);
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
    const timer = setTimeout(() => {
      const turn = ++asked.current;
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
      navigate(`/applications/paquets/${row.id}`);
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
          <span className="flex items-center gap-4">
            <TextButton type="button" onClick={() => setGuide(true)}>
              <span className="inline-flex items-center gap-2">
                <Icon icon={BookOpen} />
                {text("package.guide")}
              </span>
            </TextButton>
            {canDeploy ? (
              <TextButton type="button" onClick={() => void keep()}>
                {text("appServices.keep")}
              </TextButton>
            ) : null}
          </span>
        }
      />
      {guide ? <PackageGuide onClose={() => setGuide(false)} /> : null}
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

// guideSections teach a package one notion at a time, in the order a first
// package is written.
const guideSections: { title: MsgKey; body: MsgKey }[] = [
  { title: "guide.servicesTitle", body: "guide.servicesBody" },
  { title: "guide.publicTitle", body: "guide.publicBody" },
  { title: "guide.disksTitle", body: "guide.disksBody" },
  { title: "guide.filesTitle", body: "guide.filesBody" },
  { title: "guide.variablesTitle", body: "guide.variablesBody" },
  { title: "guide.orderTitle", body: "guide.orderBody" },
  { title: "guide.healthTitle", body: "guide.healthBody" },
  { title: "guide.installTitle", body: "guide.installBody" },
];

const guideTokens: { token: string; body: MsgKey }[] = [
  { token: "{{url}}", body: "guide.tokenUrl" },
  { token: "{{host}}", body: "guide.tokenHost" },
  { token: "{{secret:nom}}", body: "guide.tokenSecret" },
  { token: "{{encoded:nom}}", body: "guide.tokenEncoded" },
  { token: "{{input:clé}}", body: "guide.tokenInput" },
];

const guideFields: { field: string; body: MsgKey }[] = [
  { field: "name · summary · category", body: "guide.refName" },
  { field: "inputs[].key · label", body: "guide.refInputs" },
  { field: "env", body: "guide.refEnv" },
  { field: "services[].name · image", body: "guide.refService" },
  { field: "public · port", body: "guide.refPublic" },
  { field: "noPort", body: "guide.refNoPort" },
  { field: "replicas", body: "guide.refReplicas" },
  { field: "command · args", body: "guide.refCommand" },
  { field: "ports[].name · port", body: "guide.refPorts" },
  { field: "volumes[].path · sizeGi", body: "guide.refVolumes" },
  { field: "files[].path · content", body: "guide.refFiles" },
  { field: "services[].env", body: "guide.refServiceEnv" },
  { field: "health.http · port", body: "guide.refHealth" },
  { field: "resources.cpu · memory", body: "guide.refResources" },
  { field: "dependsOn", body: "guide.refDependsOn" },
  { field: "about.site · logo · description · translations", body: "guide.refAbout" },
];

// PackageGuide is the tutorial of packages: what each notion is for, the
// {{…}} MyInfra fills in, a whole example and every field of the format.
export function PackageGuide({ onClose }: { onClose: () => void }) {
  const [example, setExample] = useState("");
  useEffect(() => {
    void toYaml(examples[0].pkg()).then((text) => setExample(text ?? ""));
  }, []);
  return (
    <Sheet title={text("package.guide")} onClose={onClose} wide>
      <div className="flex flex-col gap-6">
        <p className="text-muted">{text("package.whatHint")}</p>
        {guideSections.map((section, index) => (
          <section key={section.title} className="flex flex-col gap-1">
            <h3 className="font-medium">
              {index + 1}. {text(section.title)}
            </h3>
            <p className="text-muted">{text(section.body)}</p>
          </section>
        ))}
        <section className="flex flex-col gap-2">
          <h3 className="font-medium">{text("guide.tokensTitle")}</h3>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
            {guideTokens.map((item) => (
              <div key={item.token} className="contents">
                <dt className="font-mono text-[13px]" style={{ color: "var(--color-code-token)" }}>
                  {item.token}
                </dt>
                <dd className="text-muted">{text(item.body)}</dd>
              </div>
            ))}
          </dl>
        </section>
        <section className="flex flex-col gap-2">
          <h3 className="font-medium">{text("guide.exampleTitle")}</h3>
          <p className="text-muted">{text("guide.exampleBody")}</p>
          {example ? <Code value={example} language="yaml" readOnly label={text("guide.exampleTitle")} minHeight="10rem" maxHeight="24rem" /> : <LoadingLines lines={3} />}
        </section>
        <section className="flex flex-col gap-2">
          <h3 className="font-medium">{text("guide.referenceTitle")}</h3>
          <dl className="flex flex-col gap-2">
            {guideFields.map((item) => (
              <div key={item.field}>
                <dt className="font-mono text-[13px]">{item.field}</dt>
                <dd className="text-muted">{text(item.body)}</dd>
              </div>
            ))}
          </dl>
        </section>
      </div>
    </Sheet>
  );
}
