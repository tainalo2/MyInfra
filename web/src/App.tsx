import { FormEvent, Suspense, lazy, useEffect, useState, type ComponentType, type ReactNode } from "react";
import { Boxes } from "lucide-react";
import { Navigate, Route, Routes, useNavigate } from "react-router-dom";
import { api } from "./api";
import { text } from "./i18n";
import { useI18n } from "./lang";
import { AuthFrame, Button, ErrorText, Field, Icon, Input, LoadingLines, Shell } from "./ui";

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
const Git = page(() => import("./accounts"), "Git");
const Clusters = page(() => import("./clusters"), "Clusters");
const Account = page(() => import("./account"), "Account");
const Transfer = page(() => import("./transfer"), "Transfer");
const TransferFollow = page(() => import("./transfer"), "TransferFollow");
const Copies = page(() => import("./copies"), "Copies");
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
  return children;
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
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let cancel = false;
    api<{ needed: boolean }>("/api/setup")
      .then(async (setup) => {
        if (cancel) return;
        const choosing = path === "/api/setup";
        if (setup.needed && !choosing) navigate("/depart", { replace: true });
        if (!setup.needed && choosing) navigate("/", { replace: true });
        if (!setup.needed && !choosing) {
          await api("/api/session");
          if (!cancel) navigate("/", { replace: true });
        }
      })
      .catch(() => undefined);
    return () => {
      cancel = true;
    };
  }, [path, navigate]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await api(path, { method: "POST", body: JSON.stringify({ password }) });
      navigate("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  return (
    <AuthFrame>
      <h1 className="mb-2 flex items-center gap-3 text-2xl font-medium">
        <Icon icon={Boxes} />
        MyInfra
      </h1>
      <p className="mb-12 text-muted">{title}</p>
      <form onSubmit={onSubmit} className="flex flex-col gap-6">
        <Field label={text("auth.password")}>
          <Input type="password" value={password} autoFocus onChange={(e) => setPassword(e.target.value)} />
        </Field>
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div>
          <Button type="submit">{submit}</Button>
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
        <Route path="/nouveau" element={<NewApp />} />
        <Route path="/applications/:id" element={<ApplicationPage />} />
        <Route path="/applications/:id/terminal/:pod" element={<PodTerminal />} />
        <Route path="/cles" element={<Keys />} />
        <Route path="/secrets" element={<Secrets />} />
        <Route path="/secrets/:id" element={<SecretPage />} />
        <Route path="/providers" element={<Providers />} />
        <Route path="/dns" element={<Dns />} />
        <Route path="/git" element={<Git />} />
        <Route path="/serveurs" element={<Navigate to="/clusters" replace />} />
        <Route path="/serveurs/:id" element={<ServerPage />} />
        <Route path="/serveurs/:id/terminal" element={<ServerTerminal />} />
        <Route path="/clusters" element={<Clusters />} />
        <Route path="/monde" element={<Navigate to="/" replace />} />
        <Route path="/historique" element={<HistoryPage />} />
        <Route path="/destructions/:id" element={<DestructionPage />} />
        <Route path="/deploiements" element={<DeploymentsPage />} />
        <Route path="/suppressions" element={<DestructionsPage />} />
        <Route path="/catalogue" element={<Navigate to="/nouveau" replace />} />
        <Route path="/catalogue/:id" element={<CatalogueArticle />} />
        <Route path="/templates" element={<Templates />} />
        <Route path="/templates/nouveau" element={<TemplateEditor />} />
        <Route path="/templates/suivis/:id" element={<TemplateRunPage />} />
        <Route path="/templates/:id" element={<TemplateEditor />} />
        <Route path="/templates/:id/deploy" element={<TemplateLaunch />} />
        <Route path="/compte" element={<Account />} />
        <Route path="/transfert" element={<Transfer />} />
        <Route path="/transfert/suivi" element={<TransferFollow />} />
        <Route path="/copies" element={<Copies />} />
        <Route path="/reglages" element={<Navigate to="/cles" replace />} />
      </Route>
    </Routes>
    </Suspense>
  );
}
