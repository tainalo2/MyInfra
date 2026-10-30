import { useEffect, useState, type FormEvent } from "react";
import { api, type Application, type Overview } from "./api";
import { text } from "./i18n";
import { placeOf } from "./places";
import { Button, ErrorText, Field, LoadingLines, Select, TextButton, liveServer } from "./ui";

// Placement says where an application runs: its pods, the places of their
// servers, and the one server it hangs on when its cluster could spread it.
export type Placement = { pods: number; places: string[]; servers: string[]; single: string; nodes: number };

export function placements(data: Overview | null) {
  const out = new Map<string, Placement>();
  if (!data) return out;
  const serverById = new Map(data.servers.map((server) => [server.id, server]));
  const placeLabel = (id: string) => {
    const server = serverById.get(id);
    return server ? placeOf(server.plan?.location)?.label || placeOf(server.place)?.label || "" : "";
  };
  for (const app of data.apps) {
    const pods = data.pods.filter((pod) => pod.appId === app.id);
    const hosts = new Set(pods.map((pod) => pod.serverId ?? "").filter(Boolean));
    if (!app.clusterId && app.serverId) hosts.add(app.serverId);
    const nodes = app.clusterId ? data.servers.filter((server) => server.clusterId === app.clusterId && liveServer(server)).length : 0;
    // A copy that restarts elsewhere from its replicated data is not a single point.
    const single = app.clusterId && !app.paused && !app.failover && nodes >= 2 && hosts.size === 1 ? [...hosts][0] : "";
    out.set(app.id, {
      pods: pods.length,
      places: [...new Set([...hosts].map(placeLabel).filter(Boolean))],
      servers: [...hosts].map((id) => serverById.get(id)?.name ?? "").filter(Boolean),
      single: single ? serverById.get(single)?.name ?? "" : "",
      nodes,
    });
  }
  return out;
}

type RedundancyPlan = { scaled: string[]; failover: string[]; database: string[]; alone: string[] };
type RedundancyOptions = { plan: RedundancyPlan; nodes: number; storageNodes: number; backedUp: string[] };

export function RedundancyDialog({ app, nodes, onClose, onDone }: { app: Application; nodes: number; onClose: () => void; onDone: () => void }) {
  const [copies, setCopies] = useState(Math.min(3, nodes));
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  // The server knows how each service keeps its data, and so what it can become.
  const [options, setOptions] = useState<RedundancyOptions | null>(null);
  useEffect(() => {
    let stop = false;
    api<RedundancyOptions>(`/api/applications/${app.id}/redundancy`)
      .then((next) => {
        if (!stop) setOptions(next);
      })
      .catch((err) => {
        if (!stop) setError(err instanceof Error ? err.message : text("common.error"));
      });
    return () => {
      stop = true;
    };
  }, [app.id]);
  const plan = options?.plan;
  const needsCopies = Boolean(plan && plan.scaled.length + plan.database.length > 0);
  const needsStorage = Boolean(plan && plan.failover.length + plan.database.length > 0);
  const storageShort = Boolean(options && needsStorage && options.nodes < options.storageNodes);
  const actionable = Boolean(plan && plan.scaled.length + plan.failover.length + plan.database.length > 0);
  const choices = Array.from({ length: Math.max(0, nodes - 1) }, (_, i) => i + 2);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSending(true);
    setError("");
    try {
      await api(`/api/applications/${app.id}/redundancy`, { method: "POST", body: JSON.stringify({ copies: needsCopies ? copies : 0 }) });
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setSending(false);
    }
  }

  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center bg-ink/40 px-6" role="dialog" aria-modal="true" aria-labelledby="redundancy-title">
      <form onSubmit={submit} className="flex max-h-[80vh] w-full max-w-md flex-col rounded-md border border-line bg-surface p-6">
        <h2 id="redundancy-title" className="mb-3 text-lg">{text("redundancy.title", { name: app.name })}</h2>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <p className="mb-6 text-muted">{text("redundancy.intro")}</p>
          {!options && !error ? <LoadingLines lines={2} label={text("field.services")} /> : null}
          {plan && !actionable ? <p className="mb-6 text-warn">{text("redundancy.none")}</p> : null}
          {storageShort ? <p className="mb-6 text-warn">{text("redundancy.storageShort", { count: options?.storageNodes ?? 3 })}</p> : null}
          {plan && actionable ? (
            <>
              {needsCopies ? (
                <div className="mb-6">
                  <Field label={text("redundancy.copies")}>
                    <Select value={copies} onChange={(e) => setCopies(Number(e.target.value))} autoFocus>
                      {choices.map((count) => (
                        <option key={count} value={count}>
                          {text("redundancy.option", { count })}
                        </option>
                      ))}
                    </Select>
                  </Field>
                </div>
              ) : null}
              <ul className="mb-6">
                {plan.scaled.map((name) => (
                  <li key={name} className="border-t border-line py-2">
                    {text("redundancy.scaled", { name, count: copies })}
                  </li>
                ))}
                {plan.database.map((name) => (
                  <li key={name} className="border-t border-line py-2">
                    {text("redundancy.database", { name, count: copies })}
                  </li>
                ))}
                {plan.failover.map((name) => (
                  <li key={name} className="border-t border-line py-2">
                    {options?.backedUp.includes(name) ? text("redundancy.failoverOn", { name }) : text("redundancy.failover", { name })}
                  </li>
                ))}
                {plan.alone.map((name) => (
                  <li key={name} className="border-t border-line py-2 text-muted">
                    {text("redundancy.alone", { name })}
                  </li>
                ))}
              </ul>
            </>
          ) : null}
          {error ? (
            <div className="mb-6">
              <ErrorText>{error}</ErrorText>
            </div>
          ) : null}
        </div>
        <div className="flex items-center gap-4">
          {actionable ? (
            <Button type="submit" disabled={sending || storageShort || (needsCopies && choices.length === 0)}>
              {sending ? <span className="size-4 animate-spin rounded-full border border-current border-t-transparent" /> : null}
              {text("redundancy.deploy")}
            </Button>
          ) : null}
          <TextButton type="button" onClick={onClose}>
            {text("common.cancel")}
          </TextButton>
        </div>
      </form>
    </div>
  );
}

