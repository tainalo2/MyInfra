import { FormEvent, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Plus, Shield, Trash2 } from "lucide-react";
import { api, type AppSecret, type Application } from "./api";
import { text } from "./i18n";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { useSession } from "./session";
import { Button, EmptyList, ErrorText, Field, Icon, Input, ItemList, ListActions, ListModeButton, LoadingCards, PageTitle, SectionTitle, TextArea, TextButton, entryClass, useListMode, useTrail } from "./ui";

function parseEntries(source: string) {
  const entries: Record<string, string> = {};
  for (const raw of source.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const cut = line.indexOf("=");
    if (cut <= 0) throw new Error(text("secret.line"));
    const key = line.slice(0, cut).trim();
    const value = line.slice(cut + 1);
    if (!value) throw new Error(text("ui.uneValeurVideNEstPas"));
    entries[key] = value;
  }
  return entries;
}

export function Secrets() {
  const navigate = useNavigate();
  const { canAdmin } = useSession();
  const [secrets, setSecrets] = useState<AppSecret[]>([]);
  const [name, setName] = useState("");
  const [entries, setEntries] = useState("");
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);
  const [creating, setCreating] = useState(false);
  const [mode, toggleMode] = useListMode();

  async function reload() {
    setSecrets(await api<AppSecret[]>("/api/app-secrets"));
  }

  useEffect(() => {
    reload()
      .catch((err) => setError(err instanceof Error ? err.message : text("common.error")))
      .finally(() => setReady(true));
  }, []);

  function close() {
    setCreating(false);
    setName("");
    setEntries("");
    setError("");
  }

  async function create(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const body = parseEntries(entries);
      if (Object.keys(body).length === 0) throw new Error(text("ui.ajoutezAuMoinsUneCle"));
      const created = await api<{ id: string }>("/api/app-secrets", {
        method: "POST",
        body: JSON.stringify({ name, entries: body }),
      });
      navigate(`/secrets/${created.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  return (
    <>
      <PageTitle
        title={text("nav.secrets")}
        icon={Shield}
        action={
          creating ? (
            <TextButton type="button" onClick={close}>
              {text("common.cancel")}
            </TextButton>
          ) : canAdmin && ready && secrets.length > 0 ? (
            <ListActions mode={mode} onToggle={toggleMode} label={text("secret.new")} onCreate={() => setCreating(true)} />
          ) : ready && secrets.length > 0 ? (
            <span className="hidden md:inline-flex">
              <ListModeButton mode={mode} onToggle={toggleMode} />
            </span>
          ) : null
        }
      />
      <p className="mb-8 max-w-[720px] text-muted">
        {text("secret.intro")}
      </p>
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {canAdmin && creating ? (
        <form onSubmit={create} className="flex max-w-[720px] flex-col gap-6">
          <Field label={text("field.name")}>
            <Input value={name} onChange={(e) => setName(e.target.value)} required />
          </Field>
          <Field label={text("field.keys")}>
            <TextArea value={entries} onChange={(e) => setEntries(e.target.value)} rows={8} placeholder={text("secret.placeholder")} required />
          </Field>
          <div>
            <Button type="submit">
              <Icon icon={Plus} />
              {text("secret.create")}
            </Button>
          </div>
        </form>
      ) : !ready ? (
        <LoadingCards />
      ) : secrets.length === 0 ? (
        canAdmin ? (
          <EmptyList message={text("secret.empty")} label={text("secret.new")} onCreate={() => setCreating(true)} />
        ) : (
          <p className="text-muted">{text("secret.empty")}</p>
        )
      ) : (
        <ItemList mode={mode}>
          {secrets.map((secret) => (
            <li key={secret.id}>
              <Link to={`/secrets/${secret.id}`} className={entryClass(mode, "stack")}>
                <span className="font-medium">{secret.name}</span>
                <span className="text-muted">
                  {secret.keyCount ?? 0} {(secret.keyCount ?? 0) === 1 ? text("secret.oneKey") : text("secret.manyKeys")} · {secret.apps.length} {secret.apps.length === 1 ? text("secret.oneApp") : text("secret.manyApps")}
                </span>
              </Link>
            </li>
          ))}
        </ItemList>
      )}
    </>
  );
}

export function SecretPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const { canAdmin } = useSession();
  const [secret, setSecret] = useState<AppSecret | null>(null);
  const [apps, setApps] = useState<Application[]>([]);
  const [name, setName] = useState("");
  const [entries, setEntries] = useState("");
  const [removed, setRemoved] = useState<string[]>([]);
  const [granted, setGranted] = useState<string[]>([]);
  const [confirm, setConfirm] = useState(false);
  const [saving, setSaving] = useState(false);
  const toast = useToast();
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);

  useTrail([
    { label: text("nav.secrets"), to: "/secrets" },
    { label: secret?.name || text("crumb.secret") },
  ]);

  async function reload() {
    const [row, list] = await Promise.all([api<AppSecret>(`/api/app-secrets/${id}`), api<Application[]>("/api/applications")]);
    setSecret(row);
    setName(row.name);
    setGranted(row.apps.map((app) => app.id));
    setRemoved([]);
    setEntries("");
    setApps(list);
  }

  useEffect(() => {
    reload()
      .catch((err) => setError(err instanceof Error ? err.message : text("common.error")))
      .finally(() => setReady(true));
  }, [id]);

  function toggle(appId: string) {
    setGranted((current) => (current.includes(appId) ? current.filter((item) => item !== appId) : [...current, appId]));
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    if (saving) return;
    setError("");
    setSaving(true);
    try {
      const set = entries.trim() ? parseEntries(entries) : {};
      await api(`/api/app-secrets/${id}`, {
        method: "PUT",
        body: JSON.stringify({ name, set, remove: removed, apps: granted }),
      });
      await reload();
      toast(text("common.savedDone"));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    await api(`/api/app-secrets/${id}`, { method: "DELETE", body: JSON.stringify({ confirmedName: secret?.name }) });
    toast(text("secret.removed", { name: secret?.name ?? "" }));
    navigate("/secrets");
  }

  if (!ready) return <LoadingCards />;
  if (!secret) return <ErrorText>{error || text("err.not_found")}</ErrorText>;

  const keys = (secret.keys ?? []).filter((key) => !removed.includes(key));

  return (
    <div className="max-w-[720px]">
      <PageTitle title={secret.name} icon={Shield} />
      <p className="text-muted">{text("secret.hidden")}</p>
      {error ? (
        <div className="mt-6">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {canAdmin ? (
      <form onSubmit={save} className="mt-8 flex flex-col gap-8">
        <Field label={text("field.name")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <section>
          <SectionTitle>{text("field.keys")}</SectionTitle>
          {keys.length === 0 ? (
            <p className="mt-4 text-muted">{text("secret.none")}</p>
          ) : (
            <ul className="mt-4 flex flex-col gap-2">
              {keys.map((key) => (
                <li key={key} className="flex flex-wrap items-center justify-between gap-4">
                  <span className="font-mono text-[13px]">{key}</span>
                  <TextButton type="button" onClick={() => setRemoved((current) => [...current, key])}>
                    {text("common.remove")}
                  </TextButton>
                </li>
              ))}
            </ul>
          )}
        </section>
        <Field label={text("secret.add")}>
          <TextArea value={entries} onChange={(e) => setEntries(e.target.value)} rows={6} placeholder={text("secret.placeholder")} />
        </Field>
        <section>
          <SectionTitle>{text("secret.allowed")}</SectionTitle>
          {apps.length === 0 ? (
            <p className="mt-4 text-muted">{text("secret.noApp")}</p>
          ) : (
            <ul className="mt-4 flex flex-col gap-3">
              {apps.map((app) => (
                <li key={app.id}>
                  <label className="flex items-center gap-3">
                    <input type="checkbox" checked={granted.includes(app.id)} onChange={() => toggle(app.id)} />
                    <span>{app.name}</span>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </section>
        <div>
          <Button type="submit" disabled={saving}>
            {saving ? <Spinner /> : null}
            {text("common.save")}
          </Button>
        </div>
      </form>
      ) : (
        <div className="mt-8 flex flex-col gap-8">
          <p className="text-muted">{secret.name}</p>
          <section>
            <SectionTitle>{text("field.keys")}</SectionTitle>
            {keys.length === 0 ? (
              <p className="mt-4 text-muted">{text("secret.none")}</p>
            ) : (
              <ul className="mt-4 flex flex-col gap-2 font-mono text-[13px]">
                {keys.map((key) => (
                  <li key={key}>{key}</li>
                ))}
              </ul>
            )}
          </section>
          <section>
            <SectionTitle>{text("secret.allowed")}</SectionTitle>
            <ul className="mt-4 flex flex-col gap-2">
              {secret.apps.map((app) => (
                <li key={app.id}>
                  <Link to={`/applications/${app.id}`} className="text-ink">{app.name}</Link>
                </li>
              ))}
            </ul>
          </section>
        </div>
      )}
      {canAdmin ? (
      <div className="mt-16 border-t border-line pt-8">
        <TextButton type="button" tone="danger" onClick={() => setConfirm(true)}>
          <Icon icon={Trash2} />
          {text("common.delete")}
        </TextButton>
      </div>
      ) : null}
      {canAdmin && confirm ? (
        <ConfirmDialog
          title={text("server.removeNamed", { name: secret.name })}
          confirmLabel={text("common.delete")}
          danger
          phrase={secret.name}
          onConfirm={remove}
          onClose={() => setConfirm(false)}
        >
          {text("secret.deleteHint", { name: secret.name })}
        </ConfirmDialog>
      ) : null}
    </div>
  );
}

export function AppSecretLinks({ appId }: { appId: string }) {
  const [rows, setRows] = useState<AppSecret[] | null>(null);

  useEffect(() => {
    api<AppSecret[]>("/api/app-secrets")
      .then(setRows)
      .catch(() => setRows([]));
  }, [appId]);

  const granted = (rows ?? []).filter((row) => row.apps.some((app) => app.id === appId));

  return (
    <section className="mt-10">
      <SectionTitle>{text("nav.secrets")}</SectionTitle>
      {rows === null ? (
        <p className="mt-4 text-muted">{text("secret.loading")}</p>
      ) : granted.length === 0 ? (
        <p className="mt-4 text-muted">{text("secret.noneGranted")}</p>
      ) : (
        <ul className="mt-4 flex flex-col gap-2">
          {granted.map((row) => (
            <li key={row.id}>
              <Link to={`/secrets/${row.id}`} className="text-ink">
                {row.name}
              </Link>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-4">
        <Link to="/secrets" className="text-muted">
          {text("secret.manage")}
        </Link>
      </p>
    </section>
  );
}
