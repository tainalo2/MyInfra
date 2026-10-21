import { RunningList } from "./activity";
import { useEffect, useState } from "react";
import { Earth, History } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { api, peek, type InfraEvent } from "./api";
import { currentLocale, text } from "./i18n";
import { ErrorText, Icon, LoadingLines, PageTitle } from "./ui";

export function HistoryPage() {
  const [params] = useSearchParams();
  return (
    <>
      <PageTitle title={text("nav.activity")} icon={History} />
      <RunningList />
      <h2 className="mb-3 text-lg">{text("activity.history")}</h2>
      <EventList server={params.get("server") ?? ""} cluster={params.get("cluster") ?? ""} />
    </>
  );
}

function eventQuery(server?: string, cluster?: string) {
  const query = new URLSearchParams();
  if (server) query.set("server", server);
  if (cluster) query.set("cluster", cluster);
  return query.toString() ? `?${query}` : "";
}

export function EventList({ server, cluster, limit }: { server?: string; cluster?: string; limit?: number }) {
  const [rows, setRows] = useState<InfraEvent[] | null>(() => peek<InfraEvent[]>(`/api/events${eventQuery(server, cluster)}`));
  const [error, setError] = useState("");

  useEffect(() => {
    const suffix = eventQuery(server, cluster);
    api<InfraEvent[]>(`/api/events${suffix}`)
      .then(setRows)
      .catch((err) => setError(err instanceof Error ? err.message : text("common.error")));
  }, [server, cluster]);

  if (error) return <ErrorText>{error}</ErrorText>;
  if (!rows) return <LoadingLines lines={4} label={text("nav.history")} />;
  if (rows.length === 0) return <p className="text-muted">{text("ui.aucunEvenement")}</p>;
  return (
    <ul className="max-w-[720px]">
      {(limit ? rows.slice(0, limit) : rows).map((event) => (
        <li key={event.id} className="border-t border-line py-3">
          <div className="flex items-baseline justify-between gap-6">
            <span>{event.message}</span>
            <span className={levelTone(event.level)}>{when(event.at)}</span>
          </div>
          <p className="mt-1 flex items-center gap-2 text-muted">
            <Icon icon={event.serverId ? History : Earth} />
            {event.serverId ? (
              <Link to={`/serveurs/${event.serverId}`} className="underline">{event.serverName || "Serveur"}</Link>
            ) : null}
            {event.clusterId ? (
              <Link to={`/historique?cluster=${event.clusterId}`} className="underline">{event.clusterName || text("field.cluster")}</Link>
            ) : null}
          </p>
        </li>
      ))}
    </ul>
  );
}

function levelTone(level: string) {
  if (level === "bad") return "text-bad";
  if (level === "warn") return "text-warn";
  if (level === "ok") return "text-ok";
  return "text-muted";
}

function when(iso: string) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(currentLocale(), { dateStyle: "medium", timeStyle: "short" });
}
