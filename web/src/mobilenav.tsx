import { useState } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import {
  ArrowUpCircle,
  Globe,
  History,
  LayoutGrid,
  LayoutTemplate,
  Menu,
  Moon,
  Network,
  Settings,
  Sun,
  type LucideIcon,
} from "lucide-react";
import { langNames, langs, text, type MsgKey } from "./i18n";
import { Sheet } from "./kit";
import { useI18n } from "./lang";
import { useTheme } from "./theme";
import { Icon, focus } from "./ui";
import { settingsMatch } from "./navmap";
import { urgent, useUpdates } from "./updates";

type Tab = { to: string; label: MsgKey; icon: LucideIcon; end?: boolean; match?: string[] };

const tabs: Tab[] = [
  { to: "/", label: "nav.home", icon: Globe, end: true },
  { to: "/applications", label: "nav.apps", icon: LayoutGrid, match: ["/applications", "/nouveau", "/catalogue"] },
  { to: "/clusters", label: "nav.servers", icon: Network, match: ["/clusters", "/serveurs"] },
  { to: "/historique", label: "nav.activity", icon: History, match: ["/historique", "/deploiements", "/suppressions", "/destructions"] },
];

const more: Tab[] = [
  { to: "/templates", label: "nav.templates", icon: LayoutTemplate },
  { to: "/mises-a-jour", label: "nav.updates", icon: ArrowUpCircle },
  { to: "/reglages", label: "nav.settings", icon: Settings, match: settingsMatch },
];

// BottomTabs is the navigation of a phone: the four places used most, within
// reach of the thumb, and the rest in a sheet.
export function BottomTabs({ deployments, removals }: { deployments: number; removals: number }) {
  useI18n();
  const location = useLocation();
  const [open, setOpen] = useState(false);
  const busy = deployments + removals;
  const { view } = useUpdates();
  const updates = view?.updates.length ?? 0;
  const hot = view?.updates.some(urgent) ?? false;
  const inMore = more.some((item) => location.pathname.startsWith(item.to) || (item.match?.some((prefix) => location.pathname.startsWith(prefix)) ?? false));
  return (
    <>
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
                `relative flex min-h-14 flex-col items-center justify-center gap-1 text-[11px] ${focus} ${isActive || covered ? "text-ink" : "text-muted"}`
              }
            >
              <Icon icon={tab.icon} />
              <span className="max-w-full truncate px-1">{text(tab.label)}</span>
              {tab.to === "/historique" && busy > 0 ? (
                <span className="absolute right-[calc(50%-1.25rem)] top-1.5 min-w-4 rounded-full bg-warn px-1 text-center text-[10px] leading-4 text-canvas" aria-label={text("nav.running", { count: busy })}>
                  {busy}
                </span>
              ) : null}
            </NavLink>
          );
        })}
        <button
          type="button"
          onClick={() => setOpen(true)}
          aria-haspopup="dialog"
          className={`flex min-h-14 flex-col items-center justify-center gap-1 text-[11px] ${focus} ${inMore ? "text-ink" : "text-muted"}`}
        >
          <span className="relative">
            <Icon icon={Menu} />
            {updates > 0 ? <span className={`absolute -right-1.5 -top-1 size-2.5 rounded-full ${hot ? "bg-bad" : "bg-link"}`} aria-label={text("upd.waiting", { count: updates })} /> : null}
          </span>
          {text("nav.more")}
        </button>
      </nav>
      {open ? <MoreSheet onClose={() => setOpen(false)} /> : null}
    </>
  );
}

function MoreSheet({ onClose }: { onClose: () => void }) {
  const { lang, setLang } = useI18n();
  const { theme, toggle } = useTheme();
  const navigate = useNavigate();
  const { view } = useUpdates();
  const updates = view?.updates.length ?? 0;
  const hot = view?.updates.some(urgent) ?? false;
  const items: Tab[] = more;
  return (
    <Sheet title={text("nav.more")} onClose={onClose}>
      <ul className="-mx-2 flex flex-col">
        {items.map((item) => (
          <li key={item.to}>
            <button
              type="button"
              onClick={() => {
                onClose();
                navigate(item.to);
              }}
              className={`flex min-h-12 w-full items-center gap-3 rounded-md px-2 text-left ${focus}`}
            >
              <Icon icon={item.icon} className="text-muted" />
              <span className="flex-1">{text(item.label)}</span>
              {item.to === "/mises-a-jour" && updates > 0 ? (
                <span className={`min-w-5 rounded-full px-1.5 text-center text-[11px] leading-5 text-canvas ${hot ? "bg-bad" : "bg-muted"}`}>{updates}</span>
              ) : null}
            </button>
          </li>
        ))}
      </ul>
      <div className="mt-4 flex flex-col gap-3 border-t border-line pt-4">
        <label className="flex min-h-12 items-center justify-between gap-4">
          <span>{text("header.lang")}</span>
          <select value={lang} onChange={(event) => setLang(event.target.value as (typeof langs)[number])} className={`rounded-md border border-line bg-surface px-3 py-2 ${focus}`}>
            {langs.map((code) => (
              <option key={code} value={code}>
                {langNames[code]}
              </option>
            ))}
          </select>
        </label>
        <button type="button" onClick={toggle} className={`flex min-h-12 items-center justify-between gap-4 text-left ${focus}`}>
          <span>{theme === "dark" ? text("header.themeLight") : text("header.themeDark")}</span>
          <Icon icon={theme === "dark" ? Sun : Moon} />
        </button>
        <p className="text-[12px] text-faint">{text("footer.license")}</p>
      </div>
    </Sheet>
  );
}
