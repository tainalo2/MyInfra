import { FormEvent, useCallback, useEffect, useState } from "react";
import { Cloud, Container, GitBranch, Globe, Plus, Trash2, type LucideIcon } from "lucide-react";
import { api, type Secret } from "./api";
import { text } from "./i18n";
import { CredentialFields, emptyCredentials, registryOptions, type Credentials } from "./credentials";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { ProviderLogo } from "./logos";
import { ProviderGuide, hasGuide } from "./provider-guide";
import { ProviderSelect, type ProviderOption } from "./provider-select";
import { ActionsMenu, Button, EmptyList, ErrorText, Field, Icon, Input, ItemList, ListActions, LoadingCards, LoadingLines, PageTitle, MenuItem, Skeleton, TextButton, entryClass, focus, useListMode } from "./ui";

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
  return (
    <ActionsMenu label={text("provider.actions")}>
      <MenuItem onClick={onChangeKey}>{text("ui.changerLaCle")}</MenuItem>
      <MenuItem tone="danger" onClick={onDelete}>
        {text("ui.supprimerLeProvider")}
      </MenuItem>
    </ActionsMenu>
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
  kind: "provider_token" | "dns_token" | "git_token" | "registry_token";
  options: ProviderOption[];
  empty: string;
  createLabel: string;
  selectLabel?: string;
  chooseFirst?: boolean;
}) {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState(chooseFirst ? "" : options[0].value);
  const [creds, setCreds] = useState<Credentials>(emptyCredentials);
  const [editing, setEditing] = useState<Secret | null>(null);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [creating, setCreating] = useState(false);
  const [checking, setChecking] = useState(false);
  const [links, setLinks] = useState<Record<string, LinkState>>({});
  const [guideOpen, setGuideOpen] = useState(false);
  const [removing, setRemoving] = useState<Secret | null>(null);
  const toast = useToast();
  const [mode, toggleMode] = useListMode();
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
        [id]: { state: "down", error: err instanceof Error ? err.message : text("provider.linkFailed") },
      }));
    }
  }, []);

  useEffect(() => {
    reload()
      .catch((err) => setError(err.message))
      .finally(() => setLoaded(true));
  }, [kind]);

  // A Git or registry login is checked once, when it is saved.
  const watchedIds = kind === "git_token" || kind === "registry_token" ? "" : secrets.map((secret) => secret.id).join(" ");

  useEffect(() => {
    if (!watchedIds) return;
    for (const id of watchedIds.split(" ")) {
      void probe(id);
    }
  }, [watchedIds, probe]);

  function close() {
    setCreating(false);
    setName("");
    setCreds(emptyCredentials);
    setEditing(null);
    setProvider(chooseFirst ? "" : options[0].value);
    setGuideOpen(false);
    setError("");
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    setError("");
    setChecking(true);
    try {
      const body = { name, kind, provider, ...creds };
      if (editing) {
        await api(`/api/secrets/${editing.id}`, { method: "PUT", body: JSON.stringify(body) });
      } else {
        await api("/api/secrets/tokens", { method: "POST", body: JSON.stringify(body) });
      }
      setName("");
      setCreds(emptyCredentials);
      setEditing(null);
      setCreating(false);
      setProvider(chooseFirst ? "" : options[0].value);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setChecking(false);
    }
  }

  async function remove(secret: Secret) {
    await api(`/api/secrets/${secret.id}`, { method: "DELETE" });
    setSecrets((current) => current.filter((item) => item.id !== secret.id));
    setRemoving(null);
    toast(text("account.removed", { name: secret.name }));
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
            setCreds(emptyCredentials);
          }}
        />
        )}
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
            {editing ? null : (
              <Field label={text("field.name")}>
                <Input value={name} onChange={(e) => setName(e.target.value)} required />
              </Field>
            )}
            <CredentialFields kind={kind} provider={provider} value={creds} onChange={setCreds} />
            <div>
              <Button type="submit" disabled={checking}>
                {checking ? <Spinner /> : <Icon icon={Plus} />}
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
                    <ProviderLogo id={options.find((option) => option.value === secret.provider)?.logo ?? secret.provider ?? ""} />
                    {options.find((option) => option.value === secret.provider)?.label ?? secret.provider}
                    {kind === "provider_token" ? null : <span>····{secret.hint}</span>}
                  </span>
                  {(kind === "provider_token" || kind === "registry_token") && secret.keyId ? (
                    <span className="mt-1 block break-all font-mono text-muted">{secret.keyId}</span>
                  ) : null}
                  {kind === "git_token" || kind === "registry_token" ? null : (
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
                    setCreds(emptyCredentials);
                    setError("");
                  }}
                  onDelete={() => setRemoving(secret)}
                />
              ) : (
                <TextButton type="button" tone="danger" onClick={() => setRemoving(secret)}>
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
      {removing ? (
        <ConfirmDialog
          title={text("server.removeNamed", { name: removing.name })}
          confirmLabel={text("common.delete")}
          danger
          onConfirm={() => remove(removing)}
          onClose={() => setRemoving(null)}
        >
          {text("account.deleteHint")}
        </ConfirmDialog>
      ) : null}
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

export function Registries() {
  return (
    <TokenPage
      title={text("registry.title")}
      icon={Container}
      kind="registry_token"
      options={registryOptions}
      empty={text("registry.empty")}
      createLabel={text("registry.new")}
      selectLabel={text("registry.which")}
    />
  );
}
