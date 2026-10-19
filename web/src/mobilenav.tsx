import { useState } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import {
  ArrowRightLeft,
  Cloud,
  GitBranch,
  Globe,
  History,
  House,
  KeyRound,
  LayoutGrid,
  LayoutTemplate,
  Lock,
  Menu,
  Moon,
  Network,
  Rocket,
  Shield,
  Sun,
  Trash2,
  Waypoints,
  type LucideIcon,
} from "lucide-react";
import { langNames, langs, text, type MsgKey } from "./i18n";
import { Sheet } from "./kit";
import { useI18n } from "./lang";
import { useTheme } from "./theme";
import { Icon, focus } from "./ui";

type Tab = { to: string; label: MsgKey; icon: LucideIcon; end?: boolean; match?: string[] };

const tabs: Tab[] = [
  { to: "/", label: "nav.home", icon: House, end: true },
  { to: "/applications", label: "nav.apps", icon: LayoutGrid, match: ["/applications", "/nouveau", "/catalogue"] },
  { to: "/clusters", label: "nav.servers", icon: Network, match: ["/clusters", "/serveurs"] },
  { to: "/historique", label: "nav.activity", icon: History, match: ["/historique", "/deploiements", "/suppressions", "/destructions"] },
];

const more: Tab[] = [
  { to: "/templates", label: "nav.templates", icon: LayoutTemplate },
  { to: "/providers", label: "nav.providers", icon: Cloud },
  { to: "/dns", label: "nav.dns", icon: Globe },
  { to: "/git", label: "nav.git", icon: GitBranch },
  { to: "/cles", label: "nav.ssh", icon: KeyRound },
  { to: "/secrets", label: "nav.secrets", icon: Shield },
  { to: "/transfert", label: "nav.transfer", icon: ArrowRightLeft },
  { to: "/copies", label: "nav.copies", icon: Waypoints },
  { to: "/compte", label: "nav.account", icon: Lock },
];

// BottomTabs is the navigation of a phone: the four places used most, within
// reach of the thumb, and the rest in a sheet.
export function BottomTabs({ deployments, removals }: { deployments: number; removals: number }) {
  useI18n();
  const location = useLocation();
  const [open, setOpen] = useState(false);
  const busy = deployments + removals;
  const inMore = more.some((item) => location.pathname.startsWith(item.to));
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
          <Icon icon={Menu} />
          {text("nav.more")}
        </button>
      </nav>
      {open ? <MoreSheet deployments={deployments} removals={removals} onClose={() => setOpen(false)} /> : null}
    </>
  );
}

function MoreSheet({ deployments, removals, onClose }: { deployments: number; removals: number; onClose: () => void }) {
  const { lang, setLang } = useI18n();
  const { theme, toggle } = useTheme();
  const navigate = useNavigate();
  const items: Tab[] = [
    ...(deployments > 0 ? [{ to: "/deploiements", label: "nav.deployments" as MsgKey, icon: Rocket }] : []),
    ...(removals > 0 ? [{ to: "/suppressions", label: "nav.destructions" as MsgKey, icon: Trash2 }] : []),
    ...more,
  ];
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
              {text(item.label)}
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
