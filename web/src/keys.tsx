import { FormEvent, useEffect, useState } from "react";
import { Download, KeyRound, Plus, Trash2 } from "lucide-react";
import { api, type Secret } from "./api";
import { text } from "./i18n";
import { Button, EmptyList, ErrorText, Field, Icon, Input, ItemList, ListActions, LoadingCards, PageTitle, TextArea, TextButton, entryClass, useListMode } from "./ui";

export function Keys() {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [keyName, setKeyName] = useState("");
  const [importName, setImportName] = useState("");
  const [publicKey, setPublicKey] = useState("");
  const [showImport, setShowImport] = useState(false);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);
  const [creating, setCreating] = useState(false);
  const [mode, toggleMode] = useListMode();

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
    setPublicKey("");
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
        body: JSON.stringify({ name: importName, publicKey }),
      });
      setImportName("");
      setPublicKey("");
      setShowImport(false);
      setCreating(false);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
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
        title={text("key.title")}
        icon={KeyRound}
        action={
          creating ? (
            <TextButton type="button" onClick={close}>
              {text("common.cancel")}
            </TextButton>
          ) : ready && secrets.length > 0 ? (
            <ListActions mode={mode} onToggle={toggleMode} label={text("key.new")} onCreate={() => setCreating(true)} />
          ) : null
        }
      />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {creating ? (
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
              <Field label={text("field.publicKey")}>
                <TextArea value={publicKey} onChange={(e) => setPublicKey(e.target.value)} required />
              </Field>
              <div>
                <Button type="submit">{text("cluster.import")}</Button>
              </div>
            </form>
          ) : null}
        </>
      ) : !ready ? (
        <LoadingCards />
      ) : secrets.length === 0 ? (
        <EmptyList message={text("key.empty")} label={text("key.new")} onCreate={() => setCreating(true)} />
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
              <span className="flex gap-4">
                {secret.hasPrivate ? (
                  <TextButton type="button" onClick={() => download(secret).catch((err) => setError(err.message))}>
                    <Icon icon={Download} />
                    {text("ui.telecharger")}
                  </TextButton>
                ) : null}
                <TextButton type="button" onClick={() => remove(secret.id)}>
                  <Icon icon={Trash2} />
                  {text("common.delete")}
                </TextButton>
              </span>
            </li>
          ))}
        </ItemList>
      )}
    </>
  );
}
