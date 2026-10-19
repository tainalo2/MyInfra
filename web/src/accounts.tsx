import { FormEvent, useCallback, useEffect, useRef, useState } from "react";
import { Cloud, GitBranch, Globe, MoreHorizontal, Plus, Trash2, type LucideIcon } from "lucide-react";
import { api, type Secret } from "./api";
import { text } from "./i18n";
import { ProviderLogo } from "./logos";
import { ProviderGuide } from "./provider-guide";
import { ProviderSelect } from "./provider-select";
import { Button, EmptyList, ErrorText, Field, Icon, Input, ItemList, ListActions, LoadingCards, LoadingLines, PageTitle, Skeleton, TextButton, entryClass, useListMode } from "./ui";

export const providerOptions = [
  { value: "hetzner", label: "Hetzner" },
  { value: "scaleway", label: "Scaleway" },
  { value: "ovh", label: "OVH" },
  { value: "digitalocean", label: "DigitalOcean" },
  { value: "infomaniak", label: "Infomaniak" },
  { value: "upcloud", label: "UpCloud" },
  { value: "akamai", label: "Akamai" },
  { value: "vultr", label: "Vultr" },
  { value: "exoscale", label: "Exoscale" },
  { value: "gcore", label: "Gcore" },
  { value: "kamatera", label: "Kamatera" },
  { value: "ionos", label: "IONOS" },
  { value: "openstack", label: "OpenStack" },
  { value: "proxmox", label: "Proxmox" },
];

type LinkState = { state: "checking" | "up" | "down"; error?: string };

