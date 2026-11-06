import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { RefreshCw } from "lucide-react";
import { api } from "./api";
import { text, type MsgKey } from "./i18n";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { useI18n } from "./lang";
import { usePoll } from "./poll";
import { useSession } from "./session";
import { ErrorText, Icon, LoadingLines, TextButton, focus } from "./ui";

type DnsRecord = { type: string; target: string; server?: string };

type DnsName = {
  name: string;
  records: DnsRecord[];
  expected?: string[];
  use?: "app" | "myinfra";
  useId?: string;
  useName?: string;
  state: "ok" | "differs" | "missing" | "unmanaged";
};

type DnsZone = { zone: string; account: string; provider: string; error?: string; names: DnsName[] };

const stateTone: Record<DnsName["state"], string> = { ok: "text-ok", differs: "text-warn", missing: "text-bad", unmanaged: "text-muted" };

type Filter = "all" | "check" | "used" | "other";

const filters: { key: Filter; label: MsgKey; test: (name: DnsName) => boolean }[] = [
  { key: "all", label: "dnsrec.filter.all", test: () => true },
  { key: "check", label: "dnsrec.filter.check", test: (name) => name.state !== "ok" },
  { key: "used", label: "dnsrec.used", test: (name) => Boolean(name.use) },
  { key: "other", label: "dnsrec.filter.other", test: (name) => !name.use },
];

// DnsRecords shows the address records of every zone MyInfra can write: the
// names it uses, compared with what they should answer, and the others —
// such as a name left behind by a removed copy — that can be removed here.
// It sits under the DNS accounts, on the Domains page.
export function DnsRecords() {
  useI18n();
  const toast = useToast();
  const { canAdmin } = useSession();
  const { data, error, reload } = usePoll<DnsZone[]>("/api/dns/records", 0);
  const [busy, setBusy] = useState("");
  const [drop, setDrop] = useState<{ zone: string; name: string } | null>(null);
  const [picked, setPicked] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [reloading, setReloading] = useState(false);

  async function publish(zone: string, name: string) {
    setBusy(name);
    try {
      await api("/api/dns/records/publish", { method: "POST", body: JSON.stringify({ zone, name }) });
      toast(text("dnsrec.published", { name }));
      await reload();
    } catch (err) {
      toast(err instanceof Error ? err.message : text("common.error"), "bad");
    } finally {
      setBusy("");
    }
  }

  const keyOf = (zone: DnsZone) => zone.zone || zone.account;
  const zone = data?.find((item) => keyOf(item) === picked) ?? data?.[0];
  const check = (item: DnsZone) => item.names.filter((name) => name.state !== "ok").length;
  const shown = zone ? zone.names.filter(filters.find((item) => item.key === filter)!.test) : [];

  return (
    <section id="adresses" aria-label={text("dnsrec.section")}>
      <div className="mb-3 flex flex-wrap items-start justify-between gap-x-6 gap-y-2">
        <div>
          <h2>{text("dnsrec.section")}</h2>
          <p className="max-w-[720px] text-[13px] text-muted">{text("dnsrec.intro")}</p>
        </div>
        <TextButton
          type="button"
          disabled={reloading}
          onClick={async () => {
            setReloading(true);
            await reload();
            setReloading(false);
          }}
        >
          {reloading ? <Spinner /> : <Icon icon={RefreshCw} />}
          {text("dnsrec.reload")}
        </TextButton>
      </div>
      {error && !data ? <ErrorText>{error}</ErrorText> : null}
      {!data ? (
        <LoadingLines lines={5} label={text("dnsrec.section")} />
      ) : data.length === 0 || !zone ? (
        <p className="text-muted">{text("dnsrec.none")}</p>
      ) : (
        <div className="flex flex-col gap-4 md:flex-row md:items-start md:gap-6">
          {data.length > 1 ? (
            <nav aria-label={text("dnsrec.zones")} className="-mx-4 flex gap-2 overflow-x-auto px-4 md:mx-0 md:w-[200px] md:shrink-0 md:flex-col md:gap-0.5 md:overflow-visible md:px-0">
              {data.map((item) => {
                const on = keyOf(item) === keyOf(zone);
                const count = check(item);
                return (
                  <button
                    key={keyOf(item)}
                    type="button"
                    aria-pressed={on}
                    onClick={() => setPicked(keyOf(item))}
                    className={`flex shrink-0 flex-col rounded-md border px-3 py-1.5 text-left md:border-0 ${on ? "border-ink bg-status" : "border-line"} ${focus}`}
                  >
                    <span className="font-mono text-[13px]">{keyOf(item)}</span>
                    <span className={`text-[12px] ${count ? "text-warn" : "text-muted"}`}>
                      {count ? text("dnsrec.toCheck", { count }) : text("dnsrec.names", { count: item.names.length })}
                    </span>
                  </button>
                );
              })}
            </nav>
          ) : null}
          <div className="min-w-0 flex-1">
            <p className="mb-3 text-[13px] text-faint">
              {zone.zone ? `${zone.zone} · ` : ""}
              {zone.account} · {zone.provider.toUpperCase()}
            </p>
            {zone.error ? <ErrorText>{zone.error}</ErrorText> : null}
            {zone.names.length === 0 ? (
              <p className="text-muted">{text("dnsrec.empty", { zone: keyOf(zone) })}</p>
            ) : (
              <>
                <div role="group" aria-label={text("dnsrec.show")} className="mb-3 flex flex-wrap gap-2">
                  {filters.map((item) => {
                    const on = item.key === filter;
                    return (
                      <Pill key={item.key} on={on} onClick={() => setFilter(item.key)} count={zone.names.filter(item.test).length}>
                        {text(item.label)}
                      </Pill>
                    );
                  })}
                </div>
                <div className="overflow-hidden rounded-md border border-line bg-surface">
                  <div aria-hidden className="hidden grid-cols-[minmax(0,1.3fr)_minmax(0,1.2fr)_minmax(0,0.8fr)_minmax(0,1fr)] gap-4 border-b border-line px-4 py-2 text-[12px] text-muted md:grid">
                    <span>{text("dnsrec.col.name")}</span>
                    <span>{text("dnsrec.col.target")}</span>
                    <span>{text("dnsrec.col.app")}</span>
                    <span>{text("dnsrec.col.state")}</span>
                  </div>
                  {shown.length === 0 ? <p className="px-4 py-3 text-muted">{text("overview.noMatch")}</p> : null}
                  <ul className="flex flex-col">
                    {shown.map((name) => (
                      <NameRow key={name.name} name={name}>
                        {canAdmin && name.use && name.state !== "ok" ? (
                          <TextButton type="button" onClick={() => void publish(zone.zone, name.name)} disabled={busy !== ""}>
                            {busy === name.name ? <Spinner /> : null}
                            {text("dnsrec.publish")}
                          </TextButton>
                        ) : null}
                        {canAdmin && !name.use ? (
                          <TextButton type="button" tone="danger" onClick={() => setDrop({ zone: zone.zone, name: name.name })}>
                            {text("common.delete")}
                          </TextButton>
                        ) : null}
                      </NameRow>
                    ))}
                  </ul>
                </div>
              </>
            )}
          </div>
        </div>
      )}
      {drop ? (
        <ConfirmDialog
          title={text("dnsrec.dropTitle", { name: drop.name })}
          confirmLabel={text("common.delete")}
          danger
          phrase={drop.name}
          phraseLabel={text("dnsrec.dropPhrase")}
          onClose={() => setDrop(null)}
          onConfirm={async () => {
            await api("/api/dns/records/drop", { method: "POST", body: JSON.stringify(drop) });
            toast(text("dnsrec.dropped", { name: drop.name }));
            setDrop(null);
            await reload();
          }}
        >
          {text("dnsrec.dropHint")}
        </ConfirmDialog>
      ) : null}
    </section>
  );
}

