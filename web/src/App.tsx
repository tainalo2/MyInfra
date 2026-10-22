import { FormEvent, Suspense, lazy, useEffect, useState, type ComponentType, type ReactNode } from "react";
import { Boxes } from "lucide-react";
import { Navigate, Route, Routes, useNavigate } from "react-router-dom";
import { api, returnPath } from "./api";
import { Spinner } from "./kit";
import { text } from "./i18n";
import { useI18n } from "./lang";
import { RequireAdmin, RequireDeploy, SessionGate, useSession } from "./session";
import { LoginTotpPage, saveTotpChallenge } from "./login-totp";
import { AuthFrame, Button, ErrorText, Field, Icon, Input, LoadingLines, Shell } from "./ui";

type LoginAnswer = { ok?: boolean; totpRequired?: boolean; challenge?: string };

// Each screen is loaded when it is first opened, so the first page is light.
function page<M, K extends keyof M>(load: () => Promise<M>, name: K) {
  return lazy(() => load().then((module) => ({ default: module[name] as ComponentType })));
}

const CatalogueArticle = page(() => import("./catalogue"), "CatalogueArticle");
const Home = page(() => import("./home"), "Home");
const NewApp = page(() => import("./apps"), "NewApp");
const ApplicationPage = page(() => import("./apps"), "ApplicationPage");
const ServerPage = page(() => import("./servers"), "ServerPage");
const PodTerminal = page(() => import("./terminal"), "PodTerminal");
const ServerTerminal = page(() => import("./terminal"), "ServerTerminal");
const Keys = page(() => import("./keys"), "Keys");
const Secrets = page(() => import("./secrets"), "Secrets");
const SecretPage = page(() => import("./secrets"), "SecretPage");
const Providers = page(() => import("./accounts"), "Providers");
const Dns = page(() => import("./accounts"), "Dns");
const Registries = page(() => import("./accounts"), "Registries");
const Images = page(() => import("./localimages"), "Images");
const Clusters = page(() => import("./clusters"), "Clusters");
const Account = page(() => import("./account"), "Account");
const Transfer = page(() => import("./transfer"), "Transfer");
const TransferFollow = page(() => import("./transfer"), "TransferFollow");
const Copies = page(() => import("./copies"), "Copies");
const UpdatesPage = page(() => import("./updates"), "UpdatesPage");
const DnsRecords = page(() => import("./dnsrecords"), "DnsRecords");
const SettingsPage = page(() => import("./settings"), "SettingsPage");
const Templates = page(() => import("./templates"), "Templates");
const TemplateEditor = page(() => import("./templates"), "TemplateEditor");
const TemplateLaunch = page(() => import("./templates"), "TemplateLaunch");
const TemplateRunPage = page(() => import("./templates"), "TemplateRunPage");
const DestructionPage = page(() => import("./destructions"), "DestructionPage");
const DestructionsPage = page(() => import("./destructions"), "DestructionsPage");
const DeploymentsPage = page(() => import("./deployments"), "DeploymentsPage");
const HistoryPage = page(() => import("./world"), "HistoryPage");
const Overview = page(() => import("./overview"), "Overview");
const Start = page(() => import("./start"), "Start");
const UsersPage = page(() => import("./users"), "UsersPage");

function Gate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<"load" | "in" | "setup" | "login" | "start" | "down">("load");

  useEffect(() => {
    let cancel = false;
    (async () => {
      let setup: { needed: boolean };
      try {
        setup = await api<{ needed: boolean }>("/api/setup");
      } catch {
        if (!cancel) setState("down");
        return;
      }
      if (cancel) return;
      if (setup.needed) {
        setState("setup");
        return;
      }
      try {
        const session = await api<{ start?: boolean }>("/api/session");
        if (cancel) return;
        setState(session.start ? "start" : "in");
      } catch {
        if (!cancel) setState("login");
      }
    })();
    return () => {
      cancel = true;
    };
  }, []);

  if (state === "load") {
    return (
      <main className="mx-auto max-w-[720px] px-6 py-12">
        <LoadingLines lines={3} />
      </main>
    );
  }
  if (state === "down") {
    return (
      <main className="mx-auto max-w-[720px] px-6 py-12">
        <ErrorText>{text("common.error")}</ErrorText>
      </main>
    );
  }
  if (state === "setup") return <Navigate to="/depart" replace />;
  if (state === "login") return <Navigate to="/entree" replace />;
  if (state === "start") return <Navigate to="/mise-en-route" replace />;
  return (
    <SessionGate>
      {children}
    </SessionGate>
  );
}

