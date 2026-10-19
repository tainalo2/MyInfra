import { FormEvent, useState } from "react";
import { Waypoints } from "lucide-react";
import { api } from "./api";
import { text } from "./i18n";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { usePoll } from "./poll";
import { Button, ErrorText, Field, Input, LoadingLines, PageTitle, TextButton } from "./ui";

type Peer = {
  url: string;
  lastOk?: string;
  lastError?: string;
};

type Status = {
  instanceId: string;
  replica: boolean;
  peers: Peer[];
};

export function Copies() {
  const { data: status, setData: setStatus, error: loadError } = usePoll<Status>("/api/sync", 5000);
  const [url, setUrl] = useState("");
  const [error, setError] = useState("");
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState("");
  const toast = useToast();

  async function add(event: FormEvent) {
    event.preventDefault();
    if (adding) return;
    setError("");
    setAdding(true);
    try {
      setStatus(await api<Status>("/api/sync/peers", { method: "POST", body: JSON.stringify({ url }) }));
      toast(text("copy.added", { url }));
      setUrl("");
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setAdding(false);
    }
  }

  async function remove(peer: string) {
    setStatus(await api<Status>("/api/sync/peers", { method: "DELETE", body: JSON.stringify({ url: peer }) }));
    setRemoving("");
    toast(text("copy.removed", { url: peer }));
  }

  if (!status) {
    return loadError ? <ErrorText>{loadError}</ErrorText> : <LoadingLines />;
  }

  return (
    <>
      <PageTitle title={text("copy.title")} icon={Waypoints} />
      <p className="mb-8 max-w-xl text-muted">
        {text("ui.lesCopiesQuiPartagentLaMeme")}
      </p>
      <p className="mb-12 text-muted">
        {status.replica
          ? text("ui.cetteCopieNePilotePasLes")
          : text("ui.cetteCopiePiloteLesMachines")}
      </p>
      <form onSubmit={add} className="mb-12 flex flex-col gap-6">
        <Field label={text("copy.address")}>
          <Input value={url} placeholder="https://myinfra.exemple" onChange={(e) => setUrl(e.target.value)} />
        </Field>
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div>
          <Button type="submit" disabled={url.trim() === "" || adding}>
            {adding ? <Spinner /> : null}
            {text("ui.ajouter")}
          </Button>
        </div>
      </form>
      {status.peers.length === 0 ? (
        <p className="text-muted">{text("copy.empty")}</p>
      ) : (
        <ul className="flex flex-col gap-6">
          {status.peers.map((peer) => (
            <li key={peer.url} className="flex flex-wrap items-start justify-between gap-4 border-b border-line pb-6">
              <div className="min-w-0">
                <p className="break-all">{peer.url}</p>
                <p className="text-muted">{peer.lastError ? peer.lastError : peer.lastOk ? text("copy.upToDate") : text("ui.enAttente")}</p>
              </div>
              <TextButton type="button" tone="danger" onClick={() => setRemoving(peer.url)}>
                {text("common.remove")}
              </TextButton>
            </li>
          ))}
        </ul>
      )}
      {removing ? (
        <ConfirmDialog title={text("copy.removeTitle")} confirmLabel={text("common.remove")} danger onConfirm={() => remove(removing)} onClose={() => setRemoving("")}>
          {text("copy.removeHint", { url: removing })}
        </ConfirmDialog>
      ) : null}
    </>
  );
}
