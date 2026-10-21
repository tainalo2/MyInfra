import { FormEvent, useEffect, useState } from "react";
import { Waypoints } from "lucide-react";
import { answers, type OtherCopy } from "./activity";
import { api } from "./api";
import { text } from "./i18n";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { useI18n } from "./lang";
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
  fixed: boolean;
  peers: Peer[];
  other?: OtherCopy;
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
      <LeadSection status={status} />
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

// LeadSection says which copy drives the machines, and moves the lead: a
// replica takes it, when the main copy fails or to act from here; the copy
// that took it gives it back to the other one.
function LeadSection({ status }: { status: Status }) {
  useI18n();
  const [asking, setAsking] = useState<"" | "lead" | "force" | "handover">("");
  const [restarting, setRestarting] = useState(false);
  const other = status.other;
  const reachable = answers(other);

  async function move(kind: "lead" | "force" | "handover") {
    if (kind === "handover") {
      await api("/api/copies/handover", { method: "POST", body: "{}" });
    } else {
      await api("/api/copies/lead", { method: "POST", body: JSON.stringify({ force: kind === "force" }) });
    }
    setAsking("");
    setRestarting(true);
  }

  return (
    <section className="mb-12 max-w-xl rounded-md border border-line px-4 py-4">
      <h2 className="mb-2 font-medium">{text("lead.title")}</h2>
      <p className="text-muted">
        {status.replica
          ? other
            ? text("lead.follows", { url: other.url })
            : text("lead.followsAway")
          : text("ui.cetteCopiePiloteLesMachines")}
      </p>
      {other ? (
        <p className={`mt-2 flex items-center gap-2 text-[13px] ${reachable ? "text-ok" : "text-bad"}`}>
          <span className={`inline-block size-2 rounded-full ${reachable ? "bg-ok" : "bg-bad"}`} aria-hidden />
          {reachable
            ? text("lead.reachable")
            : other.lastError === "Injoignable."
              ? text("lead.down")
              : other.lastError
                ? text("lead.unreachable", { error: other.lastError })
                : text("ui.enAttente")}
        </p>
      ) : null}
      {!status.replica && other && !other.drives ? <p className="mt-2 text-muted">{text("lead.standby", { url: other.url })}</p> : null}
      {status.fixed ? (
        <p className="mt-4 text-[13px] text-faint">{text("lead.fixed")}</p>
      ) : status.replica ? (
        <div className="mt-4 flex flex-col gap-2">
          <p className="text-[13px] text-faint">{text("lead.takeHint")}</p>
          <div>
            <Button type="button" variant={reachable ? "secondary" : "primary"} onClick={() => setAsking(reachable ? "lead" : "force")}>
              {text("lead.take")}
            </Button>
          </div>
        </div>
      ) : other && !other.drives ? (
        <div className="mt-4">
          <Button type="button" variant="secondary" onClick={() => setAsking("handover")} disabled={!reachable}>
            {text("lead.handover")}
          </Button>
        </div>
      ) : null}
      {asking === "lead" ? (
        <ConfirmDialog title={text("lead.take")} confirmLabel={text("lead.take")} onConfirm={() => move("lead")} onClose={() => setAsking("")}>
          {text("lead.takeText", { url: other?.url ?? "" })}
        </ConfirmDialog>
      ) : null}
      {asking === "force" ? (
        <ConfirmDialog title={text("lead.forceTitle")} confirmLabel={text("lead.force")} danger onConfirm={() => move("force")} onClose={() => setAsking("")}>
          <span className="flex flex-col gap-3">
            <span>{text("lead.forceText", { url: other?.url ?? "" })}</span>
            <span>{text("lead.forceRisk")}</span>
          </span>
        </ConfirmDialog>
      ) : null}
      {asking === "handover" && other ? (
        <ConfirmDialog title={text("lead.handover")} confirmLabel={text("lead.handoverConfirm")} onConfirm={() => move("handover")} onClose={() => setAsking("")}>
          {text("lead.handoverText", { url: other.url })}
        </ConfirmDialog>
      ) : null}
      {restarting ? <RoleRestarting replica={!status.replica} /> : null}
    </section>
  );
}

// RoleRestarting covers the page while this copy starts again in its new
// role, then reloads it.
function RoleRestarting({ replica }: { replica: boolean }) {
  useI18n();
  useEffect(() => {
    const timer = window.setInterval(async () => {
      try {
        const body = await api<Status>("/api/sync");
        if (body.replica === replica) window.location.reload();
      } catch {
        // Not back yet.
      }
    }, 2000);
    return () => window.clearInterval(timer);
  }, [replica]);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-canvas/95 px-6" role="alertdialog" aria-live="assertive" aria-label={text("lead.restarting")}>
      <div className="flex max-w-sm flex-col items-center gap-4 text-center">
        <Spinner />
        <p className="text-lg">{text("lead.restarting")}</p>
        <p className="text-muted">{text(replica ? "lead.restartingFollow" : "lead.restartingLead")}</p>
      </div>
    </div>
  );
}
