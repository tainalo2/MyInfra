import { FormEvent, useCallback, useEffect, useState, type ReactNode } from "react";
import { DnsRecords } from "./dnsrecords";
import { Cloud, Container, GitBranch, Globe, Plus, Trash2, type LucideIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { api, type Secret, type Server } from "./api";
import { text } from "./i18n";
import { CredentialFields, emptyCredentials, registryOptions, type Credentials } from "./credentials";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { ProviderLogo } from "./logos";
import { ProviderGuide, hasGuide } from "./provider-guide";
import type { ProviderOption } from "./provider-select";
import { useSession } from "./session";
import { ActionsMenu, Button, EmptyList, ErrorText, Icon, Input, ItemList, ListActions, ListModeButton, LoadingCards, LoadingLines, PageTitle, MenuItem, Skeleton, TextButton, entryClass, focus, useListMode } from "./ui";

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
  intro,
  section,
  after,
}: {
  title: string;
  icon: LucideIcon;
  kind: "provider_token" | "dns_token" | "git_token" | "registry_token";
  options: ProviderOption[];
  empty: string;
  createLabel: string;
  selectLabel?: string;
  chooseFirst?: boolean;
  // intro says what the page holds, under its title.
  intro?: string;
  // section shows the list as a part of a larger page, under a heading.
  section?: { hint: string };
  // after is shown under the list, once no form is open.
  after?: ReactNode;
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
  const { canAdmin } = useSession();
  const [mode, toggleMode] = useListMode();
  const chosen = provider !== "";
  // A provider account says which servers it rents.
  const [rented, setRented] = useState<Server[]>([]);
  useEffect(() => {
    if (kind !== "provider_token") return;
    api<Server[]>("/api/servers").then(setRented).catch(() => undefined);
  }, [kind]);

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

  const heading = section ? (
    <div className="mb-3 flex flex-wrap items-start justify-between gap-x-6 gap-y-2">
      <div>
        <h2>{loaded ? `${title} · ${secrets.length}` : title}</h2>
        <p className="text-[13px] text-muted">{section.hint}</p>
      </div>
      {creating || editing ? (
        <TextButton type="button" onClick={close}>
          {text("common.cancel")}
        </TextButton>
      ) : canAdmin && loaded && secrets.length > 0 ? (
        <TextButton
          type="button"
          onClick={() => {
            setEditing(null);
            setCreating(true);
          }}
        >
          <Icon icon={Plus} />
          {createLabel}
        </TextButton>
      ) : null}
    </div>
  ) : null;

  return (
    <>
      {section ? heading : <PageTitle
        inTree
        title={title}
        action={
          creating || editing ? (
            <TextButton type="button" onClick={close}>
              {text("common.cancel")}
            </TextButton>
          ) : canAdmin && loaded && secrets.length > 0 ? (
            <ListActions
              mode={mode}
              onToggle={toggleMode}
              label={createLabel}
              onCreate={() => {
                setEditing(null);
                setCreating(true);
              }}
            />
          ) : loaded && secrets.length > 0 ? (
            <span className="hidden md:inline-flex">
              <ListModeButton mode={mode} onToggle={toggleMode} />
            </span>
          ) : null
        }
      />}
      {intro && !section ? <p className="mb-6 max-w-[720px] text-muted">{intro}</p> : null}
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {canAdmin && (creating || editing) ? (
      <form onSubmit={save} className="flex max-w-[720px] flex-col gap-8">
        <section className="flex flex-col gap-3">
          <h2>
            <span className="mr-2 text-muted">1</span>
            {selectLabel}
          </h2>
          {editing ? (
            <p className="flex items-center gap-2">
              <ProviderLogo id={provider} />
              {options.find((option) => option.value === provider)?.label ?? provider}
              <span className="text-muted">{editing.name}</span>
            </p>
          ) : (
            <ProviderTiles
              label={selectLabel}
              value={provider}
              options={options}
              onChange={(value) => {
                setProvider(value);
                setCreds(emptyCredentials);
              }}
            />
          )}
        </section>
        {chosen && hasGuide(kind, provider) ? (
          <section className="flex flex-col gap-2">
            <h2>
              <span className="mr-2 text-muted">2</span>
              {text("provider.makeKey", { name: options.find((option) => option.value === provider)?.label ?? provider })}
            </h2>
            <p className="text-[13px] text-muted">{text("provider.makeKeyHint")}</p>
            <div>
              <TextButton type="button" onClick={() => setGuideOpen(true)}>
                {text("ui.ouObtenirCesCles")}
              </TextButton>
            </div>
          </section>
        ) : null}
        {chosen ? (
          <>
            <section className="flex flex-col gap-4">
              <h2>
                <span className="mr-2 text-muted">{hasGuide(kind, provider) ? 3 : 2}</span>
                {text("provider.pasteKey")}
              </h2>
              <CredentialFields kind={kind} provider={provider} value={creds} onChange={setCreds} />
            </section>
            {editing ? null : (
              <section className="flex flex-col gap-4">
                <h2>
                  <span className="mr-2 text-muted">{hasGuide(kind, provider) ? 4 : 3}</span>
                  {text("field.name")}
                </h2>
                <Input value={name} onChange={(e) => setName(e.target.value)} aria-label={text("field.name")} required />
              </section>
            )}
            <div className="flex flex-wrap items-center gap-4">
              <Button type="submit" disabled={checking}>
                {checking ? <Spinner /> : <Icon icon={Plus} />}
                {checking ? text("ui.verificationDeLaConnexion") : text("common.save")}
              </Button>
              <span className="text-[13px] text-muted">{text("provider.keySafe")}</span>
            </div>
          </>
        ) : null}
      </form>
      ) : !loaded ? (
        <LoadingCards />
      ) : secrets.length === 0 ? (
        canAdmin && section ? (
          <div className="flex flex-col items-start gap-2 rounded-md border border-line bg-surface px-4 py-3">
            <p className="text-muted">{empty}</p>
            <TextButton type="button" onClick={() => setCreating(true)}>
              <Icon icon={Plus} />
              {createLabel}
            </TextButton>
          </div>
        ) : canAdmin ? (
          <EmptyList message={empty} label={createLabel} onCreate={() => setCreating(true)} />
        ) : (
          <p className="text-muted">{empty}</p>
        )
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
                  {kind === "provider_token" ? <RentedLine servers={rented.filter((server) => server.providerId === secret.id)} /> : null}
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
              {canAdmin ? (
                kind === "provider_token" ? (
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
                )
              ) : null}
            </span>
          </li>
        ))}
      </ItemList>
      )}
      {after && !creating && !editing && loaded && secrets.length > 0 ? <div className="mt-8">{after}</div> : null}
      {kind === "provider_token" && canAdmin && !creating && !editing && loaded && secrets.length > 0 ? (
        <section className="mt-8">
          <h2 className="mb-3">{text("provider.others")}</h2>
          <ul className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4">
            {options
              .filter((option) => !secrets.some((secret) => secret.provider === option.value))
              .map((option) => (
                <li key={option.value}>
                  <button
                    type="button"
                    onClick={() => {
                      setEditing(null);
                      setProvider(option.value);
                      setCreds(emptyCredentials);
                      setCreating(true);
                    }}
                    className={`flex w-full items-center gap-2 rounded-md border border-line bg-surface px-3 py-2 text-left ${focus}`}
                  >
                    <ProviderLogo id={option.logo ?? option.value} />
                    <span className="truncate">{option.label}</span>
                  </button>
                </li>
              ))}
          </ul>
          <p className="mt-4 text-[13px] text-muted">
            {text("provider.manualBefore")}{" "}
            <Link to="/clusters" className={`text-link ${focus}`}>
              {text("provider.manualLink")}
            </Link>
            {text("provider.manualAfter")}
          </p>
        </section>
      ) : null}
      {guideOpen && chosen ? <ProviderGuide kind={kind} provider={provider} onClose={() => setGuideOpen(false)} /> : null}
      {canAdmin && removing ? (
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
      intro={text("settings.providersHint")}
      icon={Cloud}
      kind="provider_token"
      options={providerOptions}
      empty={text("connect.noProvider")}
      createLabel={text("connect.newProvider")}
    />
  );
}