function ConnectionState({ link }: { link?: LinkState }) {
  const state = link?.state ?? "checking";
  if (state === "up") {
    return (
      <span className="inline-flex items-center gap-2 text-ok" role="status">
        <span className="size-2 rounded-full bg-ok" aria-hidden />
        {text("ui.connecte")}
      </span>
    );
  }
  if (state === "down") {
    return (
      <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1" role="status">
        <span className="inline-flex items-center gap-2 text-bad">
          <span className="size-2 rounded-full bg-bad" aria-hidden />
          {text("ui.horsLigne")}
        </span>
        {link?.error ? <span className="text-faint">{link.error}</span> : null}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-2" role="status" aria-label={text("provider.checking")}>
      <Skeleton className="size-2 rounded-full" />
      <Skeleton className="h-4 w-20" />
    </span>
  );
}

function ProviderMenu({ onChangeKey, onDelete }: { onChangeKey: () => void; onDelete: () => void }) {
  const [open, setOpen] = useState(false);
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

  return (
    <div className="relative" ref={root}>
      <button
        type="button"
        aria-label={text("provider.actions")}
        aria-haspopup="menu"
        aria-expanded={open}
        className="text-ink focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-2 focus-visible:outline-focus"
        onClick={() => setOpen((value) => !value)}
      >
        <Icon icon={MoreHorizontal} />
      </button>
      {open ? (
        <div role="menu" className="absolute right-0 z-20 mt-2 w-56 rounded-md border border-line bg-surface py-1">
          <button
            type="button"
            role="menuitem"
            className="block w-full px-3 py-2 text-left hover:bg-status"
            onClick={() => {
              setOpen(false);
              onChangeKey();
            }}
          >
            {text("ui.changerLaCle")}
          </button>
          <button
            type="button"
            role="menuitem"
            className="block w-full px-3 py-2 text-left text-bad hover:bg-status"
            onClick={() => {
              setOpen(false);
              onDelete();
            }}
          >
            {text("ui.supprimerLeProvider")}
          </button>
        </div>
      ) : null}
    </div>
  );
}

export const dnsOptions = [
  { value: "cloudflare", label: "Cloudflare" },
  { value: "ovh", label: "OVH" },
];

function TokenPage({
  title,
  icon,
  kind,
  options,
  empty,
  createLabel,
  selectLabel = text("ui.provider"),
  chooseFirst = false,
}: {
  title: string;
  icon: LucideIcon;
  kind: "provider_token" | "dns_token" | "git_token";
  options: { value: string; label: string }[];
  empty: string;
  createLabel: string;
  selectLabel?: string;
  chooseFirst?: boolean;
}) {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState(chooseFirst ? "" : options[0].value);
  const [token, setToken] = useState("");
  const [keyId, setKeyId] = useState("");
  const [projectId, setProjectId] = useState("");
  const [editing, setEditing] = useState<Secret | null>(null);
  const [applicationKey, setApplicationKey] = useState("");
  const [applicationSecret, setApplicationSecret] = useState("");
  const [consumerKey, setConsumerKey] = useState("");
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [creating, setCreating] = useState(false);
  const [checking, setChecking] = useState(false);
  const [links, setLinks] = useState<Record<string, LinkState>>({});
  const [guideOpen, setGuideOpen] = useState(false);
  const [mode, toggleMode] = useListMode();
  const ovh = provider === "ovh";
  const chosen = provider !== "";

  async function reload() {
    const rows = await api<Secret[]>("/api/secrets");
    setSecrets(rows.filter((secret) => secret.kind === kind));
  }

  const probe = useCallback(async (id: string) => {
    setLinks((current) => ({ ...current, [id]: { state: "checking" } }));
    try {
      const result = await api<{ ok: boolean; error?: string }>(`/api/secrets/${id}/check`, { method: "POST" });
      setLinks((current) => ({ ...current, [id]: result.ok ? { state: "up" } : { state: "down", error: result.error } }));
    } catch (err) {
      setLinks((current) => ({
        ...current,
        [id]: { state: "down", error: err instanceof Error ? err.message : "La connexion au provider a échoué." },
      }));
    }
  }, []);

  useEffect(() => {
    reload()
      .catch((err) => setError(err.message))
      .finally(() => setLoaded(true));
  }, [kind]);

  const watchedIds = kind === "git_token" ? "" : secrets.map((secret) => secret.id).join(" ");

  useEffect(() => {
    if (!watchedIds) return;
    for (const id of watchedIds.split(" ")) {
      void probe(id);
    }
  }, [watchedIds, probe]);

  function close() {
    setCreating(false);
    setName("");
    setToken("");
    setKeyId("");
    setProjectId("");
    setEditing(null);
    setApplicationKey("");
    setApplicationSecret("");
    setConsumerKey("");
    setProvider(chooseFirst ? "" : options[0].value);
    setGuideOpen(false);
    setError("");
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    setError("");
    setChecking(true);
    try {
      const body = { name, kind, provider, token, keyId, projectId, applicationKey, applicationSecret, consumerKey };
      if (editing) {
        await api(`/api/secrets/${editing.id}`, { method: "PUT", body: JSON.stringify(body) });
      } else {
        await api("/api/secrets/tokens", { method: "POST", body: JSON.stringify(body) });
      }
      setName("");
      setToken("");
      setKeyId("");
      setEditing(null);
      setProjectId("");
      setApplicationKey("");
      setApplicationSecret("");
      setConsumerKey("");
      setCreating(false);
      setProvider(chooseFirst ? "" : options[0].value);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setChecking(false);
    }
  }

  async function remove(id: string) {
    setError("");
    try {
      await api(`/api/secrets/${id}`, { method: "DELETE" });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  return (
    <>
      <PageTitle
        title={title}
        icon={icon}
        action={
          creating || editing ? (
            <TextButton type="button" onClick={close}>
              {text("common.cancel")}
            </TextButton>
          ) : loaded && secrets.length > 0 ? (
            <ListActions
              mode={mode}
              onToggle={toggleMode}
              label={createLabel}
              onCreate={() => {
                setEditing(null);
                setCreating(true);
              }}
            />
          ) : null
        }
      />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {creating || editing ? (
      <form onSubmit={save} className="flex max-w-[720px] flex-col gap-6">
        {editing ? (
          <p className="flex items-center gap-2">
            <ProviderLogo id={provider} />
            {options.find((option) => option.value === provider)?.label ?? provider}
            <span className="text-muted">{editing.name}</span>
          </p>
        ) : (
        <ProviderSelect
          label={selectLabel}
          value={provider}
          options={options}
          onChange={(value) => {
            setProvider(value);
            setToken("");
            setKeyId("");
            setProjectId("");
            setApplicationKey("");
            setApplicationSecret("");
            setConsumerKey("");
          }}
        />
        )}
        {chosen ? (
          <button
            type="button"
            className="self-start underline focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-2 focus-visible:outline-focus"
            onClick={() => setGuideOpen(true)}
          >
            {text("ui.ouObtenirCesCles")}
          </button>
        ) : null}
        {chosen ? (
          <>
            {editing ? null : (
              <Field label={text("field.name")}>
                <Input value={name} onChange={(e) => setName(e.target.value)} required />
              </Field>
            )}
            {ovh ? (
              <>
                <Field label={text("provider.appKey")}>
                  <Input value={applicationKey} onChange={(e) => setApplicationKey(e.target.value)} required />
                </Field>
                <Field label={text("provider.appSecret")}>
                  <Input type="password" value={applicationSecret} onChange={(e) => setApplicationSecret(e.target.value)} required />
                </Field>
                <Field label="Consumer key">
                  <Input type="password" value={consumerKey} onChange={(e) => setConsumerKey(e.target.value)} required />
                </Field>
                <p className="text-faint">
                  {kind === "dns_token"
                    ? text("ui.endpointOvhEuLesZonesDns")
                    : text("ui.endpointOvhEuPublicCloudA")}
                </p>
              </>
            ) : provider === "scaleway" ? (
              <>
                <Field label="Access key">
                  <Input value={keyId} onChange={(e) => setKeyId(e.target.value)} required autoComplete="off" spellCheck={false} />
                </Field>
                <Field label={text("provider.secretKey")}>
                  <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} required />
                </Field>
                <Field label={text("provider.project")}>
                  <Input value={projectId} onChange={(e) => setProjectId(e.target.value)} required />
                </Field>
                <p className="text-faint">{text("ui.facturationALHeureLeMois")}</p>
              </>
            ) : provider === "proxmox" ? (
              <>
                <Field label={text("ui.adresse")}>
                  <Input value={applicationKey} onChange={(e) => setApplicationKey(e.target.value)} required autoComplete="off" spellCheck={false} placeholder="https://pve.exemple:8006" />
                </Field>
                <Field label={text("ui.jetonApi")}>
                  <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} required />
                </Field>
              </>
            ) : provider === "openstack" ? (
              <>
                <Field label={text("ui.adresseKeystone")}>
                  <Input value={token} onChange={(e) => setToken(e.target.value)} required autoComplete="off" spellCheck={false} />
                </Field>
                <Field label={text("provider.id")}>
                  <Input value={applicationKey} onChange={(e) => setApplicationKey(e.target.value)} required autoComplete="off" spellCheck={false} />
                </Field>
                <Field label={text("crumb.secret")}>
                  <Input type="password" value={applicationSecret} onChange={(e) => setApplicationSecret(e.target.value)} required />
                </Field>
                <Field label={text("provider.project")}>
                  <Input value={projectId} onChange={(e) => setProjectId(e.target.value)} autoComplete="off" spellCheck={false} />
                </Field>
              </>
            ) : provider === "infomaniak" || provider === "exoscale" || provider === "kamatera" ? (
              <>
                <Field label={provider === "exoscale" ? "Clé API" : provider === "kamatera" ? "Client ID" : text("provider.id")}>
                  <Input value={applicationKey} onChange={(e) => setApplicationKey(e.target.value)} required autoComplete="off" spellCheck={false} />
                </Field>
                <Field label={provider === "infomaniak" ? text("crumb.secret") : "Secret API"}>
                  <Input type="password" value={applicationSecret} onChange={(e) => setApplicationSecret(e.target.value)} required />
                </Field>
                {provider === "infomaniak" ? <p className="text-faint">{text("ui.facturationALHeureEntameeLe")}</p> : null}
              </>
            ) : (
              <Field label={provider === "hetzner" || provider === "cloudflare" || provider === "digitalocean" || provider === "vultr" || provider === "akamai" || provider === "upcloud" || provider === "gcore" || provider === "ionos" ? text("ui.jetonApi") : "Jeton"}>
                <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} required />
              </Field>
            )}
            {provider === "hetzner" ? <p className="text-faint">{text("ui.facturationALHeurePlafonneeAu2")}</p> : null}
            {provider === "digitalocean" ? (
              <p className="text-faint">{text("ui.facturationALaSecondePlafonneeAu")}</p>
            ) : null}
            {provider === "upcloud" ? <p className="text-faint">{text("ui.facturationALHeureLeMois")}</p> : null}
            {provider === "vultr" ? <p className="text-faint">{text("ui.facturationALHeurePlafonneeAu")}</p> : null}
            {provider === "akamai" ? <p className="text-faint">{text("ui.akamaiConnectedCloudLinodeFacturationA")}</p> : null}
            {provider === "exoscale" ? <p className="text-faint">{text("ui.facturationALHeureLeDisque")}</p> : null}
            {provider === "gcore" ? <p className="text-faint">{text("ui.facturationALHeureMyinfraUtilise")}</p> : null}
            {provider === "kamatera" ? <p className="text-faint">{text("ui.facturationAuMoisTraficInclusPrix")}</p> : null}
            {provider === "ionos" ? <p className="text-faint">{text("ui.facturationALaMinuteUneMachine")}</p> : null}
            <div>
              <Button type="submit" disabled={checking}>
                <Icon icon={Plus} />
                {checking ? text("ui.verificationDeLaConnexion") : text("common.save")}
              </Button>
            </div>
          </>
        ) : null}
      </form>
      ) : !loaded ? (
        <LoadingCards />
      ) : secrets.length === 0 ? (
        <EmptyList message={empty} label={createLabel} onCreate={() => setCreating(true)} />
      ) : (
      <ItemList mode={mode}>
        {secrets.map((secret) => (
          <li key={secret.id} className={entryClass(mode, "stack")}>
            <span className="flex items-start justify-between gap-4">
              <span className="flex min-w-0 items-start gap-3">
                <Icon icon={icon} className="mt-1 text-muted" />
                <span className="min-w-0">
                  {secret.name}
                  <span className="mt-1 flex items-center gap-2 text-muted">
                    <ProviderLogo id={secret.provider ?? ""} />
                    {options.find((option) => option.value === secret.provider)?.label ?? secret.provider}
                    {kind === "provider_token" ? null : <span>····{secret.hint}</span>}
                  </span>
                  {kind === "provider_token" && secret.keyId ? (
                    <span className="mt-1 block break-all font-mono text-muted">{secret.keyId}</span>
                  ) : null}
                  {kind === "git_token" ? null : (
                    <span className="mt-2 flex flex-col gap-2">
                      <ConnectionState link={links[secret.id]} />
                      {kind === "dns_token" && links[secret.id]?.state === "up" ? <ZoneList id={secret.id} /> : null}
                    </span>
                  )}
                </span>
              </span>
              {kind === "provider_token" ? (
                <ProviderMenu
                  onChangeKey={() => {
                    setCreating(false);
                    setEditing(secret);
                    setProvider(secret.provider ?? "");
                    setName(secret.name);
                    setToken("");
                    setKeyId("");
                    setProjectId("");
                    setApplicationKey("");
                    setApplicationSecret("");
                    setConsumerKey("");
                    setError("");
                  }}
                  onDelete={() => remove(secret.id)}
                />
              ) : (
                <TextButton type="button" onClick={() => remove(secret.id)}>
                  <Icon icon={Trash2} />
                  {text("common.delete")}
                </TextButton>
              )}
            </span>
          </li>
        ))}
      </ItemList>
      )}
      {guideOpen && chosen ? <ProviderGuide kind={kind} provider={provider} onClose={() => setGuideOpen(false)} /> : null}
    </>
  );
}