function NameRow({ name, children }: { name: DnsName; children?: ReactNode }) {
  const owner = name.use === "myinfra" ? `/applications/myinfra` : name.useId ? `/applications/${name.useId}` : "";
  const dot = name.state === "ok" ? "bg-ok" : name.state === "differs" ? "bg-warn" : name.state === "missing" ? "bg-bad" : "bg-faint";
  return (
    <li className="grid gap-x-4 gap-y-1 border-t border-line px-4 py-3 first:border-t-0 md:grid-cols-[minmax(0,1.3fr)_minmax(0,1.2fr)_minmax(0,0.8fr)_minmax(0,1fr)] md:items-start">
      <span className="break-all font-mono text-[13px]">{name.name}</span>
      <span className="flex flex-col text-[13px] [overflow-wrap:anywhere]">
        <span>
          {name.records.length > 0
            ? name.records.map((record) => `${record.type} ${record.target}${record.server ? ` (${record.server})` : ""}`).join(" · ")
            : text("dnsrec.noRecord")}
        </span>
        {name.state === "differs" || name.state === "missing" ? (
          <span className="text-faint">{text("dnsrec.expected", { addresses: (name.expected ?? []).join(" · ") || "—" })}</span>
        ) : null}
      </span>
      <span className="text-[13px]">
        {name.useName ? (
          owner ? (
            <Link to={owner} className={focus}>
              {name.useName}
            </Link>
          ) : (
            name.useName
          )
        ) : (
          <span className="text-faint">{text("dnsrec.noApp")}</span>
        )}
      </span>
      <span className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <span className={`inline-flex items-center gap-1.5 text-[13px] ${stateTone[name.state]}`}>
          <span aria-hidden className={`size-2 rounded-full ${dot}`} />
          {text(`dnsrec.state.${name.state}` as MsgKey)}
        </span>
        {children}
      </span>
    </li>
  );
}

// Pill is a filter: one word and its count, filled when chosen.
export function Pill({ on, onClick, count, children }: { on: boolean; onClick: () => void; count?: number; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      onClick={onClick}
      className={`inline-flex shrink-0 items-center gap-1.5 rounded-md border px-3 py-1 ${on ? "border-ink bg-ink text-canvas" : "border-line bg-surface text-muted"} ${focus}`}
    >
      {children}
      {count !== undefined ? <span className={`text-[13px] ${on ? "opacity-80" : "text-faint"}`}>{count}</span> : null}
    </button>
  );
}
