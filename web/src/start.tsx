import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, type Secret } from "./api";
import { dnsOptions, providerOptions } from "./accounts";
import { CredentialFields, emptyCredentials, type Credentials } from "./credentials";
import { Spinner } from "./kit";
import { ProviderGuide, hasGuide } from "./provider-guide";
import { ProviderSelect } from "./provider-select";
import { ToolInstall } from "./tools";
import { AuthFrame, Button, ErrorText, Field, Input, LoadingLines, TextArea, TextButton, focus } from "./ui";
import { text } from "./i18n";

const steps = ["start.tools", "start.key", "ui.provider", "start.dns"] as const;

type HostTools = { ansible: boolean; tofu: boolean; tofuEnabled: boolean };

export function Start() {
  const navigate = useNavigate();
  const [ready, setReady] = useState(false);
  const [step, setStep] = useState(0);
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [tools, setTools] = useState<HostTools | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancel = false;
    (async () => {
      const setup = await api<{ needed: boolean }>("/api/setup");
      if (cancel) return;
      if (setup.needed) {
        navigate("/depart", { replace: true });
        return;
      }
      try {
        const session = await api<{ start?: boolean }>("/api/session");
        if (cancel) return;
        if (!session.start) {
          navigate("/", { replace: true });
          return;
        }
      } catch {
        if (!cancel) navigate("/entree", { replace: true });
        return;
      }
      const [rows, host] = await Promise.all([api<Secret[]>("/api/secrets"), api<HostTools>("/api/tools")]);
      if (cancel) return;
      setSecrets(rows);
      setTools(host);
      setReady(true);
    })().catch(() => {
      if (!cancel) setError(text("ui.leDepartNAPasPu"));
    });
    return () => {
      cancel = true;
    };
  }, [navigate]);

  async function finish() {
    setError("");
    try {
      await api("/api/onboarding", { method: "POST" });
      navigate("/", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  function next() {
    setError("");
    if (step >= steps.length - 1) {
      void finish();
      return;
    }
    setStep((current) => current + 1);
  }

  function saved(secret: Secret) {
    setSecrets((current) => [...current, secret]);
    next();
  }

  return (
    <AuthFrame>
      <ol className="mb-4 flex flex-wrap gap-x-4 gap-y-1 text-[13px]" aria-label={text("start.progress", { step: step + 1, count: steps.length })}>
        {steps.map((label, index) => (
          <li key={label} className={index === step ? "text-ink" : index < step ? "text-ok" : "text-faint"} aria-current={index === step ? "step" : undefined}>
            {index + 1}. {text(label)}
          </li>
        ))}
      </ol>
      <h1 className="mb-12 text-2xl font-medium">{text(steps[step])}</h1>
      {!ready ? (
        <LoadingLines lines={3} />
      ) : step === 0 ? (
        <ToolsStep tools={tools} onTools={setTools} onNext={next} />
      ) : step === 1 ? (
        <KeyStep existing={secrets.find((secret) => secret.kind === "ssh_key")} onSaved={saved} onSkip={next} />
      ) : step === 2 ? (
        <AccountStep
          key="provider"
          kind="provider_token"
          options={providerOptions}
          existing={secrets.find((secret) => secret.kind === "provider_token")}
          onSaved={saved}
          onSkip={next}
        />
      ) : (
        <AccountStep
          key="dns"
          kind="dns_token"
          options={dnsOptions}
          chooseFirst
          existing={secrets.find((secret) => secret.kind === "dns_token")}
          onSaved={saved}
          onSkip={next}
        />
      )}
      {error ? (
        <div className="mt-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {step > 0 ? (
        <div className="mt-8">
          <TextButton type="button" onClick={() => setStep((current) => current - 1)}>
            {text("ui.retour")}
          </TextButton>
        </div>
      ) : null}
    </AuthFrame>
  );
}

function ToolsStep({ tools, onTools, onNext }: { tools: HostTools | null; onTools: (tools: HostTools) => void; onNext: () => void }) {
  const missingAnsible = !tools?.ansible;
  const missingTofu = Boolean(tools?.tofuEnabled && !tools.tofu);
  const missing = missingAnsible || missingTofu;

  return (
    <div className="flex flex-col items-start gap-6">
      <p className="text-muted">
        {missing
          ? text("ui.installezCeQuiManquePourDeployer")
          : text("ui.lesOutilsNecessairesSontDejaInstalles")}
      </p>
      {tools?.ansible ? <p>{text("ui.ansibleEstInstalle")}</p> : null}
      {tools?.tofuEnabled && tools.tofu ? <p>{text("ui.opentofuEstInstalle")}</p> : null}
      <ToolInstall ansible tofu onChange={onTools} />
      <Button type="button" onClick={onNext}>
        {missing ? text("ui.passer") : text("auth.continue")}
      </Button>
    </div>
  );
}

function KeyStep({ existing, onSaved, onSkip }: { existing?: Secret; onSaved: (secret: Secret) => void; onSkip: () => void }) {
  const [name, setName] = useState("");
  const [publicKey, setPublicKey] = useState("");
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setBusy(true);
    try {
      const secret = importing
        ? await api<Secret>("/api/secrets/ssh-keys/import", { method: "POST", body: JSON.stringify({ name, publicKey }) })
        : await api<Secret>("/api/secrets/ssh-keys", { method: "POST", body: JSON.stringify({ name }) });
      onSaved(secret);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  if (existing) {
    return (
      <div className="flex flex-col items-start gap-6">
        <p className="text-muted">La clé {existing.name} est déjà enregistrée.</p>
        <Button type="button" onClick={onSkip}>
          {text("auth.continue")}
        </Button>
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-6">
      <p className="text-muted">{text("ui.cetteCleSertAOuvrirVos")}</p>
      <Field label={text("field.name")}>
        <Input value={name} onChange={(e) => setName(e.target.value)} required autoFocus />
      </Field>
      {importing ? (
        <Field label={text("field.publicKey")}>
          <TextArea value={publicKey} onChange={(e) => setPublicKey(e.target.value)} required />
        </Field>
      ) : null}
      {error ? <ErrorText>{error}</ErrorText> : null}
      <div className="flex items-center gap-6">
        <Button type="submit" disabled={busy}>
          {importing ? text("cluster.import") : text("ui.creerLaCle")}
        </Button>
        <TextButton type="button" onClick={() => setImporting((current) => !current)}>
          {importing ? text("server.createKey") : text("ui.importerUneCle")}
        </TextButton>
        <TextButton type="button" onClick={onSkip}>
          {text("ui.passer")}
        </TextButton>
      </div>
    </form>
  );
}

function AccountStep({
  kind,
  options,
  chooseFirst,
  existing,
  onSaved,
  onSkip,
}: {
  kind: "provider_token" | "dns_token";
  options: { value: string; label: string }[];
  chooseFirst?: boolean;
  existing?: Secret;
  onSaved: (secret: Secret) => void;
  onSkip: () => void;
}) {
  const [name, setName] = useState("");
  const [provider, setProvider] = useState(chooseFirst ? "" : options[0].value);
  const [creds, setCreds] = useState<Credentials>(emptyCredentials);
  const [guideOpen, setGuideOpen] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const chosen = provider !== "";

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setBusy(true);
    try {
      const secret = await api<Secret>("/api/secrets/tokens", {
        method: "POST",
        body: JSON.stringify({ name, kind, provider, ...creds }),
      });
      onSaved(secret);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  if (existing) {
    return (
      <div className="flex flex-col items-start gap-6">
        <p className="text-muted">{text("start.already", { name: existing.name })}</p>
        <Button type="button" onClick={onSkip}>
          {text("auth.continue")}
        </Button>
      </div>
    );
  }

  return (
    <>
      <form onSubmit={submit} className="flex flex-col gap-6">
        <ProviderSelect
          label={kind === "dns_token" ? text("start.dnsAccount") : text("ui.provider")}
          value={provider}
          options={options}
          onChange={(value) => {
            setProvider(value);
            setCreds(emptyCredentials);
          }}
        />
        {chosen && hasGuide(kind, provider) ? (
          <button
            type="button"
            className={`self-start underline ${focus}`}
            onClick={() => setGuideOpen(true)}
          >
            {text("ui.ouObtenirCesCles")}
          </button>
        ) : null}
        {chosen ? (
          <>
            <Field label={text("field.name")}>
              <Input value={name} onChange={(e) => setName(e.target.value)} required />
            </Field>
            <CredentialFields kind={kind} provider={provider} value={creds} onChange={setCreds} />
          </>
        ) : null}
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div className="flex items-center gap-6">
          {chosen ? (
            <Button type="submit" disabled={busy}>
              {busy ? <Spinner /> : null}
              {busy ? text("ui.verificationDeLaConnexion") : text("common.save")}
            </Button>
          ) : null}
          <TextButton type="button" onClick={onSkip}>
            {text("ui.passer")}
          </TextButton>
        </div>
      </form>
      {guideOpen && chosen ? <ProviderGuide kind={kind} provider={provider} onClose={() => setGuideOpen(false)} /> : null}
    </>
  );
}
