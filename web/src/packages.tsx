import { FormEvent, Suspense, lazy, useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { ArrowLeft, ArrowRight, BookOpen, Copy, LayoutGrid, Package as PackageIcon, Plus, Ship, Trash2, Upload } from "lucide-react";
import { ApiError, api, peek, type Package, type PackageAbout, type PackageDisk, type PackageFile, type PackagePort, type PackageProblem, type PackageVolume, type PackageRow, type PackageService } from "./api";
import { Code, languageOf } from "./code-lazy";
import { currentLocale, knownMessage, text, type MsgKey } from "./i18n";
import { useI18n } from "./lang";
import { ChosenName, PlanPanel, packagePlan, underPlan } from "./plan";
import { AppsTabs, ConfirmDialog, Sheet, Spinner, Tabs, useToast, useWide } from "./kit";
import { useSession } from "./session";
import { ToolInstall } from "./tools";
import { AppLogo } from "./app-logo";
import { LibraryTag } from "./library-tag";
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
  cardGrid,
  useTrail,
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
    if (choice === "blank") navigate("/applications/paquets/nouveau", { state: { blank: true } });
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
            <Button type="button" onClick={() => navigate("/applications/paquets/nouveau")}>
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
        <ul className={cardGrid}>
          {rows.map((row) => (
            <li key={row.id} className="flex flex-col gap-3 rounded-md border border-line bg-surface p-4">
              <div className="flex items-start justify-between gap-3">
                <Link to={`/applications/paquets/${row.id}`} className="flex min-w-0 items-start gap-3 text-ink">
                  <AppLogo slug={row.logo ?? ""} name={row.name} size={36} />
                  <span className="min-w-0">
                    <span className="font-medium">{row.name}</span>
                    {row.summary ? <p className="mt-1 text-muted">{row.summary}</p> : null}
                    <LibraryTag mark={row.library} />
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

// PackageNew starts a package: from a docker-compose pasted here, from the
// catalogue, a chart or nothing, or from an example. A draft, an example or
// a blank start opens the editor itself.
export function PackageNew() {
  const location = useLocation();
  const state = location.state as { draft?: Draft; example?: string; blank?: boolean } | null;
  if (state?.draft || state?.example || state?.blank) return <PackageEditor />;
  return <PackageStart />;
}

function PackageStart() {
  useI18n();
  const navigate = useNavigate();
  const [compose, setCompose] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [importing, setImporting] = useState<"file" | "helm" | null>(null);
  const [copying, setCopying] = useState(false);
  const [about, setAbout] = useState(false);
  useTrail([
    { label: text("nav.apps"), to: "/applications" },
    { label: text("nav.packages"), to: "/applications/paquets" },
    { label: text("package.new") },
  ]);

  async function convert() {
    setBusy(true);
    setError("");
    try {
      const converted = await api<Converted>("/api/packages/convert", { method: "POST", body: JSON.stringify({ text: compose }) });
      if (converted.format === "package" && converted.problems.length === 0) {
        const row = await api<PackageRow>("/api/packages/import", { method: "POST", body: JSON.stringify({ text: converted.yaml }) });
        navigate(`/applications/paquets/${row.id}`, { state: { saved: true } });
      } else {
        navigate("/applications/paquets/nouveau", { state: { draft: { yaml: converted.yaml, warnings: converted.warnings, format: converted.format } } });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  const others: { key: StartChoice; title: string; body: string; icon: typeof Plus }[] = [
    { key: "catalogue", title: text("package.fromCatalogue"), body: text("package.fromCatalogueHint"), icon: Copy },
    { key: "file", title: text("package.fromCompose"), body: text("package.fromComposeHint"), icon: Upload },
    { key: "helm", title: text("package.fromHelm"), body: text("package.fromHelmChoice"), icon: Ship },
    { key: "blank", title: text("package.fromBlank"), body: text("package.fromBlankHint"), icon: Plus },
  ];

  function choose(key: StartChoice) {
    if (key === "blank") navigate("/applications/paquets/nouveau", { state: { blank: true } });
    else if (key === "file" || key === "helm") setImporting(key);
    else if (key === "catalogue") setCopying(true);
    else navigate("/applications/paquets/nouveau", { state: { example: key } });
  }

  return (
    <>
      <PageTitle
        inTree
        title={text("package.new")}
        action={
          <TextButton type="button" onClick={() => setAbout((open) => !open)} aria-expanded={about}>
            {text("package.whatTitle")}
          </TextButton>
        }
      />
      <div className="flex max-w-[860px] flex-col gap-8">
        <p className="text-muted">{text("package.whatHint")}</p>
        {about ? <PackagesIntro open /> : null}
        <section className="flex flex-col gap-3">
          <div>
            <h2>{text("package.composeTitle")}</h2>
            <p className="text-[13px] text-muted">{text("package.composeHint")}</p>
          </div>
          <Code value={compose} onChange={setCompose} language="yaml" label={text("package.composeTitle")} placeholder={"services:\n  web:\n    image: nginx:1.27"} minHeight="12rem" />
          {error ? <ErrorText>{error}</ErrorText> : null}
          <div className="flex flex-wrap items-center gap-4">
            <Button type="button" onClick={() => void convert()} disabled={busy || compose.trim() === ""}>
              {busy ? <Spinner /> : null}
              {text("package.convert")}
            </Button>
            <TextButton type="button" onClick={() => setImporting("file")}>
              <Icon icon={Upload} />
              {text("package.fromFile")}
            </TextButton>
          </div>
        </section>
        <section className="flex flex-col gap-3">
          <h2>{text("package.orFrom")}</h2>
          <ul className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {others.map((choice) => (
              <li key={choice.key}>
                <button type="button" onClick={() => choose(choice.key)} className={`card-link flex h-full w-full items-start gap-3 rounded-md border border-line bg-surface p-4 text-left ${focus}`}>
                  <Icon icon={choice.icon} className="mt-1 shrink-0 text-muted" />
                  <span>
                    <span className="block">{choice.title}</span>
                    <span className="block text-[13px] text-muted">{choice.body}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </section>
        <section className="flex flex-col gap-3">
          <h2>{text("package.examplesTitle")}</h2>
          <ul className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {examples.map((example) => {
              const pkg = example.pkg();
              return (
                <li key={example.key}>
                  <button type="button" onClick={() => choose(example.key)} className={`card-link flex h-full w-full flex-col gap-2 rounded-md border border-line bg-surface p-4 text-left ${focus}`}>
                    <span>{example.title()}</span>
                    <span className="text-[13px] text-muted">{example.summary()}</span>
                    <span className="flex flex-wrap gap-1.5">
                      {pkg.services.map((service) => (
                        <span key={service.name} className="rounded border border-line px-1.5 font-mono text-[12px] text-muted">
                          {service.name}
                        </span>
                      ))}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      </div>
      {importing ? (
        <ImportSheet
          initial={importing}
          onClose={() => setImporting(null)}
          onDone={(row) => navigate(`/applications/paquets/${row.id}`, { state: { saved: true } })}
          onDraft={(draft) => navigate("/applications/paquets/nouveau", { state: { draft } })}
        />
      ) : null}
      {copying ? <CopyArticleSheet onClose={() => setCopying(false)} onDone={(row) => navigate(`/applications/paquets/${row.id}`, { state: { saved: true } })} /> : null}
    </>
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
  // Choosing an element of the plan opens its settings below it.
  const [chosen, setChosen] = useState<string | null>(null);
  // The nav of the chosen element stays under the line of the title: it needs
  // its height.
  const line = useRef<HTMLDivElement>(null);
  const [lineReady, setLineReady] = useState(false);
  useEffect(() => {
    const node = line.current;
    const scroller = node?.closest("main");
    if (!node || !scroller) return;
    const observer = new ResizeObserver(() => scroller.style.setProperty("--line-height", `${Math.floor(node.getBoundingClientRect().height)}px`));
    observer.observe(node);
    return () => {
      observer.disconnect();
      scroller.style.removeProperty("--line-height");
    };
  }, [lineReady]);

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

  // What the plan has chosen sets what shows under it: a service alone, a
  // storage, or, with nothing chosen, the package itself.
  const serviceName = chosen?.startsWith("svc:") ? chosen.slice(4) : null;
  const diskName = chosen?.startsWith("disk:") ? chosen.slice(5) : null;
  const diskAt = diskName === null ? -1 : (pkg.disks ?? []).findIndex((disk) => disk.name === diskName);
  const at = serviceName !== null ? pkg.services.findIndex((service) => service.name === serviceName) : -1;
  const drawn = packagePlan(pkg);
  const storage = diskName !== null && (diskAt >= 0 || drawn.nodes.some((node) => node.key === chosen)) ? diskName : null;
  // A service out of sight still says that something is wrong with it.
  const plan: typeof drawn = {
    ...drawn,
    nodes: drawn.nodes.map((node) => {
      const index = pkg.services.findIndex((service) => `svc:${service.name}` === node.key);
      return index >= 0 && problemsUnder(problems, `services[${index}]`).length > 0 ? { ...node, status: "bad" as const } : node;
    }),
  };
  const selected = mode === "visual" && plan.nodes.some((node) => node.key === chosen) ? chosen : null;
  const picked = plan.nodes.find((node) => node.key === selected);

  // Renaming what is chosen keeps it chosen, even while its name is empty.
  function edit(next: Package) {
    if (serviceName !== null && at >= 0 && next.services.length === pkg.services.length && next.services[at].name !== serviceName) setChosen(`svc:${next.services[at].name}`);
    const disk = next.disks?.[diskAt];
    if (diskAt >= 0 && disk && next.disks?.length === pkg.disks?.length && disk.name !== diskName) setChosen(`disk:${disk.name}`);
    setPkg(next);
  }

  // A new service is chosen at once, so that its settings open.
  function addService() {
    const name = freeServiceName(pkg.services);
    setPkg({ ...pkg, services: [...pkg.services, { name, image: "" }] });
    setChosen(`svc:${name}`);
  }

  // A new storage is a shared disk: dashed in the plan until a service mounts it.
  function addStorage() {
    const disks = pkg.disks ?? [];
    let n = disks.length + 1;
    let name = disks.length === 0 ? "media" : `disk-${n}`;
    while (disks.some((disk) => disk.name === name)) name = `disk-${++n}`;
    setPkg({ ...pkg, disks: [...disks, { name, sizeGi: 10 }] });
    setChosen(`disk:${name}`);
  }

  // What the plan has chosen is removed from its title.
  const removal = !canDeploy
    ? null
    : at >= 0 && pkg.services.length > 1
      ? {
          label: text("package.removeService", { name: pkg.services[at].name }),
          run: () => {
            setChosen(null);
            setPkg(withoutService(pkg, at));
          },
        }
      : storage !== null
        ? {
            label: text("package.storageRemove"),
            run: () => {
              setChosen(null);
              setPkg(withoutStorage(pkg, storage));
            },
          }
        : null;

  // Each band of the plan adds its own kind of element, in the form only.
  const adds =
    mode === "visual" && canDeploy
      ? { services: { label: text("package.addService"), onClick: addService }, storage: { label: text("package.addStorage"), onClick: addStorage } }
      : undefined;

  return (
    <>
      <PageTitle inTree title={id ? pkg.name || text("nav.packages") : text("package.new")} icon={PackageIcon} />
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
      {!id ? <p className="mb-6 max-w-[720px] text-muted">{text("package.newHint")}</p> : null}
      {guide ? <PackageGuide onClose={() => setGuide(false)} /> : null}
      {/* The plan comes first; in the YAML view it follows the text, and
          choosing an element goes back to the form to change it. */}
      {adds || pkg.services.length > 0 || (pkg.disks ?? []).length > 0 ? (
        <>
          <PlanPanel
            model={plan}
            adds={adds}
            label={text("plan.labelPackage")}
            whole={text("plan.wholePackage")}
            named={false}
            selected={selected}
            onSelect={(key) => {
              if (mode === "yaml") void switchTo("visual");
              setChosen(key);
            }}
          />
          <div className="mt-2" />
        </>
      ) : null}
      {/* Fields out of sight must not stop the form: the server says what is
          wrong, and the page shows it. */}
      <form onSubmit={save} noValidate className="flex flex-col gap-3">
        {warnings.length > 0 ? <WarningList warnings={warnings} /> : null}
        {/* One line under the plan: the chosen element, then what to do with
            the package. It stays in sight under the plan. The form is the rule; the text is a part of the menu, and
            this line leads back from it. */}
        <div
          className={underPlan}
          ref={(node) => {
            line.current = node;
            if (node && !lineReady) setLineReady(true);
          }}
        >
          <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1 border-b border-line py-1">
            <div className="min-w-0">
              {mode === "yaml" ? (
                <span className="flex items-center gap-3">
                  <TextButton type="button" onClick={() => void switchTo("visual")}>
                    <Icon icon={ArrowLeft} />
                    {text("package.visual")}
                  </TextButton>
                  <span className="text-[12px] text-muted">{text("package.advanced")}</span>
                </span>
              ) : picked ? (
                <span className="flex min-w-0 items-center gap-3">
                  <ChosenName node={picked} />
                  {removal ? <IconButton type="button" icon={Trash2} label={removal.label} onClick={removal.run} /> : null}
                </span>
              ) : null}
            </div>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
              {id && canDeploy ? (
                <Button type="button" variant="secondary" className="sm:py-1!" onClick={() => navigate(`/nouveau?package=${id}`)}>
                  {text("package.installCta")}
                </Button>
              ) : null}
              <ActionsMenu label={text("apps.more")}>
                {mode === "visual" ? <MenuItem onClick={() => void switchTo("yaml")}>{text("package.advanced")}</MenuItem> : null}
                <MenuItem onClick={() => setGuide(true)}>{text("package.guide")}</MenuItem>
                {id ? <MenuItem onClick={() => void download({ id, name: pkg.name }, "yaml").catch((err) => toast(err.message, "bad"))}>{text("package.exportYaml")}</MenuItem> : null}
              </ActionsMenu>
            </div>
          </div>
        </div>
        {mode === "yaml" ? (
          <div className="flex flex-col gap-4">
            <p className="max-w-[720px] text-muted">{text("package.yamlHint")}</p>
            <Code value={yamlText} onChange={onYaml} language="yaml" problems={lined(problems)} label={text("package.advanced")} minHeight="24rem" maxHeight="70dvh" readOnly={!canDeploy} />
            {problems.length > 0 ? <ProblemList problems={problems} /> : <p className="text-ok">{text("package.valid")}</p>}
          </div>
        ) : (
          <VisualEditor key={revision} pkg={pkg} onChange={edit} problems={problems} only={at} storage={storage} onChoose={setChosen} />
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
  only,
  storage,
  onChoose,
}: {
  pkg: Package;
  onChange: (pkg: Package) => void;
  problems: PackageProblem[];
  // only is the index of the service chosen in the plan, or -1: its settings
  // alone show. storage names the storage chosen. With neither, the package
  // itself shows. What is out of sight stays mounted, with what it holds.
  only: number;
  storage: string | null;
  onChoose: (key: string | null) => void;
}) {
  const [part, setPart] = useState<"questions" | "variables">("questions");
  function set<K extends keyof Package>(key: K, value: Package[K]) {
    onChange({ ...pkg, [key]: value });
  }
  const whole = only < 0 && storage === null;
  // The disks of one service another may link to: linking makes them shared.
  const owned = pkg.services.flatMap((service, at) =>
    (service.volumes ?? []).flatMap((volume, row) => (volume.disk || !volume.path ? [] : [{ service: at, value: `own:${at}:${row}`, label: `${service.name} · ${volume.path}` }])),
  );
  function share(into: number, row: number, value: string) {
    const [, owner, volume] = value.split(":").map(Number);
    onChange(shareDisk(pkg, owner, volume, into, row).pkg);
  }

  return (
    <div className="flex flex-col">
      <section className={`flex flex-col gap-3 ${whole ? "" : "hidden"}`}>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
          <Field label={text("package.name")}>
            <Input value={pkg.name} onChange={(e) => set("name", e.target.value)} required />
          </Field>
          <Field label={text("package.summary")}>
            <Input value={pkg.summary ?? ""} onChange={(e) => set("summary", e.target.value || undefined)} />
          </Field>
          <Field label={text("package.category")}>
            <Input value={pkg.category ?? ""} onChange={(e) => set("category", e.target.value || undefined)} />
          </Field>
        </div>
        <AboutFields about={pkg.about} onChange={(about) => set("about", about)} />
        <Tabs
          label={text("package.generalParts")}
          active={part}
          onChange={setPart}
          tabs={[
            { key: "questions", label: text("package.questionsTab") },
            { key: "variables", label: text("package.sharedEnv") },
          ]}
        />
        <div className={part === "questions" ? "" : "hidden"}>
          <Inputs pkg={pkg} onChange={onChange} />
        </div>
        <div className={part === "variables" ? "" : "hidden"}>
          <p className="mb-3 text-faint">{text("package.sharedEnvHint")}</p>
          <VariableRows value={pkg.env} onChange={(env) => set("env", env)} inputs={pkg.inputs ?? []} services={pkg.services} secrets />
        </div>
        {/* The services are out of sight here: what is wrong with them is not. */}
        {problems.length > 0 ? <ProblemList problems={problems} /> : null}
      </section>
      {storage !== null ? <StoragePage pkg={pkg} name={storage} onChange={onChange} onChoose={onChoose} problems={problems} /> : null}
      <div className={only >= 0 ? "" : "hidden"}>
        <ServicesEditor services={pkg.services} onChange={(services) => set("services", services)} problems={problems} disks={(pkg.disks ?? []).map((disk) => disk.name).filter(Boolean)} only={only} addable={false} owned={owned} onShare={share} inputs={pkg.inputs ?? []} />
      </div>
    </div>
  );
}

// RowLegend names the columns of a list of rows, above them.
function RowLegend({ columns, labels }: { columns: string; labels: string[] }) {
  return (
    <div aria-hidden className={`hidden gap-3 text-[13px] text-muted md:grid ${columns}`}>
      {labels.map((label) => (
        <span key={label}>{label}</span>
      ))}
    </div>
  );
}

const withoutCopies = (service: PackageService) => {
  const next = { ...service };
  delete next.replicas;
  return next;
};

// shareDisk turns the disk of one service into a shared one, and mounts it on
// another service, at row of its volumes. A shared disk needs a name, and the
// services that mount it run one copy.
function shareDisk(pkg: Package, owner: number, volume: number, into: number, row: number) {
  const source = pkg.services[owner].volumes![volume];
  const base = `${pkg.services[owner].name}-data`.toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^[^a-z]+/, "").slice(0, 28) || "data";
  const taken = new Set((pkg.disks ?? []).map((disk) => disk.name));
  let name = base;
  for (let n = 2; taken.has(name); n++) name = `${base}-${n}`;
  const services = pkg.services.map((service, at) => {
    if (at !== owner && at !== into) return service;
    const volumes = [...(service.volumes ?? [])];
    if (at === owner) volumes[volume] = { path: source.path, disk: name };
    if (at === into) volumes[row] = { path: volumes[row]?.path ?? "", disk: name };
    return { ...withoutCopies(service), volumes };
  });
  return { pkg: { ...pkg, services, disks: [...(pkg.disks ?? []), { name, sizeGi: source.sizeGi ?? 1 }] }, name };
}

// withoutService removes a service, and the waits on it.
function withoutService(pkg: Package, index: number) {
  const gone = pkg.services[index].name;
  return { ...pkg, services: pkg.services.filter((_, at) => at !== index).map((service) => ({ ...service, dependsOn: service.dependsOn?.filter((name) => name !== gone) })) };
}

// withoutStorage removes a storage from the package and the services.
function withoutStorage(pkg: Package, name: string) {
  const shared = (pkg.disks ?? []).some((disk) => disk.name === name);
  const services = pkg.services.map((service) => {
    const volumes = (service.volumes ?? []).filter((volume) => (shared ? volume.disk !== name : volume.disk || ownKey(service, volume.path) !== name));
    return { ...service, volumes: volumes.length > 0 ? volumes : undefined };
  });
  const rest = (pkg.disks ?? []).filter((disk) => disk.name !== name);
  return { ...pkg, services, disks: rest.length > 0 ? rest : undefined };
}

const ownKey = (service: PackageService, path: string) => `${service.name}${path.replace(/\//g, "-")}`;

// StoragePage sets one storage: its name and size, its type, and the services
// that use it, each at its own path.
function StoragePage({
  pkg,
  name,
  onChange,
  onChoose,
  problems,
}: {
  pkg: Package;
  name: string;
  onChange: (pkg: Package) => void;
  onChoose: (key: string | null) => void;
  problems: PackageProblem[];
}) {
  const disks = pkg.disks ?? [];
  const diskAt = disks.findIndex((disk) => disk.name === name);
  const disk = disks[diskAt];
  const owner = disk ? -1 : pkg.services.findIndex((service) => (service.volumes ?? []).some((volume) => !volume.disk && ownKey(service, volume.path) === name));
  const row = owner < 0 ? -1 : pkg.services[owner].volumes!.findIndex((volume) => !volume.disk && ownKey(pkg.services[owner], volume.path) === name);
  const own = owner >= 0 ? pkg.services[owner].volumes![row] : undefined;
  const mountOf = (service: PackageService, at: number) => (disk ? (service.volumes ?? []).find((volume) => volume.disk === name) : at === owner ? own : undefined);
  const mine = problems.filter((problem) => problem.path.startsWith(`disks[${diskAt}]`) && diskAt >= 0);

  function setDisk(part: Partial<PackageDisk>) {
    onChange({ ...pkg, disks: disks.map((item, at) => (at === diskAt ? { ...item, ...part } : item)) });
  }
  function setOwnSize(sizeGi?: number) {
    onChange({ ...pkg, services: pkg.services.map((service, at) => (at === owner ? { ...service, volumes: service.volumes!.map((volume, index) => (index === row ? { ...volume, sizeGi } : volume)) } : service)) });
  }
  function setPath(at: number, path: string) {
    const service = pkg.services[at];
    const volumes = (service.volumes ?? []).map((volume) => (volume === mountOf(service, at) ? { ...volume, path } : volume));
    onChange({ ...pkg, services: pkg.services.map((item, index) => (index === at ? { ...item, volumes } : item)) });
    if (!disk) onChoose(`disk:${ownKey(service, path)}`);
  }
  function toggle(at: number, on: boolean) {
    if (!disk && on) {
      const shared = shareDisk(pkg, owner, row, at, (pkg.services[at].volumes ?? []).length);
      onChange(shared.pkg);
      onChoose(`disk:${shared.name}`);
      return;
    }
    const services = pkg.services.map((service, index) => {
      if (index !== at) return service;
      if (on) return { ...withoutCopies(service), volumes: [...(service.volumes ?? []), { path: "", disk: name }] };
      const volumes = (service.volumes ?? []).filter((volume) => volume.disk !== name);
      return { ...service, volumes: volumes.length > 0 ? volumes : undefined };
    });
    onChange({ ...pkg, services });
  }
  return (
    <section aria-label={text("plan.band.storage")} className="flex flex-col gap-4">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-[minmax(0,1fr)_180px]">
        {disk ? (
          <Field label={text("package.diskName")}>
            <Input value={disk.name} placeholder="media" className="font-mono" autoCapitalize="off" spellCheck={false} onChange={(e) => setDisk({ name: e.target.value })} />
          </Field>
        ) : (
          <span />
        )}
        <Field label={text("package.diskSize")}>
          <Input
            inputMode="numeric"
            value={(disk ? disk.sizeGi : own?.sizeGi) || ""}
            placeholder="10"
            onChange={(e) => (disk ? setDisk({ sizeGi: numberOrUndefined(e.target.value) ?? 0 }) : setOwnSize(numberOrUndefined(e.target.value)))}
          />
        </Field>
      </div>
      <Group label={text("package.storageType")}>
        <div className="flex flex-col gap-2">
          <label className="flex items-start gap-3">
            <input type="radio" name="storage-type" checked readOnly className="mt-1" />
            <span>
              <span className="block">{text("package.storageSimple")}</span>
              <span className="block text-[13px] text-faint">{text("package.storageSimpleHint")}</span>
            </span>
          </label>
          <label className="flex items-start gap-3 text-muted">
            <input type="radio" name="storage-type" disabled className="mt-1" />
            <span>
              <span className="block">{text("package.storageRedundant")}</span>
              <span className="block text-[13px] text-faint">{text(disk ? "package.storageRedundantShared" : "package.storageRedundantOwn")}</span>
            </span>
          </label>
        </div>
      </Group>
      <div role="group" aria-label={text("package.storageUsers")} className="flex flex-col gap-2">
        <RowLegend columns={storageUserColumns} labels={[text("package.storageUsers"), text("package.diskPath")]} />
        {pkg.services.map((service, at) => {
          const mount = mountOf(service, at);
          return (
            <div key={at} className={`grid grid-cols-1 items-center gap-3 ${storageUserColumns}`}>
              <label className="flex min-w-0 items-center gap-3">
                <input type="checkbox" checked={Boolean(mount)} disabled={at === owner} onChange={(e) => toggle(at, e.target.checked)} />
                <span className="truncate font-mono">{service.name || "…"}</span>
              </label>
              {mount ? <Input value={mount.path} placeholder="/data" aria-label={text("package.diskPath")} className="font-mono" onChange={(e) => setPath(at, e.target.value)} /> : <span className="hidden md:block" />}
            </div>
          );
        })}
      </div>
      {mine.length > 0 ? <ProblemList problems={mine} /> : null}
    </section>
  );
}

const storageUserColumns = "md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]";

// A common variable is told by its kind, not written: the form below reads
// each value into a kind and writes it back. A value made of several parts
// stays a text, whole.
type VariableKind = "text" | "bool" | "secret" | "input" | "url" | "host";
type Variable = { id: number; key: string; kind: VariableKind; value: string };

function readVariable(raw: string): Pick<Variable, "kind" | "value"> {
  if (raw === "true" || raw === "false") return { kind: "bool", value: raw };
  const token = /^\{\{(secret|input|url|host)(?::([^{}]*))?\}\}$/.exec(raw);
  if (!token) return { kind: "text", value: raw };
  return { kind: token[1] as VariableKind, value: token[2] ?? "" };
}

function writeVariable(item: Variable) {
  if (item.kind === "text") return item.value;
  if (item.kind === "bool") return item.value === "true" ? "true" : "false";
  return item.value ? `{{${item.kind}:${item.value}}}` : item.kind === "url" || item.kind === "host" ? `{{${item.kind}}}` : "";
}

const variableColumns = "md:grid-cols-[minmax(0,1fr)_minmax(0,220px)_minmax(0,1fr)_24px]";

// VariableRows edits variables, one row each: a name, a kind, and the control
// that kind needs. A blank row follows the last, on screen only.
function VariableRows({
  value,
  onChange,
  inputs,
  services,
  secrets = false,
}: {
  value?: Record<string, string>;
  onChange: (value: Record<string, string> | undefined) => void;
  inputs: { key: string; label: string }[];
  services: PackageService[];
  // secrets offers generated passwords: they belong to the common variables,
  // which are sealed.
  secrets?: boolean;
}) {
  const next = useRef(0);
  const blank = (item: Variable) => !item.key && item.kind === "text" && !item.value;
  // One blank row at the end, whatever was just emptied or filled: the row
  // being typed in keeps its place, and its focus.
  const padded = (list: Variable[]) => {
    const out = [...list];
    while (out.length > 1 && blank(out[out.length - 1]) && blank(out[out.length - 2])) out.pop();
    if (out.length === 0 || !blank(out[out.length - 1])) out.push({ id: next.current++, key: "", kind: "text", value: "" });
    return out;
  };
  const [items, setItems] = useState<Variable[]>(() => padded(Object.entries(value ?? {}).map(([key, raw]) => ({ id: next.current++, key, ...readVariable(raw) }))));
  const kinds: { key: VariableKind; label: MsgKey }[] = [
    { key: "text", label: "package.envKindText" },
    { key: "bool", label: "package.envKindBool" },
    ...(secrets ? [{ key: "secret" as const, label: "package.envKindSecret" as const }] : []),
    { key: "input", label: "package.envKindInput" },
    { key: "url", label: "package.envKindUrl" },
    { key: "host", label: "package.envKindHost" },
  ];
  // The first public service is the main one: it is the address without a name.
  const others = services.filter((service) => service.public).slice(1);

  function commit(list: Variable[]) {
    setItems(padded(list));
    const map: Record<string, string> = {};
    for (const item of list) if (item.key.trim()) map[item.key.trim()] = writeVariable(item);
    onChange(Object.keys(map).length ? map : undefined);
  }
  function change(id: number, part: Partial<Variable>) {
    commit(items.map((item) => (item.id === id ? { ...item, ...part } : item)));
  }
  function setKind(item: Variable, kind: VariableKind) {
    const value = kind === "bool" ? "false" : kind === "secret" ? item.key.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "password" : kind === "input" ? (inputs[0]?.key ?? "") : "";
    change(item.id, { kind, value });
  }

  return (
    <div className="flex flex-col gap-2">
      <RowLegend columns={variableColumns} labels={[text("package.envName"), text("package.envKind"), text("package.envValue")]} />
      {items.map((item) => (
        <div key={item.id} className={`grid grid-cols-1 items-center gap-3 ${variableColumns}`}>
          <Input value={item.key} placeholder="ADMIN_EMAIL" aria-label={text("package.envName")} className="font-mono" autoCapitalize="off" spellCheck={false} onChange={(e) => change(item.id, { key: e.target.value })} />
          <Select value={item.kind} aria-label={text("package.envKind")} onChange={(e) => setKind(item, e.target.value as VariableKind)}>
            {kinds.map((kind) => (
              <option key={kind.key} value={kind.key}>
                {text(kind.label)}
              </option>
            ))}
          </Select>
          {item.kind === "bool" ? (
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={item.value === "true"} onChange={(e) => change(item.id, { value: e.target.checked ? "true" : "false" })} />
              <span>{text("package.envOn")}</span>
            </label>
          ) : item.kind === "input" ? (
            <Select value={item.value} aria-label={text("package.envValue")} disabled={inputs.length === 0} onChange={(e) => change(item.id, { value: e.target.value })}>
              {inputs.length === 0 ? <option value="">{text("package.envNoInput")}</option> : null}
              {inputs.some((input) => input.key === item.value) || inputs.length === 0 ? null : <option value={item.value}>{item.value}</option>}
              {inputs.map((input) => (
                <option key={input.key} value={input.key}>
                  {input.label || input.key}
                </option>
              ))}
            </Select>
          ) : item.kind === "url" || item.kind === "host" ? (
            <Select value={item.value} aria-label={text("package.envValue")} onChange={(e) => change(item.id, { value: e.target.value })}>
              <option value="">{text("package.envMainService")}</option>
              {others.some((service) => service.name === item.value) || !item.value ? null : <option value={item.value}>{item.value}</option>}
              {others.map((service) => (
                <option key={service.name} value={service.name}>
                  {service.name}
                </option>
              ))}
            </Select>
          ) : (
            <Input
              value={item.value}
              aria-label={text(item.kind === "secret" ? "package.envSecretName" : "package.envValue")}
              autoCapitalize="off"
              spellCheck={false}
              onChange={(e) => change(item.id, { value: e.target.value })}
            />
          )}
          {blank(item) ? <span className="hidden md:block" /> : <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => commit(items.filter((other) => other.id !== item.id))} />}
        </div>
      ))}
    </div>
  );
}

// ServicesEditor edits the services of a package or of a running
// application: one card each, who waits for whom above them.
// freeServiceName names a new service after those there are: service-N.
function freeServiceName(services: PackageService[]) {
  let n = services.length + 1;
  while (services.some((service) => service.name === `service-${n}`)) n++;
  return `service-${n}`;
}

function ServicesEditor({
  services,
  onChange,
  problems,
  disks,
  only,
  addable = true,
  owned = [],
  onShare,
  inputs = [],
}: {
  // only is the index of the one service to show.
  only?: number;
  // owned are the disks of one service the others may link to; onShare links
  // one to the service at index, at row of its volumes.
  owned?: { service: number; value: string; label: string }[];
  onShare?: (index: number, row: number, value: string) => void;
  inputs?: { key: string; label: string }[];
  // addable puts the button that adds a service at the head of the list;
  // a page that has it on its line of tabs leaves it out.
  addable?: boolean;
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
    onChange([...services, { name: freeServiceName(services), image: "" }]);
  }

  function removeService(index: number) {
    const gone = services[index].name;
    onChange(services.filter((_, at) => at !== index).map((service) => ({ ...service, dependsOn: service.dependsOn?.filter((name) => name !== gone) })));
  }

  return (
    <section className="flex flex-col gap-4">
      {/* A chosen service is named on the line above: no title for one card. */}
      <div className={`flex flex-wrap items-center justify-between gap-4 ${only !== undefined ? "hidden" : ""}`}>
        <h2 className="text-base">{text("field.services")}</h2>
        {addable ? (
          <TextButton type="button" onClick={addService}>
            {text("package.addService")}
          </TextButton>
        ) : null}
      </div>
      <div className={only !== undefined ? "hidden" : ""}>
        <Dependencies services={services} />
      </div>
      {services.map((service, index) => (
        <div key={index} className={only !== undefined && index !== only ? "hidden" : ""}>
        <ServiceCard
          service={service}
          others={services.filter((_, at) => at !== index).map((other) => other.name)}
          links={[...shared.map((name) => ({ value: name, label: name })), ...owned.filter((item) => item.service !== index)]}
          onShare={(row, value) => onShare?.(index, row, value)}
          inputs={inputs}
          services={services}
          problems={problemsUnder(problems, `services[${index}]`).map((problem) => ({ ...problem, path: problem.path.slice(`services[${index}]`.length + 1) }))}
          onChange={(next) => setService(index, next)}
          onRemove={services.length > 1 ? () => removeService(index) : undefined}
          named={only === undefined}
        />
        </div>
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
const tabTip: Partial<Record<ServiceTab, MsgKey>> = {
  general: "package.tipGeneral",
  health: "package.tipHealth",
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
  links,
  onShare,
  inputs,
  services,
  problems,
  onChange,
  onRemove,
  named,
}: {
  service: PackageService;
  others: string[];
  links: { value: string; label: string }[];
  onShare: (row: number, value: string) => void;
  inputs: { key: string; label: string }[];
  services: PackageService[];
  problems: PackageProblem[];
  onChange: (service: PackageService) => void;
  onRemove?: () => void;
  // named makes a card headed by the service, one in a list. Without, the
  // service is alone on the page: the line above says its name and removes
  // it, and the tabs stay in sight. A phone keeps the name on the page.
  named: boolean;
}) {
  const [tab, setTab] = useState<ServiceTab>("general");
  const roomy = useWide("(min-width: 1024px)");
  const headless = !named && roomy;
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
  const tip = tabTip[tab];
  const published = service.public ? <span className="rounded border border-line px-1.5 text-[13px] text-muted">{text("common.published")}</span> : null;
  const remove = onRemove ? <IconButton type="button" icon={Trash2} label={text("package.removeService", { name: service.name })} onClick={onRemove} /> : null;

  return (
    <article className={named ? "flex flex-col gap-3 rounded-md border border-line bg-surface p-3" : "flex flex-col gap-3"}>
      {headless ? null : (
        <header className="flex items-center justify-between gap-4">
          <h3 className="flex min-w-0 items-center gap-2">
            <span className="truncate font-mono">{service.name || "…"}</span>
            {published}
          </h3>
          {named ? remove : null}
        </header>
      )}
      <div className={named ? "" : "md:sticky md:top-[calc(var(--plan-bottom,0px)+var(--line-height,0px))] md:z-[8] md:bg-canvas"}>
        <Tabs label={service.name} active={tab} onChange={setTab} small={!named} tabs={tabs.map((item) => ({ ...item, mark: marked.has(item.key) }))} aside={headless && published ? published : undefined} />
      </div>
      {tip ? <p className="text-[13px] text-faint">{text(tip)}</p> : null}
      {tab === "general" ? <GeneralTab service={service} others={others} set={set} onChange={onChange} /> : null}
      {tab === "network" ? <NetworkTab service={service} set={set} /> : null}
      {tab === "storage" ? <StorageTab service={service} set={set} links={links} onShare={onShare} /> : null}
      {tab === "files" ? <FilesTab service={service} set={set} /> : null}
      {tab === "env" ? <VariableRows value={service.env} onChange={(env) => set("env", env)} inputs={inputs} services={services} /> : null}
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

// A row of the ports, its legend above: the same columns for both.
const portColumns = "md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_124px_132px_minmax(0,1fr)_24px]";

// A row can open a port in both protocols. The package has no such word: it
// holds two ports of one number, the UDP one unnamed, since a name is unique.
type PortRow = Omit<PackagePort, "protocol"> & { protocol?: "tcp" | "udp" | "both" };

function portRows(ports: PackagePort[]) {
  const rows: PortRow[] = [];
  for (let at = 0; at < ports.length; at++) {
    const port = ports[at];
    const twin = ports[at + 1];
    if (twin && port.port === twin.port && port.expose === twin.expose && (port.protocol ?? "tcp") === "tcp" && twin.protocol === "udp" && !twin.name) {
      rows.push({ ...port, protocol: "both" });
      at++;
    } else {
      rows.push(port);
    }
  }
  return rows;
}

function portsOf(rows: PortRow[]) {
  return rows.flatMap((row): PackagePort[] => {
    const { protocol, ...port } = row;
    if (protocol !== "both") return [{ ...port, ...(protocol ? { protocol } : {}) }];
    const twin: PackagePort = { port: port.port, protocol: "udp" };
    if (port.expose !== undefined) twin.expose = port.expose;
    return [port, twin];
  });
}

// NetworkTab lists the ports with one blank row after them, on screen only:
// it joins the package once something is typed in it, and a new one follows
// as soon as the last has its port.
function NetworkTab({ service, set }: { service: PackageService; set: Setter }) {
  const ports = portRows(service.ports ?? []);
  const last = ports[ports.length - 1];
  const rows: PortRow[] = !last || last.port ? [...ports, { port: 0 }] : ports;
  const empty = (port: PortRow) => !port.name && !port.port && !port.protocol && port.expose === undefined;

  function change(index: number, part: Partial<PortRow>) {
    const next = rows.map((port, at) => (at === index ? { ...port, ...part } : port));
    while (next.length > 0 && empty(next[next.length - 1])) next.pop();
    set("ports", portsOf(next));
  }

  return (
    <div role="group" aria-label={text("package.ports")} className="flex flex-col gap-2">
      <RowLegend columns={portColumns} labels={[text("package.portName"), text("package.port"), text("package.protocol"), text("package.expose"), text("package.exposePort")]} />
      {rows.map((port, index) => (
        <div key={index} className={`grid grid-cols-1 items-center gap-3 ${portColumns}`}>
          <Input value={port.name ?? ""} placeholder="metrics" aria-label={text("package.portName")} onChange={(e) => change(index, { name: e.target.value || undefined })} />
          <Input inputMode="numeric" value={port.port || ""} placeholder="9100" aria-label={text("package.port")} onChange={(e) => change(index, { port: numberOrUndefined(e.target.value) ?? 0 })} />
          <Select value={port.protocol ?? "tcp"} aria-label={text("package.protocol")} onChange={(e) => change(index, { protocol: e.target.value === "tcp" ? undefined : (e.target.value as PortRow["protocol"]) })}>
            <option value="tcp">TCP</option>
            <option value="udp">UDP</option>
            <option value="both">TCP + UDP</option>
          </Select>
          <label className="flex items-center gap-2 text-[13px]">
            <input type="checkbox" aria-label={text("package.expose")} checked={port.expose !== undefined} onChange={(e) => change(index, { expose: e.target.checked ? port.port : undefined })} />
            <span className="md:hidden">{text("package.expose")}</span>
          </label>
          {port.expose !== undefined ? (
            <Input inputMode="numeric" value={port.expose || ""} placeholder={String(port.port || 51820)} aria-label={text("package.exposePort")} onChange={(e) => change(index, { expose: numberOrUndefined(e.target.value) ?? 0 })} />
          ) : (
            <span className="hidden md:block" />
          )}
          {index < ports.length ? <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => set("ports", portsOf(ports.filter((_, at) => at !== index)))} /> : <span className="hidden md:block" />}
        </div>
      ))}
    </div>
  );
}

const volumeColumns = "md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_120px_24px]";

// StorageTab lists what the service mounts, in the rows of NetworkTab: on each
// a new storage or an existing one, and the path the service uses.
function StorageTab({ service, set, links, onShare }: { service: PackageService; set: Setter; links: { value: string; label: string }[]; onShare: (row: number, value: string) => void }) {
  const volumes = service.volumes ?? [];
  const last = volumes[volumes.length - 1];
  const rows: PackageVolume[] = !last || last.path ? [...volumes, { path: "" }] : volumes;
  const empty = (volume: PackageVolume) => !volume.path && !volume.disk && !volume.sizeGi;

  function commit(next: PackageVolume[]) {
    const list = [...next];
    while (list.length > 0 && empty(list[list.length - 1])) list.pop();
    set("volumes", list);
  }
  function change(index: number, part: Partial<PackageVolume>) {
    commit(rows.map((volume, at) => (at === index ? { ...volume, ...part } : volume)));
  }
  function choose(index: number, value: string) {
    if (value.startsWith("own:")) return onShare(index, value);
    const path = rows[index].path;
    commit(rows.map((volume, at) => (at === index ? (value ? { path, disk: value } : { path, sizeGi: 1 }) : volume)));
  }

  return (
    <div role="group" aria-label={text("package.volumes")} className="flex flex-col gap-2">
      <RowLegend columns={volumeColumns} labels={[text("plan.band.storage"), text("package.diskPath"), text("package.diskSize")]} />
      {rows.map((volume, index) => (
        <div key={index} className={`grid grid-cols-1 items-center gap-3 ${volumeColumns}`}>
          <Select value={volume.disk ?? ""} aria-label={text("plan.band.storage")} onChange={(e) => choose(index, e.target.value)}>
            <option value="">{text("package.storageNew")}</option>
            {volume.disk && !links.some((link) => link.value === volume.disk) ? <option value={volume.disk}>{volume.disk}</option> : null}
            {links.map((link) => (
              <option key={link.value} value={link.value}>
                {link.label}
              </option>
            ))}
          </Select>
          <Input value={volume.path} placeholder="/data" aria-label={text("package.diskPath")} className="font-mono" onChange={(e) => change(index, { path: e.target.value })} />
          {volume.disk ? (
            <span className="hidden md:block" />
          ) : (
            <Input inputMode="numeric" value={volume.sizeGi ?? ""} placeholder="1" aria-label={text("package.diskSize")} onChange={(e) => change(index, { sizeGi: numberOrUndefined(e.target.value) })} />
          )}
          {index < volumes.length ? <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => set("volumes", volumes.filter((_, at) => at !== index))} /> : <span className="hidden md:block" />}
        </div>
      ))}
    </div>
  );
}

const fileColumns = "md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_24px]";

// FilesTab lists the files, in the rows of NetworkTab: a path and its content.
function FilesTab({ service, set }: { service: PackageService; set: Setter }) {
  const files = service.files ?? [];
  const last = files[files.length - 1];
  const rows: PackageFile[] = !last || last.path ? [...files, { path: "", content: "" }] : files;

  function change(index: number, part: Partial<PackageFile>) {
    const next = rows.map((file, at) => (at === index ? { ...file, ...part } : file));
    while (next.length > 0 && !next[next.length - 1].path && !next[next.length - 1].content) next.pop();
    set("files", next);
  }

  return (
    <div role="group" aria-label={text("package.files")} className="flex flex-col gap-2">
      <RowLegend columns={fileColumns} labels={[text("package.filePath"), text("package.fileContent")]} />
      {rows.map((file, index) => (
        <div key={index} className={`grid grid-cols-1 items-start gap-3 ${fileColumns}`}>
          <Input value={file.path} placeholder="/etc/app/config.ini" aria-label={text("package.filePath")} className="font-mono" onChange={(e) => change(index, { path: e.target.value })} />
          <Code value={file.content} onChange={(content) => change(index, { content })} language={languageOf(file.path)} label={file.path || text("package.fileContent")} minHeight="2.5rem" />
          {index < files.length ? <IconButton type="button" icon={Trash2} label={text("common.delete")} onClick={() => set("files", files.filter((_, at) => at !== index))} /> : <span className="hidden md:block" />}
        </div>
      ))}
    </div>
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

// The stops of the sliders: fine at the start, where a small service sits.
const cpuStops = ["100m", "250m", "500m", "750m", "1", "2", "3", "4", "6", "8", "12", "16"];
const memoryStops = ["64Mi", "128Mi", "256Mi", "512Mi", "768Mi", "1Gi", "2Gi", "3Gi", "4Gi", "6Gi", "8Gi", "12Gi", "16Gi", "32Gi"];

const coresOf = (value: string) => (value.endsWith("m") ? parseFloat(value) / 1000 : parseFloat(value));

function mebibytesOf(value: string) {
  const match = /^([\d.]+)(Ki|Mi|Gi|Ti|K|M|G|T)?$/.exec(value);
  if (!match) return NaN;
  const unit: Record<string, number> = { Ki: 1 / 1024, Mi: 1, Gi: 1024, Ti: 1048576, K: 1000 / 1048576, M: 1e6 / 1048576, G: 1e9 / 1048576, T: 1e12 / 1048576 };
  return parseFloat(match[1]) * (match[2] ? unit[match[2]] : 1 / 1048576);
}

function coresLabel(value: string) {
  const cores = coresOf(value);
  if (Number.isNaN(cores)) return value;
  return text(cores > 1 ? "package.cpuMany" : "package.cpuOne", { count: cores.toLocaleString(currentLocale()) });
}

function memoryLabel(value: string) {
  const size = mebibytesOf(value);
  if (Number.isNaN(size)) return value;
  const count = (amount: number) => Math.round(amount * 100) / 100;
  return size >= 1024 ? text("package.memoryGi", { count: count(size / 1024).toLocaleString(currentLocale()) }) : text("package.memoryMi", { count: count(size).toLocaleString(currentLocale()) });
}

// ResourceSlider moves along stops, the first being no reservation. A value
// between two stops gets one of its own, so that showing it never changes it.
function ResourceSlider({
  label,
  hint,
  value,
  stops,
  amount,
  words,
  onChange,
}: {
  label: string;
  hint?: string;
  value: string;
  stops: string[];
  amount: (value: string) => number;
  words: (value: string) => string;
  onChange: (value: string) => void;
}) {
  const known = stops.map((stop) => (value && amount(stop) === amount(value) ? value : stop));
  const scale = value && !known.includes(value) ? [...known, value].sort((a, b) => (amount(a) || Infinity) - (amount(b) || Infinity)) : known;
  const all = ["", ...scale];
  const shown = (item: string) => (item ? words(item) : text("package.resourceAuto"));
  return (
    <label className="block">
      <span className="mb-2 flex items-baseline justify-between gap-3">
        <span className="text-muted">{label}</span>
        <span>{shown(value)}</span>
      </span>
      <input
        type="range"
        min={0}
        max={all.length - 1}
        step={1}
        value={all.indexOf(value)}
        aria-valuetext={shown(value)}
        onChange={(e) => onChange(all[Number(e.target.value)])}
        className="w-full accent-ink"
      />
      <span className="mt-1 flex justify-between text-[12px] text-faint">
        <span>{shown("")}</span>
        <span>{shown(all[all.length - 1])}</span>
      </span>
      {hint ? <span className="mt-2 block text-faint">{hint}</span> : null}
    </label>
  );
}

function ResourcesTab({ service, set }: { service: PackageService; set: Setter }) {
  function setResource(key: "cpu" | "memory", value: string) {
    const next = { ...service.resources, [key]: value || undefined };
    set("resources", next.cpu || next.memory || next.gpu ? next : undefined);
  }
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <ResourceSlider label={text("package.cpu")} value={service.resources?.cpu ?? ""} stops={cpuStops} amount={coresOf} words={coresLabel} onChange={(value) => setResource("cpu", value)} />
        <ResourceSlider label={text("package.memory")} hint={text("package.memoryHint")} value={service.resources?.memory ?? ""} stops={memoryStops} amount={mebibytesOf} words={memoryLabel} onChange={(value) => setResource("memory", value)} />
      </div>
      <p className="text-[13px] text-faint">{text("package.resourceAutoHint")}</p>
    </div>
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
      <PageTitle inTree title={text("appServices.title", { name })} icon={PackageIcon} />
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
        {/* As in a package: the two views, then what to add and to do, on one line. */}
        <Tabs
          label={text("package.view")}
          active={mode}
          onChange={(next) => void switchTo(next)}
          tabs={[
            { key: "visual", label: text("package.visual") },
            { key: "yaml", label: text("package.advanced"), mark: !readable },
          ]}
          aside={
            <>
              {mode === "visual" && canDeploy ? (
                <>
                  <TextButton type="button" onClick={() => setServices([...services, { name: freeServiceName(services), image: "" }])}>
                    <Icon icon={Plus} />
                    {text("package.addService")}
                  </TextButton>
                  <span aria-hidden className="hidden h-5 w-px bg-line sm:block" />
                </>
              ) : null}
              <TextButton type="button" onClick={() => setGuide(true)}>
                <Icon icon={BookOpen} />
                {text("package.guide")}
              </TextButton>
              {canDeploy ? (
                <TextButton type="button" onClick={() => void keep()}>
                  {text("appServices.keep")}
                </TextButton>
              ) : null}
            </>
          }
        />
        {mode === "yaml" ? (
          <div className="flex flex-col gap-4">
            <Code value={yamlText} onChange={onYaml} language="yaml" problems={lined(problems)} label={text("package.advanced")} minHeight="24rem" maxHeight="70dvh" readOnly={!canDeploy} />
            {problems.length > 0 ? <ProblemList problems={problems} /> : null}
          </div>
        ) : (
          <ServicesEditor services={services} onChange={setServices} problems={problems} addable={false} />
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
  { title: "guide.sharedTitle", body: "guide.sharedBody" },
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
  { field: "disks[].name · sizeGi", body: "guide.refDisks" },
  { field: "volumes[].path · sizeGi · disk", body: "guide.refVolumes" },
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