export function Providers() {
  return (
    <TokenPage
      title={text("provider.title")}
      icon={Cloud}
      kind="provider_token"
      options={providerOptions}
      empty="Aucun provider."
      createLabel="Nouveau provider"
    />
  );
}

function ZoneList({ id }: { id: string }) {
  const [zones, setZones] = useState<string[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api<string[]>(`/api/secrets/${id}/zones`)
      .then(setZones)
      .catch((err) => setError(err.message));
  }, [id]);

  if (error) return <span className="text-bad">{error}</span>;
  if (!zones) return <LoadingLines lines={3} label={text("dns.reading")} />;
  if (zones.length === 0) return <span className="text-muted">{text("ui.aucunDomaineSurCeCompte")}</span>;
  return (
    <ul className="flex flex-col">
      {zones.map((zone) => (
        <li key={zone} className="font-mono text-[13px]">
          {zone}
        </li>
      ))}
    </ul>
  );
}

export function Dns() {
  return (
    <TokenPage
      title={text("dns.title")}
      icon={Globe}
      kind="dns_token"
      options={dnsOptions}
      empty="Aucun compte DNS."
      createLabel="Nouveau compte DNS"
      chooseFirst
    />
  );
}

const gitHosts = [
  { value: "github", label: "GitHub" },
  { value: "gitlab", label: "GitLab" },
];

export function Git() {
  return (
    <TokenPage
      title={text("git.title")}
      icon={GitBranch}
      kind="git_token"
      options={gitHosts}
      empty="Aucun compte Git."
      createLabel="Nouveau compte Git"
      selectLabel="Hôte"
    />
  );
}
