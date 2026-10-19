import { FormEvent, useEffect, useState } from "react";
import { Waypoints } from "lucide-react";
import { api } from "./api";
import { text } from "./i18n";
import { Button, ErrorText, Field, Input, LoadingLines, PageTitle } from "./ui";

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
  const [status, setStatus] = useState<Status | null>(null);
  const [url, setUrl] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let cancel = false;
    const run = () => {
      api<Status>("/api/sync")
        .then((next) => {
          if (!cancel) setStatus(next);
        })
        .catch((err: unknown) => {
          if (!cancel) setError(err instanceof Error ? err.message : text("common.error"));
        });
    };
    run();
    const timer = window.setInterval(run, 5000);
    return () => {
      cancel = true;
      window.clearInterval(timer);
    };
  }, []);

  async function add(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      setStatus(await api<Status>("/api/sync/peers", { method: "POST", body: JSON.stringify({ url }) }));
      setUrl("");
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  async function remove(peer: string) {
    setError("");
    try {
      setStatus(await api<Status>("/api/sync/peers", { method: "DELETE", body: JSON.stringify({ url: peer }) }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  if (!status) {
    return error ? <ErrorText>{error}</ErrorText> : <LoadingLines />;
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
          <Button type="submit" disabled={url.trim() === ""}>
            {text("ui.ajouter")}
          </Button>
        </div>
      </form>
      {status.peers.length === 0 ? (
        <p className="text-muted">{text("copy.empty")}</p>
      ) : (
        <ul className="flex flex-col gap-6">
          {status.peers.map((peer) => (
            <li key={peer.url} className="flex items-start justify-between gap-6 border-b border-line pb-6">
              <div>
                <p>{peer.url}</p>
                <p className="text-muted">{peer.lastError ? peer.lastError : peer.lastOk ? "À jour." : text("ui.enAttente")}</p>
              </div>
              <Button type="button" onClick={() => remove(peer.url)}>
                {text("common.remove")}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
