import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { ListTree, RefreshCw } from "lucide-react";
import { api } from "./api";
import { text, type MsgKey } from "./i18n";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { useI18n } from "./lang";
import { usePoll } from "./poll";
import { Button, ErrorText, Fold, Icon, LoadingLines, PageTitle, TextButton } from "./ui";

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

// DnsRecords shows the address records of every zone MyInfra can write: the
// names it uses, compared with what they should answer, and the others —
// such as a name left behind by a removed copy — that can be removed here.
export function DnsRecords() {
  useI18n();
  const toast = useToast();
  const { data, error, reload } = usePoll<DnsZone[]>("/api/dns/records", 0);
  const [busy, setBusy] = useState("");
  const [drop, setDrop] = useState<{ zone: string; name: string } | null>(null);

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

  return (
    <>
      <PageTitle
        title={text("nav.dnsRecords")}
        icon={ListTree}
        action={
          <Button type="button" variant="secondary" onClick={() => void reload()}>
            <Icon icon={RefreshCw} />
            {text("dnsrec.reload")}
          </Button>
        }
      />
      <p className="-mt-6 mb-8 max-w-[720px] text-muted md:-mt-8">{text("dnsrec.intro")}</p>
      {error && !data ? <ErrorText>{error}</ErrorText> : null}
      {!data ? (
        <LoadingLines lines={5} label={text("nav.dnsRecords")} />
      ) : data.length === 0 ? (
        <p className="text-muted">{text("dnsrec.none")}</p>
      ) : (
        <div className="flex max-w-[960px] flex-col gap-6">
          {data.map((zone) => {
            const used = zone.names.filter((name) => name.use);
            const others = zone.names.filter((name) => !name.use);
            const attention = used.some((name) => name.state !== "ok");
            return (
              <Fold
                key={zone.zone || zone.account}
                title={`${zone.zone || zone.account}${attention ? ` · ${text("dnsrec.attention")}` : ""}`}
                defaultOpen={used.length > 0}
              >
                <p className="mb-3 text-[13px] text-faint">
                  {zone.account} · {zone.provider.toUpperCase()}
                </p>
                {zone.error ? <ErrorText>{zone.error}</ErrorText> : null}
                {used.length > 0 ? (
                  <section className="mb-6">
                    <h3 className="mb-2 text-muted">{text("dnsrec.used")}</h3>
                    <ul className="flex flex-col">
                      {used.map((name) => (
                        <NameRow key={name.name} name={name}>
                          {name.state !== "ok" ? (
                            <Button type="button" variant="secondary" onClick={() => void publish(zone.zone, name.name)} disabled={busy !== ""}>
                              {busy === name.name ? <Spinner /> : null}
                              {text("dnsrec.publish")}
                            </Button>
                          ) : null}
                        </NameRow>
                      ))}
                    </ul>
                  </section>
                ) : null}
                {others.length > 0 ? (
                  <section>
                    <h3 className="mb-2 text-muted">{text("dnsrec.others")}</h3>
                    <ul className="flex flex-col">
                      {others.map((name) => (
                        <NameRow key={name.name} name={name}>
                          <TextButton type="button" tone="danger" onClick={() => setDrop({ zone: zone.zone, name: name.name })}>
                            {text("common.delete")}
                          </TextButton>
                        </NameRow>
                      ))}
                    </ul>
                  </section>
                ) : null}
              </Fold>
            );
          })}
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
    </>
  );
}

function NameRow({ name, children }: { name: DnsName; children?: ReactNode }) {
  const owner = name.use === "myinfra" ? `/applications/myinfra` : name.useId ? `/applications/${name.useId}` : "";
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-line py-3">
      <div className="min-w-0">
        <p className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span className="break-all font-mono text-[13px]">{name.name}</span>
          <span className={`text-[13px] ${stateTone[name.state]}`}>{text(`dnsrec.state.${name.state}` as MsgKey)}</span>
          {name.useName ? (
            owner ? (
              <Link to={owner} className="text-[13px] underline-offset-2 hover:underline">
                {name.useName}
              </Link>
            ) : (
              <span className="text-[13px]">{name.useName}</span>
            )
          ) : null}
        </p>
        <p className="mt-1 text-[13px] text-muted [overflow-wrap:anywhere]">
          {name.records.length > 0
            ? name.records.map((record) => `${record.target}${record.server ? ` (${record.server})` : ""}`).join(" · ")
            : text("dnsrec.noRecord")}
        </p>
        {name.state === "differs" || name.state === "missing" ? (
          <p className="mt-1 text-[13px] text-faint">{text("dnsrec.expected", { addresses: (name.expected ?? []).join(" · ") || "—" })}</p>
        ) : null}
      </div>
      {children}
    </li>
  );
}
