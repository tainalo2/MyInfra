import { FormEvent, useEffect, useRef, useState } from "react";
import { Download, FileUp, KeyRound, Plus, Trash2 } from "lucide-react";
import { api, type Secret } from "./api";
import { text } from "./i18n";
import { ConfirmDialog, useToast } from "./kit";
import { useSession } from "./session";
import { Button, EmptyList, ErrorText, Field, Icon, Input, ItemList, ListActions, ListModeButton, LoadingCards, PageTitle, TextArea, TextButton, entryClass, useListMode } from "./ui";

export function Keys() {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [keyName, setKeyName] = useState("");
  const [importName, setImportName] = useState("");
  const [pastedKey, setPastedKey] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);
  const kind = keyKind(pastedKey);
  const [showImport, setShowImport] = useState(false);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);
  const [creating, setCreating] = useState(false);
  const [mode, toggleMode] = useListMode();
  const [removing, setRemoving] = useState<Secret | null>(null);
  const toast = useToast();
  const { canAdmin } = useSession();

  async function reload() {
    const rows = await api<Secret[]>("/api/secrets");
    setSecrets(rows.filter((secret) => secret.kind === "ssh_key"));
  }

  useEffect(() => {
    reload()
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
  }, []);

  function close() {
    setCreating(false);
    setShowImport(false);
    setKeyName("");
    setImportName("");
    setPastedKey("");
    setError("");
  }

  async function generate(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await api("/api/secrets/ssh-keys", { method: "POST", body: JSON.stringify({ name: keyName }) });
      setKeyName("");
      setCreating(false);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  async function importKey(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await api("/api/secrets/ssh-keys/import", {
        method: "POST",
        body: JSON.stringify(kind === "private" ? { name: importName, privateKey: pastedKey } : { name: importName, publicKey: pastedKey }),
      });
      setImportName("");
      setPastedKey("");
      setShowImport(false);
      setCreating(false);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  // A key file is a few kilobytes: anything larger is not one.
  async function readKeyFile(file: File) {
    setError("");
    if (file.size > 64 * 1024) {
      setError(text("keys.fileTooBig"));
      return;
    }
    setPastedKey((await file.text()).trim());
    if (!importName.trim()) setImportName(file.name.replace(/\.pub$/, ""));
  }

  async function download(secret: Secret) {
    const body = await api<{ privateKey: string }>(`/api/secrets/${secret.id}/private`);
    const blob = new Blob([body.privateKey], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `${secret.name}.key`;
    link.click();
    URL.revokeObjectURL(url);
  }

  async function remove(secret: Secret) {
    await api(`/api/secrets/${secret.id}`, { method: "DELETE" });
    setSecrets((current) => current.filter((item) => item.id !== secret.id));
    setRemoving(null);
    toast(text("key.removed", { name: secret.name }));
  }

  return (
    <>
      <PageTitle
        title={text("key.title")}
        icon={KeyRound}
        action={
          creating ? (
            <TextButton type="button" onClick={close}>
              {text("common.cancel")}
            </TextButton>
          ) : canAdmin && ready && secrets.length > 0 ? (
            <ListActions mode={mode} onToggle={toggleMode} label={text("key.new")} onCreate={() => setCreating(true)} />
          ) : ready && secrets.length > 0 ? (
            <span className="hidden md:inline-flex">
              <ListModeButton mode={mode} onToggle={toggleMode} />
            </span>
          ) : null
        }
      />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {canAdmin && creating ? (
        <>
          <form onSubmit={generate} className="mb-6 flex max-w-[720px] flex-col gap-6">
            <Field label={text("field.name")}>
              <Input value={keyName} onChange={(e) => setKeyName(e.target.value)} required />
            </Field>
            <div className="flex items-center gap-6">
              <Button type="submit">
                <Icon icon={Plus} />
                {text("server.createKey")}
              </Button>
              <TextButton type="button" onClick={() => setShowImport((value) => !value)}>
                <Icon icon={KeyRound} />
                {text("ui.importerUneClePublique")}
              </TextButton>
            </div>
          </form>
          {showImport ? (
            <form onSubmit={importKey} className="flex max-w-[720px] flex-col gap-6">
              <Field label={text("field.name")}>
                <Input value={importName} onChange={(e) => setImportName(e.target.value)} required />
              </Field>
              <Field label={text("field.sshKey")} hint={kind === "private" ? text("keys.detectedPrivate") : kind === "public" ? text("keys.detectedPublic") : text("keys.keyHint")}>
                <TextArea value={pastedKey} onChange={(e) => setPastedKey(e.target.value)} autoComplete="off" spellCheck={false} required />
              </Field>
              <input
                ref={fileRef}
                type="file"
                className="hidden"
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  if (file) void readKeyFile(file);
                }}
              />
              <div>
                <TextButton type="button" onClick={() => fileRef.current?.click()}>
                  <Icon icon={FileUp} />
                  {text("keys.fromFile")}
                </TextButton>
              </div>
              <div>
                <Button type="submit">{text("cluster.import")}</Button>
              </div>
            </form>
          ) : null}
        </>
      ) : !ready ? (
        <LoadingCards />
      ) : secrets.length === 0 ? (
        canAdmin ? (
          <EmptyList message={text("key.empty")} label={text("key.new")} onCreate={() => setCreating(true)} />
        ) : (
          <p className="text-muted">{text("key.empty")}</p>
        )
      ) : (
        <ItemList mode={mode}>
          {secrets.map((secret) => (
            <li key={secret.id} className={entryClass(mode, mode === "cards" ? "stack" : "spread")}>
              <span className="flex min-w-0 items-start gap-3">
                <Icon icon={KeyRound} className="mt-1 text-muted" />
                <span className="min-w-0">
                  {secret.name}
                  <span className="mt-1 block break-all font-mono text-[13px] text-muted">{secret.fingerprint}</span>
                </span>
              </span>
              {canAdmin ? (
                <span className="flex gap-4">
                  {secret.hasPrivate ? (
                    <TextButton type="button" onClick={() => download(secret).catch((err) => setError(err.message))}>
                      <Icon icon={Download} />
                      {text("ui.telecharger")}
                    </TextButton>
                  ) : null}
                  <TextButton type="button" tone="danger" onClick={() => setRemoving(secret)}>
                    <Icon icon={Trash2} />
                    {text("common.delete")}
                  </TextButton>
                </span>
              ) : null}
            </li>
          ))}
        </ItemList>
      )}
      {canAdmin && removing ? (
        <ConfirmDialog
          title={text("server.removeNamed", { name: removing.name })}
          confirmLabel={text("common.delete")}
          danger
          onConfirm={() => remove(removing)}
          onClose={() => setRemoving(null)}
        >
          {text("key.deleteHint")}
        </ConfirmDialog>
      ) : null}
    </>
  );
}

// keyKind tells a private key, which always opens with a PEM header, from a
// public one on a single authorized_keys line.
function keyKind(value: string): "private" | "public" | "" {
  const trimmed = value.trim();
  if (!trimmed) return "";
  return /-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----/.test(trimmed) ? "private" : "public";
}
