import { useEffect } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { ArrowUpCircle, Globe, History, LayoutGrid, LayoutTemplate, Menu, Moon, Network, Settings, Sun, type LucideIcon } from "lucide-react";
import { langNames, langs, text, type MsgKey } from "./i18n";
import { useI18n } from "./lang";
import { useTheme } from "./theme";
import { PageTitle, focus } from "./ui";
import { settingsMatch } from "./navmap";
import { urgent, useUpdates } from "./updates";
import { userDisplayName, userInitials } from "./account-nav";
import { useSession } from "./session";
import { ListRow, RowGroup, RowState } from "./rows";
import { useWide } from "./kit";

type Tab = { to: string; label: MsgKey; icon: LucideIcon; end?: boolean; match?: string[] };

const more = ["/plus", "/templates", "/mises-a-jour", "/compte", ...settingsMatch];

const tabs: Tab[] = [
  { to: "/", label: "nav.home", icon: Globe, end: true },
  { to: "/applications", label: "nav.appsShort", icon: LayoutGrid, match: ["/applications", "/nouveau", "/catalogue"] },
  { to: "/clusters", label: "nav.servers", icon: Network, match: ["/clusters", "/serveurs"] },
  { to: "/historique", label: "nav.activity", icon: History, match: ["/historique", "/deploiements", "/suppressions", "/destructions"] },
  { to: "/plus", label: "nav.more", icon: Menu, match: more },
];

// BottomTabs is the navigation of a phone: the four places used most, within
// reach of the thumb, and the rest under Plus.
export function BottomTabs({ deployments, removals }: { deployments: number; removals: number }) {
  useI18n();
  const location = useLocation();
  const busy = deployments + removals;
  const { view } = useUpdates();
  const updates = view?.updates.length ?? 0;
  const hot = view?.updates.some(urgent) ?? false;
  return (
    <nav
      aria-label={text("nav.main")}
      className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-5 border-t border-line bg-surface pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      {tabs.map((tab) => {
        const covered = tab.match?.some((prefix) => location.pathname.startsWith(prefix)) ?? false;
        return (
          <NavLink
            key={tab.to}
            to={tab.to}
            end={tab.end}
            className={({ isActive }) =>
              `flex min-h-[58px] flex-col items-center justify-center gap-[3px] text-[11px] ${focus} ${isActive || covered ? "text-ink" : "text-faint"}`
            }
          >
            <span className="relative inline-flex">
              <tab.icon size={22} strokeWidth={1.75} aria-hidden />
              {tab.to === "/historique" && busy > 0 ? (
                <span className="absolute -right-2.5 -top-1.5 min-w-4 rounded-full bg-warn px-1 text-center text-[10px] leading-4 text-canvas" aria-label={text("nav.running", { count: busy })}>
                  {busy}
                </span>
              ) : null}
              {tab.to === "/plus" && updates > 0 ? (
                <span className={`absolute -right-1 -top-0.5 size-2 rounded-full ${hot ? "bg-bad" : "bg-link"}`} aria-label={text("upd.waiting", { count: updates })} />
              ) : null}
            </span>
            <span className="max-w-full truncate px-1">{text(tab.label)}</span>
          </NavLink>
        );
      })}
    </nav>
  );
}

// MorePage is the Plus tab of a phone: the places used less, the account,
// the language and the theme. A wide screen has them in its menu.
export function MorePage() {
  const { login, role } = useSession();
  const { lang, setLang } = useI18n();
  const { theme, toggle } = useTheme();
  const { view } = useUpdates();
  const wide = useWide();
  const navigate = useNavigate();
  const updates = view?.updates.length ?? 0;
  const hot = view?.updates.some(urgent) ?? false;

  useEffect(() => {
    if (wide) navigate("/reglages", { replace: true });
  }, [wide, navigate]);

  return (
    <>
      <PageTitle title={text("nav.more")} />
      <div className="flex flex-col gap-6">
        <RowGroup>
          <ListRow
            to="/compte"
            tile={<span className="flex size-10 items-center justify-center rounded-md bg-ink text-[13px] font-medium text-canvas">{userInitials(login || userDisplayName(login, role))}</span>}
            title={userDisplayName(login, role)}
            sub={role ? text(`role.${role}` as MsgKey) : undefined}
          />
        </RowGroup>
        <RowGroup>
          <ListRow to="/templates" icon={LayoutTemplate} title={text("nav.templates")} />
          <ListRow to="/mises-a-jour" icon={ArrowUpCircle} title={text("nav.updates")} end={updates > 0 ? <RowState tone={hot ? "bad" : "muted"}>{updates}</RowState> : null} />
          <ListRow to="/reglages" icon={Settings} title={text("nav.settings")} />
        </RowGroup>
        <RowGroup>
          <label className="row row-tiled flex min-h-14 items-center gap-3 px-4 py-2">
            <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-md bg-status text-muted">
              <Globe size={20} strokeWidth={1.75} />
            </span>
            <span className="flex-1">{text("header.lang")}</span>
            <select value={lang} onChange={(event) => setLang(event.target.value as (typeof langs)[number])} className={`bg-transparent text-[14px] text-muted ${focus}`}>
              {langs.map((code) => (
                <option key={code} value={code}>
                  {langNames[code]}
                </option>
              ))}
            </select>
          </label>
          <ListRow
            onClick={toggle}
            icon={theme === "dark" ? Moon : Sun}
            title={text("settings.theme")}
            chevron={false}
            end={<RowState>{theme === "dark" ? text("theme.dark") : text("theme.light")}</RowState>}
          />
        </RowGroup>
        <p className="px-1 text-[12px] text-faint">{text("footer.license")}</p>
      </div>
    </>
  );
}
