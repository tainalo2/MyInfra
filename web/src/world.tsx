import { RunningList } from "./activity";
import { useEffect, useState } from "react";
import { Earth, History } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { api, peek, type InfraEvent } from "./api";
import { currentLocale, text, type MsgKey } from "./i18n";
import { Pill } from "./dnsrecords";
import { StatusBadge } from "./kit";
import { useI18n } from "./lang";
import { Button, ErrorText, Icon, Input, LoadingLines, PageTitle, Select, focus } from "./ui";

type Kind = "all" | "problem" | "deploy" | "server" | "update" | "assistant";

const kinds: { key: Kind; label: MsgKey; test: (event: InfraEvent) => boolean }[] = [
  { key: "all", label: "activity.filter.all", test: () => true },
  { key: "problem", label: "activity.filter.problem", test: (event) => event.level === "bad" || event.level === "error" || event.level === "warn" },
  { key: "deploy", label: "activity.filter.deploy", test: (event) => /^(deploy|app|rollout|application|package|template)/.test(event.kind) && !event.serverId },
  { key: "server", label: "activity.filter.server", test: (event) => Boolean(event.serverId) || /^(server|cluster)/.test(event.kind) },
  { key: "update", label: "activity.filter.update", test: (event) => event.kind.includes("update") },
  { key: "assistant", label: "activity.filter.assistant", test: (event) => event.source === "mcp" || event.source === "api" },
];

const periods = { week: 7, month: 30, all: 0 } as const;

// HistoryPage is the activity: what runs now, then what happened, by day.
export function HistoryPage() {
  useI18n();
  const [params] = useSearchParams();
  const server = params.get("server") ?? "";
  const cluster = params.get("cluster") ?? "";
  const [rows, setRows] = useState<InfraEvent[] | null>(() => peek<InfraEvent[]>(`/api/events${eventQuery(server, cluster)}`));
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [period, setPeriod] = useState<keyof typeof periods>("all");
  const [kind, setKind] = useState<Kind>("all");
  const [shown, setShown] = useState(40);

  useEffect(() => {
    api<InfraEvent[]>(`/api/events${eventQuery(server, cluster)}`)
      .then(setRows)
      .catch((err) => setError(err instanceof Error ? err.message : text("common.error")));
  }, [server, cluster]);

  const needle = query.trim().toLowerCase();
  const days = periods[period];
  const since = days ? Date.now() - days * 86400000 : 0;
  const base = (rows ?? []).filter(
    (event) =>
      new Date(event.at).getTime() >= since &&
      (!needle || [event.message, event.serverName, event.clusterName, event.actor].some((word) => (word ?? "").toLowerCase().includes(needle))),
  );
  const test = kinds.find((item) => item.key === kind)!.test;
  const list = base.filter(test);
  const groups: { day: string; rows: InfraEvent[] }[] = [];
  for (const event of list.slice(0, shown)) {
    const day = dayOf(event.at);
    const last = groups[groups.length - 1];
    if (last && last.day === day) last.rows.push(event);
    else groups.push({ day, rows: [event] });
  }

  return (
    <>
      <PageTitle title={text("nav.activity")} />
      <div className="mb-8 flex flex-wrap gap-3">
        <div className="min-w-0 flex-[1_1_260px]">
          <Input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={text("activity.search")} aria-label={text("activity.search")} />
        </div>
        <div className="w-full sm:w-[200px]">
          <Select aria-label={text("activity.period")} value={period} onChange={(event) => setPeriod(event.target.value as keyof typeof periods)}>
            <option value="week">{text("activity.period.week")}</option>
            <option value="month">{text("activity.period.month")}</option>
            <option value="all">{text("activity.period.all")}</option>
          </Select>
        </div>
      </div>
      <RunningList />
      <div role="group" aria-label={text("activity.type")} className="-mx-4 mb-6 flex gap-2 overflow-x-auto px-4 md:mx-0 md:flex-wrap md:px-0">
        {kinds.map((item) => (
          <Pill key={item.key} on={kind === item.key} onClick={() => setKind(item.key)} count={rows ? base.filter(item.test).length : undefined}>
            {text(item.label)}
          </Pill>
        ))}
      </div>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {!rows && !error ? <LoadingLines lines={4} label={text("nav.history")} /> : null}
      {rows && list.length === 0 ? <p className="text-muted">{text("ui.aucunEvenement")}</p> : null}
      <div className="flex flex-col gap-8">
        {groups.map((group) => (
          <section key={group.day}>
            <h3 className="mb-2 text-[13px] text-muted">{group.day}</h3>
            <ul className="overflow-hidden rounded-md border border-line bg-surface">
              {group.rows.map((event) => (
                <EventRow key={event.id} event={event} />
              ))}
            </ul>
          </section>
        ))}
      </div>
      {list.length > shown ? (
        <div className="mt-6">
          <Button type="button" variant="secondary" onClick={() => setShown((count) => count + 40)}>
            {text("activity.more")}
          </Button>
        </div>
      ) : null}
    </>
  );
}