function PasswordForm({
  title,
  path,
  submit,
}: {
  title: string;
  path: string;
  submit: string;
}) {
  useI18n();
  const navigate = useNavigate();
  const { refresh } = useSession();
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [multiUser, setMultiUser] = useState(false);
  const [primaryLogin, setPrimaryLogin] = useState("");
  const [again, setAgain] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const choosing = path === "/api/setup";
  const signingIn = path === "/api/session";

  useEffect(() => {
    let cancel = false;
    api<{ needed: boolean; users?: number; primaryLogin?: string }>("/api/setup")
      .then(async (setup) => {
        if (cancel) return;
        if (!choosing && typeof setup.users === "number") setMultiUser(setup.users > 1);
        if (!choosing && setup.primaryLogin) setPrimaryLogin(setup.primaryLogin);
        if (setup.needed && !choosing) navigate("/depart", { replace: true });
        if (!setup.needed && choosing) navigate("/", { replace: true });
        if (!setup.needed && !choosing) {
          await api("/api/session");
          if (!cancel) navigate(returnPath(), { replace: true });
        }
      })
      .catch(() => undefined);
    return () => {
      cancel = true;
    };
  }, [path, navigate]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setError("");
    if (choosing && password !== again) {
      setError(text("auth.mismatch"));
      return;
    }
    setBusy(true);
    try {
      if (choosing) {
        await api(path, { method: "POST", body: JSON.stringify({ password }) });
        navigate("/", { replace: true });
        return;
      }
      const answer = await api<LoginAnswer>(path, {
        method: "POST",
        body: JSON.stringify({ login: multiUser ? login.trim() : "", password }),
      });
      if (answer.totpRequired && answer.challenge) {
        saveTotpChallenge(answer.challenge);
        navigate("/entree/2fa", { replace: true });
        return;
      }
      await refresh();
      navigate(returnPath(), { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  return (
    <AuthFrame>
      <h1 className="mb-2 flex items-center gap-3 text-2xl font-medium">
        <Icon icon={Boxes} />
        MyInfra
      </h1>
      <p className="mb-12 text-muted">{title}</p>
      <form onSubmit={onSubmit} autoComplete="on" className="flex flex-col gap-6">
        {signingIn && multiUser ? (
          <Field label={text("auth.login")}>
            <Input
              id="username"
              name="username"
              value={login}
              autoComplete="username"
              onChange={(e) => setLogin(e.target.value)}
              required
            />
          </Field>
        ) : null}
        {signingIn && !multiUser && primaryLogin ? (
          <input
            type="text"
            name="username"
            autoComplete="username"
            value={primaryLogin}
            readOnly
            tabIndex={-1}
            aria-hidden="true"
            className="pointer-events-none absolute h-0 w-0 opacity-0"
          />
        ) : null}
        <Field label={text("auth.password")} hint={signingIn ? text("auth.passwordLoginHint") : undefined}>
          <Input
            id={signingIn ? "password" : choosing ? "new-password" : "password"}
            name="password"
            type="password"
            value={password}
            autoFocus={!signingIn || !multiUser}
            autoComplete={choosing ? "new-password" : "current-password"}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </Field>
        {choosing ? (
          <Field label={text("auth.again")}>
            <Input
              id="new-password-confirm"
              name="new-password"
              type="password"
              value={again}
              autoComplete="new-password"
              onChange={(e) => setAgain(e.target.value)}
              required
            />
          </Field>
        ) : null}
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div>
          <Button type="submit" disabled={busy || password === ""}>
            {busy ? <Spinner /> : null}
            {submit}
          </Button>
        </div>
      </form>
    </AuthFrame>
  );
}

export function App() {
  useI18n();
  return (
    <Suspense
      fallback={
        <main className="mx-auto max-w-[720px] px-6 py-12">
          <LoadingLines lines={3} />
        </main>
      }
    >
    <Routes>
      <Route path="/depart" element={<PasswordForm title={text("auth.choose")} path="/api/setup" submit={text("auth.continue")} />} />
      <Route path="/entree" element={<PasswordForm title={text("auth.enter")} path="/api/session" submit={text("auth.enter")} />} />
      <Route path="/entree/2fa" element={<LoginTotpPage />} />
      <Route path="/mise-en-route" element={<Start />} />
      <Route
        element={
          <Gate>
            <Shell />
          </Gate>
        }
      >
        <Route path="/" element={<Overview />} />
        <Route path="/applications" element={<Home />} />
        <Route path="/nouveau" element={<RequireDeploy><NewApp /></RequireDeploy>} />
        <Route path="/applications/:id" element={<ApplicationPage />} />
        <Route path="/applications/:id/terminal/:pod" element={<RequireDeploy><PodTerminal /></RequireDeploy>} />
        <Route path="/cles" element={<Keys />} />
        <Route path="/secrets" element={<Secrets />} />
        <Route path="/secrets/:id" element={<SecretPage />} />
        <Route path="/providers" element={<Providers />} />
        <Route path="/dns" element={<Dns />} />
        <Route path="/dns/enregistrements" element={<DnsRecords />} />
        {/* Building from a Git repository does not exist yet: its access page waits for it. */}
        <Route path="/git" element={<Navigate to="/reglages" replace />} />
        <Route path="/registres" element={<Registries />} />
        <Route path="/images" element={<Images />} />
        <Route path="/serveurs" element={<Navigate to="/clusters" replace />} />
        <Route path="/serveurs/:id" element={<ServerPage />} />
        <Route path="/serveurs/:id/terminal" element={<RequireDeploy><ServerTerminal /></RequireDeploy>} />
        <Route path="/clusters" element={<Clusters />} />
        <Route path="/monde" element={<Navigate to="/" replace />} />
        <Route path="/historique" element={<HistoryPage />} />
        <Route path="/destructions/:id" element={<DestructionPage />} />
        <Route path="/deploiements" element={<DeploymentsPage />} />
        <Route path="/suppressions" element={<DestructionsPage />} />
        <Route path="/catalogue" element={<Navigate to="/nouveau" replace />} />
        <Route path="/catalogue/:id" element={<RequireDeploy><CatalogueArticle /></RequireDeploy>} />
        <Route path="/templates" element={<Templates />} />
        <Route path="/templates/nouveau" element={<RequireDeploy><TemplateEditor /></RequireDeploy>} />
        <Route path="/templates/suivis/:id" element={<TemplateRunPage />} />
        <Route path="/templates/:id" element={<TemplateEditor />} />
        <Route path="/templates/:id/deploy" element={<RequireDeploy><TemplateLaunch /></RequireDeploy>} />
        <Route path="/compte" element={<Account />} />
        <Route path="/utilisateurs" element={<RequireAdmin><UsersPage /></RequireAdmin>} />
        <Route path="/transfert" element={<RequireAdmin><Transfer /></RequireAdmin>} />
        <Route path="/transfert/suivi" element={<RequireAdmin><TransferFollow /></RequireAdmin>} />
        <Route path="/copies" element={<RequireAdmin><Copies /></RequireAdmin>} />
        <Route path="/mises-a-jour" element={<UpdatesPage />} />
        <Route path="/reglages" element={<SettingsPage />} />
      </Route>
    </Routes>
    </Suspense>
  );
}
