import { Link, NavLink, Outlet, useLocation } from "react-router-dom";
import { langNames, langs, text, type MsgKey } from "./i18n";
import { useI18n } from "./lang";
import { Suspense, createContext, useContext, useEffect, useRef, useState, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";
import {
  Boxes,
  ChevronDown,
  ChevronLeft,
  ChevronUp,
  History,
  Globe,
  LayoutGrid,
  LayoutTemplate,
  Package as PackageIcon,
  List,
  Moon,
  MoreHorizontal,
  Network,
  Plus,
  Sun,
  type LucideIcon,
  ArrowUpCircle,
  Settings,
} from "lucide-react";
import { api, type Deployment, type Destruction } from "./api";
import { Palette, SearchButton } from "./palette";
import { BottomTabs } from "./mobilenav";
import { ToastHost } from "./kit";
import { UpdatesHost, VersionNote, urgent, useUpdates } from "./updates";
import { ActivityHost, BusyIndicator, ReplicaBanner, TransferBanner, useActivity } from "./activity";
import { settingsMatch } from "./navmap";
import { useTheme } from "./theme";
import { AccountNav } from "./account-nav";
import { useSession } from "./session";

export const focus =
  "focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

export function Icon({ icon: Glyph, className }: { icon: LucideIcon; className?: string }) {
  return <Glyph size={16} strokeWidth={1.5} className={className} aria-hidden />;
}

const buttonTones = {
  primary: "bg-ink text-canvas",
  secondary: "border border-line bg-surface text-ink",
  danger: "bg-bad text-canvas",
};

// Buttons reach 44 px on a touch screen, the size a finger needs.
export function Button({ children, variant = "primary", className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: keyof typeof buttonTones }) {
  return (
    <button {...props} className={`inline-flex min-h-11 items-center justify-center gap-2 rounded-md px-4 py-2 disabled:opacity-40 sm:min-h-0 ${buttonTones[variant]} ${focus} ${className}`}>
      {children}
    </button>
  );
}

export function TextButton({ children, tone, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { tone?: "danger" }) {
  return (
    <button
      {...props}
      className={`-mx-2 inline-flex min-h-11 items-center gap-2 rounded-md px-2 py-0.5 disabled:opacity-40 sm:-mx-1.5 sm:min-h-0 sm:px-1.5 ${tone === "danger" ? "text-bad" : "text-ink"} ${focus}`}
    >
      {children}
    </button>
  );
}

export function IconButton({
  icon,
  label,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { icon: LucideIcon; label: string }) {
  return (
    <button {...props} aria-label={label} title={label} className={`-m-2.5 rounded-md p-2.5 text-ink sm:-m-1 sm:p-1 ${focus}`}>
      <Icon icon={icon} />
    </button>
  );
}

export function ActionsMenu({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const [alignEnd, setAlignEnd] = useState(false);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onPointer(event: MouseEvent) {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  function toggle() {
    if (!open && root.current) {
      const rect = root.current.getBoundingClientRect();
      const needed = 260;
      const spaceRight = window.innerWidth - rect.left;
      const spaceLeft = rect.right;
      // A menu at the start of the page used to open left, under the navigation.
      setAlignEnd(spaceRight < needed && spaceLeft > spaceRight);
    }
    setOpen((value) => !value);
  }

  return (
    <div className="relative" ref={root}>
      <IconButton icon={MoreHorizontal} label={label} aria-expanded={open} aria-haspopup="menu" onClick={toggle} />
      {open ? (
        <div role="menu" onClick={() => setOpen(false)} className={`absolute z-30 mt-2 w-max min-w-48 max-w-[min(20rem,calc(100vw-2rem))] rounded-md border border-line bg-surface p-1 ${alignEnd ? "right-0" : "left-0"}`}>
          {children}
        </div>
      ) : null}
    </div>
  );
}

export function MenuItem({ children, tone, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { tone?: "danger" }) {
  return (
    <button type="button" role="menuitem" {...props} className={`block min-h-11 w-full rounded-md px-3 py-2 text-left sm:min-h-0 ${tone === "danger" ? "text-bad" : ""} ${focus}`}>
      {children}
    </button>
  );
}

const DestructionJobs = createContext<Destruction[]>([]);
const OpenDeployments = createContext<Deployment[]>([]);

export function useDeployments() {
  return useContext(OpenDeployments);
}

export function useDestructions() {
  return useContext(DestructionJobs);
}

export function removalOf(jobs: Destruction[], kind: string, id: string) {
  return jobs.find((job) => job.kind === kind && job.targetId === id && job.status === "running");
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-2 block text-muted">{label}</span>
      {children}
      {hint ? <span className="mt-2 block text-faint">{hint}</span> : null}
    </label>
  );
}

// Group names several controls together, where one label cannot: a list of
// domains, a set of checkboxes, a row with a button.
export function Group({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <fieldset className="min-w-0">
      <legend className="mb-2 text-muted">{label}</legend>
      {hint ? <p className="mb-3 text-faint">{hint}</p> : null}
      {children}
    </fieldset>
  );
}

const control = "w-full rounded-md border border-line bg-surface px-3 py-2 outline-none focus:border-focus";

export function Input({ className = "", ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`${control} ${className}`} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} className={control} />;
}

export function TextArea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} className={`${control} min-h-24`} />;
}

export function ErrorText({ children }: { children: ReactNode }) {
  return (
    <p className="text-bad" role="alert">
      {children}
    </p>
  );
}

export function Skeleton({ className = "h-4 w-32" }: { className?: string }) {
  return <span className={`block max-w-full animate-pulse rounded bg-line ${className}`} aria-hidden />;
}

export function LoadingLines({ lines = 3, label }: { lines?: number; label?: string }) {
  const caption = label ?? text("list.loading");
  const widths = ["w-48", "w-64", "w-40", "w-56", "w-36"];
  return (
    <div className="flex flex-col gap-3" role="status" aria-label={caption}>
      {Array.from({ length: lines }, (_, index) => (
        <Skeleton key={index} className={`h-4 ${widths[index % widths.length]}`} />
      ))}
    </div>
  );
}

export function LoadingCards({ count = 4 }: { count?: number }) {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2" role="status" aria-label={text("list.loading")}>
      {Array.from({ length: count }, (_, index) => (
        <div key={index} className="flex flex-col gap-3 rounded-md border border-line p-4">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-4 w-44" />
        </div>
      ))}
    </div>
  );
}

export function SectionTitle({ children }: { children: ReactNode }) {
  return <h2 className="text-base">{children}</h2>;
}

export function Fold({ title, children, defaultOpen = false }: { title: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  // A section that holds a problem opens by itself once the problem shows.
  useEffect(() => {
    if (defaultOpen) setOpen(true);
  }, [defaultOpen]);
  return (
    <section>
      <h2 className="text-base">
        <button type="button" className={`-mx-1.5 inline-flex items-center gap-2 rounded-md px-1.5 py-0.5 ${focus}`} aria-expanded={open} onClick={() => setOpen((value) => !value)}>
          <Icon icon={open ? ChevronUp : ChevronDown} />
          {title}
        </button>
      </h2>
      {open ? <div className="mt-4">{children}</div> : null}
    </section>
  );
}

const failedReasons = new Set([
  "CrashLoopBackOff",
  "ImagePullBackOff",
  "ErrImagePull",
  "InvalidImageName",
  "CreateContainerConfigError",
  "CreateContainerError",
  "RunContainerError",
  "StartError",
  "OOMKilled",
  "Error",
]);

export function podFailed(pod: { phase: string; reason: string }) {
  return pod.phase === "Failed" || failedReasons.has(pod.reason);
}

export function podHealth(pod: { ready: boolean; phase: string; reason: string }) {
  if (podFailed(pod)) {
    return { text: text("status.failed"), tone: "text-bad" };
  }
  if (pod.phase === "Pending") return { text: text("status.starting"), tone: "text-muted" };
  if (pod.ready) return { text: text("status.healthy"), tone: "text-ok" };
  return { text: text("status.notReady"), tone: "text-muted" };
}

const nav: NavSpec[] = [
  { to: "/", label: "nav.overview", icon: Globe, end: true },
  { to: "/applications", label: "nav.apps", icon: LayoutGrid, match: ["/applications", "/nouveau", "/catalogue"] },
  { to: "/clusters", label: "nav.servers", icon: Network, match: ["/serveurs"] },
  { to: "/templates", label: "nav.templates", icon: LayoutTemplate, match: ["/templates"] },
  { to: "/paquets", label: "nav.packages", icon: PackageIcon, match: ["/paquets"] },
];

const activityNav: NavSpec = { to: "/historique", label: "nav.activity", icon: History, match: ["/deploiements", "/suppressions", "/destructions"] };
const updatesNav: NavSpec = { to: "/mises-a-jour", label: "nav.updates", icon: ArrowUpCircle };
// Settings gathers what is set once and seldom touched again.
const settingsNav: NavSpec = { to: "/reglages", label: "nav.settings", icon: Settings, match: settingsMatch };

type NavSpec = { to: string; label: MsgKey; icon: LucideIcon; end?: boolean; match?: string[] };

function itemClass(active: boolean) {
  return `flex items-center gap-3 rounded-md px-3 py-2 ${focus} ${active ? "bg-status text-ink" : "text-muted"}`;
}

function NavItem({ to, label, icon, end, match, count, hot }: NavSpec & { count?: number; hot?: boolean }) {
  useI18n();
  const location = useLocation();
  const covered = match?.some((prefix) => location.pathname.startsWith(prefix)) ?? false;
  const name = text(label);
  return (
    <NavLink to={to} end={end} aria-label={count ? `${name} · ${count}` : name} className={({ isActive }) => itemClass(isActive || covered)}>
      <Icon icon={icon} />
      <span className="hidden flex-1 md:inline" aria-hidden>
        {name}
      </span>
      {count ? (
        <span className={`hidden min-w-5 rounded-full px-1.5 text-center text-[11px] leading-5 text-canvas md:inline ${hot ? "bg-bad" : "bg-muted"}`} aria-hidden>
          {count}
        </span>
      ) : null}
    </NavLink>
  );
}

// ActivityNav shows how many operations run.
function ActivityNav() {
  const { activity } = useActivity();
  return <NavItem {...activityNav} count={activity?.operations.length ?? 0} />;
}

// UpdatesNav shows how many updates wait, red when one fixes security.
function UpdatesNav() {
  const { view } = useUpdates();
  const items = view?.updates ?? [];
  return <NavItem {...updatesNav} count={items.length} hot={items.some(urgent)} />;
}


export function Header({ search = false }: { search?: boolean }) {
  const { theme, toggle } = useTheme();
  const { lang, setLang } = useI18n();
  const label = theme === "dark" ? text("header.themeLight") : text("header.themeDark");
  return (
    <header className="shrink-0 border-b border-line pt-[env(safe-area-inset-top)]">
      <div className="flex items-center justify-between gap-4 px-4 py-3 md:px-6 md:py-4">
        <Link to="/" className={`inline-flex items-center gap-2 font-medium ${focus}`}>
          <Icon icon={Boxes} />
          MyInfra
        </Link>
        <div className="flex items-center gap-3">
          {search ? <BusyIndicator /> : null}
          {search ? <SearchButton /> : null}
          <select
            aria-label={text("header.lang")}
            value={lang}
            onChange={(event) => setLang(event.target.value as (typeof langs)[number])}
            className={`${search ? "hidden md:block" : ""} bg-transparent text-ink ${focus}`}
          >
            {langs.map((code) => (
              <option key={code} value={code}>
                {langNames[code]}
              </option>
            ))}
          </select>
          <button type="button" onClick={toggle} aria-label={label} className={`${search ? "hidden md:block" : ""} -m-1 rounded-md p-1 text-ink ${focus}`}>
            <Icon icon={theme === "dark" ? Sun : Moon} />
          </button>
        </div>
      </div>
    </header>
  );
}

export function AuthFrame({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-dvh flex-col overflow-hidden">
      <Header />
      <main className="mx-auto min-h-0 w-full max-w-[720px] flex-1 overflow-y-auto px-6 py-24">{children}</main>
    </div>
  );
}

function useRunningDestructions() {
  const location = useLocation();
  const [jobs, setJobs] = useState<Destruction[]>([]);

  useEffect(() => {
    let stop = false;
    const load = () => {
      api<Destruction[]>("/api/destructions")
        .then((rows) => {
          if (!stop) setJobs(rows);
        })
        .catch(() => undefined);
    };
    load();
    const timer = window.setInterval(() => {
      if (!document.hidden) load();
    }, 3000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [location.pathname]);

  return jobs;
}

function useOpenDeployments() {
  const location = useLocation();
  const [items, setItems] = useState<Deployment[]>([]);

  useEffect(() => {
    let stop = false;
    const load = () => {
      api<Deployment[]>("/api/deployments")
        .then((rows) => {
          if (!stop) setItems(rows);
        })
        .catch(() => undefined);
    };
    load();
    const timer = window.setInterval(() => {
      if (!document.hidden) load();
    }, 3000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [location.pathname]);

  return items;
}

type Crumb = { label: string; to?: string };

const TrailContext = createContext<{
  override: Crumb[] | null;
  setOverride: (crumbs: Crumb[] | null) => void;
}>({ override: null, setOverride: () => undefined });

export function useTrail(crumbs: Crumb[]) {
  const { setOverride } = useContext(TrailContext);
  const serialized = JSON.stringify(crumbs);
  useEffect(() => {
    setOverride(JSON.parse(serialized) as Crumb[]);
    return () => setOverride(null);
  }, [serialized, setOverride]);
}

function ReadOnlyBanner() {
  useI18n();
  const { role, canDeploy } = useSession();
  if (canDeploy || role === null) return null;
  return <p className="mb-4 rounded-md border border-line bg-status px-4 py-3 text-[13px] text-muted">{text("readOnly.hint")}</p>;
}

export function Shell() {
  useI18n();
  const location = useLocation();
  const jobs = useRunningDestructions();
  const deploys = useOpenDeployments();
  const [override, setOverride] = useState<Crumb[] | null>(null);
  return (
    <ToastHost>
    <UpdatesHost>
    <ActivityHost>
    <TrailContext.Provider value={{ override, setOverride }}>
    <DestructionJobs.Provider value={jobs}>
    <OpenDeployments.Provider value={deploys}>
    <div className="flex h-dvh flex-col overflow-hidden">
      <Header search />
      <div className="flex min-h-0 flex-1">
        <aside className="hidden h-full min-h-0 w-56 shrink-0 flex-col border-r border-line md:flex">
          <nav className="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto px-2 py-4 md:px-4">
            {nav.map((item) => (
              <NavItem key={item.to} {...item} />
            ))}
            <ActivityNav />
            <UpdatesNav />
            <div className="mt-auto flex flex-col gap-1 border-t border-line pt-4">
              <NavItem {...settingsNav} />
              <AccountNav />
            </div>
          </nav>
          <div className="hidden shrink-0 flex-col gap-1 px-4 pb-4 md:flex">
            <VersionNote />
            <p className="text-[11px] leading-tight text-faint">{text("footer.license")}</p>
          </div>
        </aside>
        {location.pathname === "/" ? (
          <main className="relative min-w-0 flex-1 overflow-y-auto pb-[calc(4rem+env(safe-area-inset-bottom))] md:overflow-hidden md:pb-0">
            <Suspense fallback={<LoadingLines lines={4} />}>
              <Outlet />
            </Suspense>
          </main>
        ) : (
          <main className="min-w-0 flex-1 overflow-y-auto px-4 pb-[calc(6rem+env(safe-area-inset-bottom))] pt-4 md:px-6 md:py-12">
            <div className="mx-auto w-full max-w-[960px]">
              <ReadOnlyBanner />
              <TransferBanner />
              <ReplicaBanner />
              <Breadcrumb />
              <Suspense fallback={<LoadingLines lines={4} />}>
                <Outlet />
              </Suspense>
            </div>
          </main>
        )}
      </div>
    <BottomTabs deployments={deploys.length} removals={jobs.length} />
    <Palette />
    </div>
    </OpenDeployments.Provider>
    </DestructionJobs.Provider>
    </TrailContext.Provider>
    </ActivityHost>
    </UpdatesHost>
    </ToastHost>
  );
}

function Breadcrumb() {
  useI18n();
  const { pathname } = useLocation();
  const { override } = useContext(TrailContext);
  const names = useCrumbNames(pathname);
  const crumbs = override ?? crumbsFor(pathname, names);
  if (crumbs.length === 0) return null;
  return (
    <nav aria-label={text("crumb.trail")} className="breadcrumb mb-4 md:mb-8">
      {crumbs.length > 1 && crumbs[crumbs.length - 2].to ? (
        <Link to={crumbs[crumbs.length - 2].to!} className={`-mx-2 inline-flex min-h-11 items-center gap-1 px-2 text-link md:hidden ${focus}`}>
          <ChevronLeft size={16} strokeWidth={1.5} aria-hidden />
          {crumbs[crumbs.length - 2].label}
        </Link>
      ) : null}
      <ol className="hidden flex-wrap items-center gap-x-2 gap-y-1 md:flex">
        {crumbs.map((crumb, index) => {
          const last = index === crumbs.length - 1;
          return (
            <li key={`${crumb.label}-${index}`} className="flex items-center gap-2">
              {index > 0 ? <span className="text-faint" aria-hidden="true">/</span> : null}
              {last || !crumb.to ? (
                <span className={last ? "text-ink" : "text-muted"}>{crumb.label}</span>
              ) : (
                <Link to={crumb.to} className={`text-link ${focus}`}>
                  {crumb.label}
                </Link>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

function useCrumbNames(pathname: string) {
  const [names, setNames] = useState<Record<string, string>>({});
  useEffect(() => {
    let stop = false;
    loadCrumbNames(pathname)
      .then((found) => {
        if (!stop && found) setNames((current) => ({ ...current, ...found }));
      })
      .catch(() => undefined);
    return () => {
      stop = true;
    };
  }, [pathname]);
  return names;
}

async function loadCrumbNames(pathname: string) {
  const app = pathname.match(/^\/applications\/([^/]+)/);
  if (app) {
    const id = decodeURIComponent(app[1]);
    const row = await api<{ name: string }>(`/api/applications/${id}`);
    return { [`app:${id}`]: row.name };
  }
  const server = pathname.match(/^\/serveurs\/([^/]+)/);
  if (server) {
    const id = decodeURIComponent(server[1]);
    const row = await api<{ name: string }>(`/api/servers/${id}`);
    return { [`server:${id}`]: row.name };
  }
  const run = pathname.match(/^\/templates\/suivis\/([^/]+)$/);
  if (run) {
    const id = decodeURIComponent(run[1]);
    const row = await api<{ templateId: string }>(`/api/template-runs/${id}`);
    const template = row.templateId ? await api<{ name: string }>(`/api/templates/${row.templateId}`) : null;
    return { [`run:${id}`]: template?.name || "Template", [`run-template:${id}`]: row.templateId };
  }
  const template = pathname.match(/^\/templates\/([^/]+)/);
  if (template && template[1] !== "nouveau" && template[1] !== "suivis") {
    const id = decodeURIComponent(template[1]);
    const row = await api<{ name: string }>(`/api/templates/${id}`);
    return { [`template:${id}`]: row.name };
  }
  const pkg = pathname.match(/^\/paquets\/([^/]+)$/);
  if (pkg && pkg[1] !== "nouveau") {
    const id = decodeURIComponent(pkg[1]);
    const row = await api<{ name: string }>(`/api/packages/${id}`);
    return { [`package:${id}`]: row.name };
  }
  const job = pathname.match(/^\/destructions\/([^/]+)$/);
  if (job) {
    const id = decodeURIComponent(job[1]);
    const row = await api<{ name: string }>(`/api/destructions/${id}`);
    return { [`job:${id}`]: row.name };
  }
  return null;
}

function crumbsFor(pathname: string, names: Record<string, string>): Crumb[] {
  const path = pathname.replace(/\/$/, "") || "/";
  if (path === "/") return [];
  if (path === "/applications") return [{ label: text("nav.apps") }];
  if (path === "/nouveau") return [{ label: text("nav.apps"), to: "/applications" }, { label: text("crumb.newApp") }];
  const app = path.match(/^\/applications\/([^/]+)(\/terminal\/[^/]+|\/services)?$/);
  if (app) {
    const id = decodeURIComponent(app[1]);
    const crumbs: Crumb[] = [{ label: text("nav.apps"), to: "/applications" }, { label: names[`app:${id}`] || text("crumb.application"), to: `/applications/${id}` }];
    if (app[2] === "/services") crumbs.push({ label: text("appServices.crumb") });
    else if (app[2]) crumbs.push({ label: text("crumb.terminal") });
    return current(crumbs);
  }
  if (path === "/clusters") return [{ label: text("nav.clusters") }];
  if (path === "/historique") return [{ label: text("nav.history") }];
  const server = path.match(/^\/serveurs\/([^/]+)(\/terminal)?$/);
  if (server) {
    const id = decodeURIComponent(server[1]);
    const crumbs: Crumb[] = [{ label: text("nav.clusters"), to: "/clusters" }, { label: names[`server:${id}`] || text("crumb.server"), to: `/serveurs/${id}` }];
    if (server[2]) crumbs.push({ label: text("crumb.terminal") });
    return current(crumbs);
  }
  if (path === "/templates") return [{ label: text("nav.templates") }];
  if (path === "/templates/nouveau") return [{ label: text("nav.templates"), to: "/templates" }, { label: text("crumb.newTemplate") }];
  const follow = path.match(/^\/templates\/suivis\/([^/]+)$/);
  if (follow) {
    const id = decodeURIComponent(follow[1]);
    const templateId = names[`run-template:${id}`];
    return current([
      { label: text("nav.templates"), to: "/templates" },
      { label: names[`run:${id}`] || text("crumb.template"), to: templateId ? `/templates/${templateId}` : undefined },
      { label: text("crumb.follow") },
    ]);
  }
  const template = path.match(/^\/templates\/([^/]+)(\/deploy)?$/);
  if (template) {
    const id = decodeURIComponent(template[1]);
    const crumbs: Crumb[] = [{ label: text("nav.templates"), to: "/templates" }, { label: names[`template:${id}`] || text("crumb.template"), to: `/templates/${id}` }];
    if (template[2]) crumbs.push({ label: text("crumb.deploy") });
    return current(crumbs);
  }
  if (path === "/paquets") return [{ label: text("nav.packages") }];
  if (path === "/paquets/nouveau") return [{ label: text("nav.packages"), to: "/paquets" }, { label: text("package.new") }];
  const pkg = path.match(/^\/paquets\/([^/]+)$/);
  if (pkg) {
    const id = decodeURIComponent(pkg[1]);
    return current([{ label: text("nav.packages"), to: "/paquets" }, { label: names[`package:${id}`] || text("nav.packages") }]);
  }
  if (path === "/deploiements") return [{ label: text("crumb.deployments") }];
  if (path === "/suppressions") return [{ label: text("crumb.destructions") }];
  const job = path.match(/^\/destructions\/([^/]+)$/);
  if (job) {
    const id = decodeURIComponent(job[1]);
    return current([{ label: text("crumb.destructions"), to: "/suppressions" }, { label: names[`job:${id}`] || text("crumb.destruction") }]);
  }
  const secret = path.match(/^\/secrets\/([^/]+)$/);
  if (secret) {
    const id = decodeURIComponent(secret[1]);
    return current([{ label: text("nav.settings"), to: "/reglages" }, { label: text("crumb.secrets"), to: "/secrets" }, { label: names[`secret:${id}`] || text("crumb.secret") }]);
  }
  const fixed: Record<string, MsgKey> = {
    "/cles": "nav.ssh",
    "/secrets": "nav.secrets",
    "/providers": "nav.providers",
    "/dns": "nav.dns",
    "/registres": "nav.registries",
    "/images": "nav.images",
    "/compte": "nav.account",
    "/utilisateurs": "nav.users",
    "/transfert": "nav.transfer",
    "/copies": "nav.copies",
    "/dns/enregistrements": "nav.dnsRecords",
  };
  const settings: Crumb = { label: text("nav.settings"), to: "/reglages" };
  if (path === "/transfert/suivi") return [settings, { label: text("nav.transfer"), to: "/transfert" }, { label: text("crumb.transferFollow") }];
  if (path === "/historique") return [{ label: text("nav.activity") }];
  if (fixed[path]) return [settings, { label: text(fixed[path]) }];
  return [];
}

function current(crumbs: Crumb[]): Crumb[] {
  return crumbs.map((crumb, index) => (index === crumbs.length - 1 ? { label: crumb.label } : crumb));
}

export function PageTitle({
  title,
  icon,
  mark,
  action,
}: {
  title: string;
  icon?: LucideIcon;
  mark?: ReactNode;
  action?: ReactNode;
}) {
  // Each page names its tab, so that several tabs can be told apart. A page
  // without a title, such as the overview, leaves just MyInfra.
  useEffect(() => {
    document.title = title ? `${title} · MyInfra` : "MyInfra";
    return () => {
      document.title = "MyInfra";
    };
  }, [title]);
  return (
    <div className="mb-6 flex flex-wrap items-center justify-between gap-x-6 gap-y-3 md:mb-12">
      <h1 className="flex min-w-0 items-center gap-3 text-xl font-medium md:text-2xl">
        {mark ? mark : icon ? <Icon icon={icon} /> : null}
        {title}
      </h1>
      {action}
    </div>
  );
}

export type ListMode = "cards" | "rows";

const listKey = "myinfra-list";

export function useListMode(): [ListMode, () => void] {
  const [mode, setMode] = useState<ListMode>(() => (localStorage.getItem(listKey) === "rows" ? "rows" : "cards"));

  function toggle() {
    setMode((current) => {
      const next = current === "cards" ? "rows" : "cards";
      localStorage.setItem(listKey, next);
      return next;
    });
  }

  return [mode, toggle];
}

export function ListModeButton({ mode, onToggle }: { mode: ListMode; onToggle: () => void }) {
  useI18n();
  const cards = mode === "cards";
  return (
    <button type="button" onClick={onToggle} aria-label={cards ? text("list.rows") : text("list.cards")} className={`-m-1 rounded-md p-1 text-ink ${focus}`}>
      <Icon icon={cards ? List : LayoutGrid} />
    </button>
  );
}

export function ListActions({
  mode,
  onToggle,
  label,
  onCreate,
}: {
  mode: ListMode;
  onToggle: () => void;
  label: string;
  onCreate: () => void;
}) {
  // Nothing is created while MyInfra moves: it would stay behind.
  const { transferring } = useActivity();
  return (
    <div className="flex items-center gap-4">
      {/* On a phone both modes show one column: the switch would change nothing. */}
      <span className="hidden md:inline-flex">
        <ListModeButton mode={mode} onToggle={onToggle} />
      </span>
      <Button type="button" onClick={onCreate} disabled={transferring} title={transferring ? text("activity.transferBanner") : undefined}>
        <Icon icon={Plus} />
        {label}
      </Button>
    </div>
  );
}

export function EmptyList({ message, label, onCreate }: { message: string; label: string; onCreate: () => void }) {
  const { transferring } = useActivity();
  return (
    <div className="flex flex-col items-start gap-6">
      <p className="text-muted">{message}</p>
      <Button type="button" onClick={onCreate} disabled={transferring} title={transferring ? text("activity.transferBanner") : undefined}>
        <Icon icon={Plus} />
        {label}
      </Button>
    </div>
  );
}

export function ItemList({ mode, columns = 2, children }: { mode: ListMode; columns?: 1 | 2; children: ReactNode }) {
  const cards = columns === 2 ? "grid grid-cols-1 gap-4 md:grid-cols-2" : "grid grid-cols-1 gap-4";
  return <ul className={mode === "cards" ? cards : "flex flex-col"}>{children}</ul>;
}

export function entryClass(mode: ListMode, layout: "spread" | "stack" = "spread") {
  if (mode === "cards") {
    return "flex h-full flex-col gap-3 rounded-md border border-line bg-surface p-4";
  }
  if (layout === "stack") {
    return "flex flex-col gap-3 border-t border-line py-4";
  }
  return "flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-line py-4";
}

export function HealthDot({ status }: { status?: string }) {
  const label = statusLabel(status || "");
  const color = label.tone === "text-ok" ? "bg-ok" : label.tone === "text-bad" ? "bg-bad" : label.tone === "text-warn" ? "bg-warn" : "bg-faint";
  return <span className={`inline-block size-2.5 shrink-0 rounded-full ${color}`} role="img" aria-label={label.text} title={label.text} />;
}

// serverState is the status to show for a server: what this copy sees of it
// before what was recorded.
export function serverState(server: { status: string; reach?: string }) {
  return server.reach || server.status;
}

// liveServer tells whether a server is linked and still answers.
export function liveServer(server: { status: string; reach?: string }) {
  return server.status === "linked" && !server.reach;
}

export function statusLabel(status: string) {
  switch (status) {
    case "linked":
      return { text: text("status.linked"), tone: "text-ok" };
    case "deploying":
      return { text: text("status.deploying"), tone: "text-warn" };
    case "updating":
      return { text: text("status.updating"), tone: "text-warn" };
    case "pending":
      return { text: text("status.pending"), tone: "text-warn" };
    case "pending_dns":
      return { text: text("status.pending_dns"), tone: "text-warn" };
    case "ready":
      return { text: text("status.ready"), tone: "text-ok" };
    case "preparing":
      return { text: text("status.preparing"), tone: "text-warn" };
    case "running":
      return { text: text("status.running"), tone: "text-ok" };
    case "failed":
      return { text: text("status.failed"), tone: "text-bad" };
    case "paused":
      return { text: text("status.paused"), tone: "text-muted" };
    case "done":
      return { text: text("status.done"), tone: "text-ok" };
    case "cancelled":
      return { text: text("status.cancelled"), tone: "text-muted" };
    case "removing":
      return { text: text("status.removing"), tone: "text-warn" };
    case "unpublished":
      return { text: text("status.unpublished"), tone: "text-muted" };
    case "lost":
    case "unreachable":
      return { text: text("status.unreachable"), tone: "text-bad" };
    case "gone":
      return { text: text("status.gone"), tone: "text-bad" };
    case "":
      // Not delivered yet: the first delivery is on its way.
      return { text: text("status.preparing"), tone: "text-warn" };
    default:
      return { text: text("status.unknown"), tone: "text-muted" };
  }
}