function dayOf(iso: string) {
  const date = new Date(iso);
  const today = new Date();
  const label = date.toLocaleDateString(currentLocale(), { weekday: "long", day: "numeric", month: "long" });
  const capital = label.charAt(0).toUpperCase() + label.slice(1);
  return date.toDateString() === today.toDateString() ? `${text("activity.today")} · ${label}` : capital;
}

function EventRow({ event }: { event: InfraEvent }) {
  const bad = event.level === "bad" || event.level === "error";
  return (
    <li className="flex gap-4 border-t border-line px-4 py-3 first:border-t-0">
      <span className="w-12 shrink-0 font-mono text-[13px] text-muted">
        {new Date(event.at).toLocaleTimeString(currentLocale(), { hour: "2-digit", minute: "2-digit" })}
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span>{event.message}</span>
          {bad ? <StatusBadge status="failed" /> : null}
        </span>
        <span className="flex flex-wrap items-center gap-x-1.5 text-[13px] text-muted">
          {event.serverId ? (
            <Link to={`/serveurs/${event.serverId}`} className={`text-link ${focus}`}>
              {event.serverName || text("overview.noServer")}
            </Link>
          ) : event.clusterId ? (
            <Link to={`/historique?cluster=${event.clusterId}`} className={`text-link ${focus}`}>
              {event.clusterName || text("field.cluster")}
            </Link>
          ) : null}
          {event.serverId || event.clusterId ? <span aria-hidden>·</span> : null}
          <span>{who(event)}</span>
        </span>
      </span>
    </li>
  );
}

function who(event: InfraEvent) {
  if (event.source === "auto") return text("activity.night");
  if (event.source === "mcp") return `${text("mcp.badge")}${event.actor ? ` · ${event.actor}` : ""}`;
  if (event.source === "api") return `${text("mcp.badgeApi")}${event.actor ? ` · ${event.actor}` : ""}`;
  return event.actor || "MyInfra";
}

function eventQuery(server?: string, cluster?: string, source?: string) {
  const query = new URLSearchParams();
  if (server) query.set("server", server);
  if (cluster) query.set("cluster", cluster);
  if (source) query.set("source", source);
  return query.toString() ? `?${query}` : "";
}

export function EventList({ server, cluster, source, limit }: { server?: string; cluster?: string; source?: "mcp"; limit?: number }) {
  const [rows, setRows] = useState<InfraEvent[] | null>(() => peek<InfraEvent[]>(`/api/events${eventQuery(server, cluster, source)}`));
  const [error, setError] = useState("");

  useEffect(() => {
    const suffix = eventQuery(server, cluster, source);
    api<InfraEvent[]>(`/api/events${suffix}`)
      .then(setRows)
      .catch((err) => setError(err instanceof Error ? err.message : text("common.error")));
  }, [server, cluster, source]);

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
            {event.source === "mcp" || event.source === "api" ? (
              <span className="rounded border border-line px-1.5 text-[12px]" title={text("mcp.badgeHint")}>
                {event.source === "mcp" ? text("mcp.badge") : text("mcp.badgeApi")}
                {event.actor ? ` · ${event.actor}` : ""}
              </span>
            ) : null}
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
