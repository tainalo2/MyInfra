import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { Navigate } from "react-router-dom";
import { api } from "./api";
import { text } from "./i18n";
import { LoadingLines } from "./ui";

export type UserRole = "admin" | "deploy" | "read";

export type UserAccount = {
  id: string;
  login: string;
  role: UserRole;
  totp: boolean;
};

type SessionState = {
  role: UserRole | null;
  login: string;
  totp: boolean;
  canDeploy: boolean;
  canAdmin: boolean;
  refresh: () => Promise<void>;
  clear: () => void;
};

const SessionContext = createContext<SessionState>({
  role: null,
  login: "",
  totp: false,
  canDeploy: false,
  canAdmin: false,
  refresh: async () => undefined,
  clear: () => undefined,
});

function normalizeRole(value: string | undefined): UserRole {
  if (value === "deploy" || value === "read") return value;
  return "admin";
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [role, setRole] = useState<UserRole | null>(null);
  const [login, setLogin] = useState("");
  const [totp, setTotp] = useState(false);

  const refresh = useCallback(async () => {
    const session = await api<{ role?: string; login?: string; totp?: boolean }>("/api/session");
    setRole(normalizeRole(session.role));
    setLogin(session.login ?? "");
    setTotp(Boolean(session.totp));
  }, []);

  const clear = useCallback(() => {
    setRole(null);
    setLogin("");
    setTotp(false);
  }, []);

  const value = useMemo(() => {
    const canAdmin = role === "admin";
    const canDeploy = canAdmin || role === "deploy";
    return { role, login, totp, canDeploy, canAdmin, refresh, clear };
  }, [role, login, totp, refresh, clear]);

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession() {
  return useContext(SessionContext);
}

export function SessionGate({ children }: { children: ReactNode }) {
  const { role, refresh } = useSession();
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    void refresh()
      .catch(() => undefined)
      .finally(() => setLoading(false));
  }, [refresh]);

  if (loading && role === null) {
    return (
      <main className="mx-auto max-w-[720px] px-6 py-12">
        <LoadingLines lines={3} />
      </main>
    );
  }
  return children;
}

export function RequireDeploy({ children }: { children: ReactNode }) {
  const { canDeploy, role } = useSession();
  if (role === null) {
    return (
      <main className="mx-auto max-w-[720px] px-6 py-12">
        <LoadingLines lines={3} />
      </main>
    );
  }
  if (!canDeploy) return <Navigate to="/applications" replace />;
  return children;
}

export function RequireAdmin({ children }: { children: ReactNode }) {
  const { canAdmin, role } = useSession();
  if (role === null) {
    return (
      <main className="mx-auto max-w-[720px] px-6 py-12">
        <LoadingLines lines={3} />
      </main>
    );
  }
  if (!canAdmin) return <Navigate to="/reglages" replace />;
  return children;
}

export function roleLabel(role: UserRole): string {
  if (role === "deploy") return text("role.deploy");
  if (role === "read") return text("role.read");
  return text("role.admin");
}
