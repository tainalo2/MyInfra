import { NavLink, useLocation } from "react-router-dom";
import { text } from "./i18n";
import { useI18n } from "./lang";
import { roleLabel, useSession } from "./session";
import { focus } from "./ui";

export function userInitials(login: string): string {
  const name = login.trim();
  if (!name) return "?";
  const parts = name.split(/[\s._-]+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return name.slice(0, 2).toUpperCase();
}

export function userDisplayName(login: string, role: "admin" | "deploy" | "read" | null): string {
  if (login.trim()) return login;
  if (role) return roleLabel(role);
  return text("nav.account");
}

export function AccountNav() {
  useI18n();
  const { login, role } = useSession();
  const location = useLocation();
  const active = location.pathname === "/compte" || location.pathname.startsWith("/compte/");
  const label = userDisplayName(login, role);
  const initials = userInitials(login || label);

  return (
    <NavLink
      to="/compte"
      className={`flex items-center gap-3 rounded-md px-3 py-2 ${focus} ${active ? "bg-status text-ink" : "text-muted"}`}
    >
      <span
        className="flex size-8 shrink-0 items-center justify-center rounded-full bg-ink text-[12px] font-medium text-canvas"
        aria-hidden
      >
        {initials}
      </span>
      <span className="hidden min-w-0 flex-1 truncate md:inline">{label}</span>
    </NavLink>
  );
}