// ProviderTiles picks a provider among tiles: the first ones shown, the
// others one click away.
function ProviderTiles({ label, value, options, onChange }: { label: string; value: string; options: ProviderOption[]; onChange: (value: string) => void }) {
  const [all, setAll] = useState(false);
  const first = 8;
  const shown = all || options.length <= first + 1 ? options : options.slice(0, first).concat(options.slice(first).filter((option) => option.value === value));
  return (
    <div className="flex flex-col gap-2">
      <div role="radiogroup" aria-label={label} className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {shown.map((option) => {
          const on = option.value === value;
          return (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={on}
              onClick={() => onChange(option.value)}
              className={`flex min-w-0 items-center gap-2 rounded-md border bg-surface px-3 py-2 text-left ${on ? "border-ink shadow-[inset_0_0_0_1px_var(--color-ink)]" : "border-line"} ${focus}`}
            >
              <ProviderLogo id={option.logo ?? option.value} />
              <span className="truncate">{option.label}</span>
            </button>
          );
        })}
      </div>
      {options.length > first + 1 && !all ? (
        <div>
          <TextButton type="button" onClick={() => setAll(true)}>
            {text("provider.seeOthers", { count: options.length - first })}
          </TextButton>
        </div>
      ) : null}
    </div>
  );
}

function RentedLine({ servers }: { servers: Server[] }) {
  if (servers.length === 0) return <span className="mt-1 block text-[13px] text-muted">{text("provider.noServer")}</span>;
  const names = servers.map((server) => (server.clusterName ? `${server.name} (${server.clusterName})` : server.name)).join(", ");
  return (
    <span className="mt-1 block text-[13px] text-muted">
      {servers.length === 1 ? text("server.count1") : text("server.countN", { count: servers.length })} · {names}
    </span>
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
      title={text("nav.dns")}
      intro={text("dns.intro")}
      after={<DnsRecords />}
      icon={Globe}
      kind="dns_token"
      options={dnsOptions}
      empty={text("connect.noDns")}
      createLabel={text("connect.newDns")}
      chooseFirst
    />
  );
}

const gitHosts = [
  { value: "github", label: "GitHub" },
  { value: "gitlab", label: "GitLab" },
];

// Git waits for the build of applications from their repository: it is not
// reachable from the screen until then.
export function Git() {
  return (
    <TokenPage
      title={text("git.title")}
      icon={GitBranch}
      kind="git_token"
      options={gitHosts}
      empty={text("connect.noGit")}
      createLabel={text("connect.newGit")}
      selectLabel={text("connect.host")}
    />
  );
}

export function Registries({ section }: { section?: { hint: string } } = {}) {
  return (
    <TokenPage
      section={section}
      title={section ? text("registry.sectionTitle") : text("registry.title")}
      icon={Container}
      kind="registry_token"
      options={registryOptions}
      empty={text("registry.empty")}
      createLabel={text("registry.new")}
      selectLabel={text("registry.which")}
    />
  );
}
