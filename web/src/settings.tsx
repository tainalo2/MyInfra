import { useEffect, useState, type ReactNode } from "react";
import { ArrowRightLeft, Bot, Cloud, Globe, HardDrive, KeyRound, Shield, UserRound, Users, Waypoints, type LucideIcon } from "lucide-react";
import { api, type ApiToken, type LocalImage, type Secret } from "./api";
import { langNames, langs, text, type MsgKey } from "./i18n";
import { ConfirmDialog } from "./kit";
import { useI18n } from "./lang";
import { usePoll } from "./poll";
import { ListRow, RowGroup, RowState } from "./rows";
import { useSession } from "./session";
import { useTheme, type ThemeChoice } from "./theme";
import { Input, PageTitle, Select, TextButton, focus, readListMode, saveListMode, type ListMode } from "./ui";
import { useUpdates } from "./updates";

type Tone = "ok" | "warn" | "muted";
type Entry = { to: string; label: MsgKey; hint: MsgKey; icon: LucideIcon; adminOnly?: boolean; deployOnly?: boolean; state?: { label: ReactNode; tone: Tone } };

function plural(count: number, one: MsgKey, many: MsgKey) {
  return count === 1 ? text(one) : text(many, { count });
}

// Counts are read once: a settings row says what is there, not live news.
function useCounts(canDeploy: boolean, canAdmin: boolean) {
  const [counts, setCounts] = useState<{ secrets?: Secret[]; images?: LocalImage[]; tokens?: ApiToken[]; users?: number; appSecrets?: number }>({});
  useEffect(() => {
    let stop = false;
    const put = (part: Partial<typeof counts>) => {
      if (!stop) setCounts((current) => ({ ...current, ...part }));
    };
    if (canDeploy) {
      api<Secret[]>("/api/secrets").then((secrets) => put({ secrets })).catch(() => undefined);
      api<LocalImage[]>("/api/images").then((images) => put({ images })).catch(() => undefined);
      api<unknown[]>("/api/app-secrets").then((rows) => put({ appSecrets: rows.length })).catch(() => undefined);
    }
    api<ApiToken[]>("/api/mcp/tokens").then((tokens) => put({ tokens })).catch(() => undefined);
    if (canAdmin) api<unknown[]>("/api/users").then((rows) => put({ users: rows.length })).catch(() => undefined);
    return () => {
      stop = true;
    };
  }, [canDeploy, canAdmin]);
  return counts;
}

export function SettingsPage() {
  useI18n();
  const { canDeploy, canAdmin, totp } = useSession();
  const counts = useCounts(canDeploy, canAdmin);
  const [query, setQuery] = useState("");
  const kinds = (kind: string) => counts.secrets?.filter((secret) => secret.kind === kind).length;
  const count = (value: number | undefined, one: MsgKey, many: MsgKey, tone: Tone = "muted") =>
    value === undefined ? undefined : { label: value === 0 ? text("count.none") : plural(value, one, many), tone: value === 0 ? "muted" : tone };
  const registries = kinds("registry_token");
  const images = counts.images?.length;
  const imageState =
    registries === undefined || images === undefined
      ? undefined
      : { label: [registries ? plural(registries, "count.registries1", "count.registriesN") : "", images ? plural(images, "count.images1", "count.imagesN") : ""].filter(Boolean).join(" · ") || text("count.none"), tone: "muted" as Tone };

  const groups: { id: string; title: MsgKey; entries: Entry[] }[] = [
    {
      id: "connexions",
      title: "settings.connect",
      entries: [
        { to: "/providers", label: "nav.providers", hint: "settings.providersHint", icon: Cloud, deployOnly: true, state: count(kinds("provider_token"), "count.accounts1", "count.accountsN", "ok") },
        { to: "/dns", label: "nav.dns", hint: "settings.domainsHint", icon: Globe, deployOnly: true, state: count(kinds("dns_token"), "count.accounts1", "count.accountsN", "ok") },
        { to: "/images", label: "nav.images", hint: "settings.imagesAllHint", icon: HardDrive, deployOnly: true, state: imageState },
        { to: "/assistants", label: "nav.mcp", hint: "settings.mcpHint", icon: Bot, state: count(counts.tokens?.length, "count.tokens1", "count.tokensN", "ok") },
      ],
    },
    {
      id: "acces",
      title: "settings.security",
      entries: [
        { to: "/compte", label: "settings.accountRow", hint: "settings.accountRowHint", icon: UserRound, state: { label: <><span className="md:hidden">{text(totp ? "settings.totpOnShort" : "settings.totpOffShort")}</span><span className="hidden md:inline">{text(totp ? "settings.totpOn" : "settings.totpOff")}</span></>, tone: totp ? "ok" : "muted" } },
        { to: "/cles", label: "nav.ssh", hint: "settings.sshHint", icon: KeyRound, deployOnly: true, state: count(kinds("ssh_key"), "count.keys1", "count.keysN") },
        { to: "/secrets", label: "nav.secrets", hint: "settings.secretsHint", icon: Shield, deployOnly: true, state: count(counts.appSecrets, "count.secrets1", "count.secretsN") },
        { to: "/utilisateurs", label: "nav.users", hint: "settings.usersHint", icon: Users, adminOnly: true, state: count(counts.users, "count.accounts1", "count.accountsN") },
      ],
    },
  ];

  const needle = query.trim().toLowerCase();
  const matches = (...words: string[]) => !needle || words.some((word) => word.toLowerCase().includes(needle));
  const shown = groups
    .map((group) => ({
      ...group,
      entries: group.entries.filter((entry) => visible(entry, canDeploy, canAdmin) && matches(text(entry.label), text(entry.hint))),
    }))
    .filter((group) => group.entries.length > 0);
  const prefs = matches(text("settings.prefs"), text("header.lang"), text("settings.theme"), text("settings.lists"));
  const mine = matches("MyInfra", text("nav.transfer"), text("nav.copies"), text("settings.stop"), text("nav.updates"));
  const parts = [...shown.map((group) => ({ id: group.id, title: text(group.title) })), ...(prefs ? [{ id: "preferences", title: text("settings.prefs") }] : []), ...(mine ? [{ id: "myinfra", title: "MyInfra" }] : [])];

  return (
    <>
      <PageTitle
        title={text("nav.settings")}
        action={
          <div className="w-full sm:w-[300px]">
            <Input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={text("settings.search")} aria-label={text("settings.search")} />
          </div>
        }
      />
      <div className="flex flex-wrap items-start gap-8">
        <nav aria-label={text("settings.parts")} className="sticky top-0 hidden max-w-[180px] flex-[1_1_160px] flex-col gap-0.5 md:flex">
          {parts.map((part) => (
            <a key={part.id} href={`#${part.id}`} className={`rounded-md px-2.5 py-1.5 text-muted ${focus}`}>
              {part.title}
            </a>
          ))}
        </nav>
        <div className="flex min-w-0 flex-[999_1_480px] flex-col gap-10">
          {shown.map((group) => (
            <RowGroup key={group.id} id={group.id} title={text(group.title)}>
              {group.entries.map((entry) => (
                <ListRow
                  key={entry.to}
                  to={entry.to}
                  icon={entry.icon}
                  phoneTile
                  title={text(entry.label)}
                  sub={<span className="hidden md:inline">{text(entry.hint)}</span>}
                  end={entry.state ? <RowState tone={entry.state.tone}>{entry.state.label}</RowState> : null}
                />
              ))}
            </RowGroup>
          ))}
          {prefs ? <Preferences /> : null}
          {mine ? <MyInfraPart canAdmin={canAdmin} /> : null}
          {parts.length === 0 ? <p className="text-muted">{text("settings.noMatch")}</p> : null}
        </div>
      </div>
    </>
  );
}

function visible(entry: Entry, canDeploy: boolean, canAdmin: boolean) {
  if (entry.adminOnly && !canAdmin) return false;
  if (entry.deployOnly && !canDeploy) return false;
  return true;
}

function PrefRow({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3.5">
      <span className="flex flex-col">
        <span>{label}</span>
        {hint ? <span className="text-[13px] text-muted">{hint}</span> : null}
      </span>
      {children}
    </div>
  );
}

function Preferences() {
  const { lang, setLang } = useI18n();
  const { choice, choose } = useTheme();
  const [list, setList] = useState<ListMode>(readListMode);
  const themes: { key: ThemeChoice; label: MsgKey }[] = [
    { key: "light", label: "theme.light" },
    { key: "dark", label: "theme.dark" },
    { key: "system", label: "theme.system" },
  ];
  return (
    <RowGroup id="preferences" title={text("settings.prefs")}>
      <PrefRow label={text("header.lang")} hint={text("settings.langHint")}>
        <div className="w-full sm:w-[200px]">
          <Select aria-label={text("header.lang")} value={lang} onChange={(event) => setLang(event.target.value as (typeof langs)[number])}>
            {langs.map((code) => (
              <option key={code} value={code}>
                {langNames[code]}
              </option>
            ))}
          </Select>
        </div>
      </PrefRow>
      <PrefRow label={text("settings.theme")}>
        <SegmentedControl label={text("settings.theme")} value={choice} onChange={choose} options={themes.map((item) => ({ key: item.key, label: text(item.label) }))} />
      </PrefRow>
      <PrefRow label={text("settings.lists")} hint={text("settings.listsHint")}>
        <div className="w-full sm:w-[200px]">
          <Select
            aria-label={text("settings.lists")}
            value={list}
            onChange={(event) => {
              const next = event.target.value as ListMode;
              setList(next);
              saveListMode(next);
            }}
          >
            <option value="cards">{text("list.asCards")}</option>
            <option value="rows">{text("list.asRows")}</option>
          </Select>
        </div>
      </PrefRow>
    </RowGroup>
  );
}

// SegmentedControl picks one of a few values: the parts of an object, a theme.
export function SegmentedControl<K extends string>({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: K;
  onChange: (key: K) => void;
  options: { key: K; label: string }[];
}) {
  return (
    <div role="radiogroup" aria-label={label} className="grid gap-1 rounded-md bg-status p-1" style={{ gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))` }}>
      {options.map((option) => (
        <button
          key={option.key}
          type="button"
          role="radio"
          aria-checked={value === option.key}
          onClick={() => onChange(option.key)}
          className={`rounded-md px-3.5 py-1 ${value === option.key ? "bg-surface text-ink" : "text-muted"} ${focus}`}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function MyInfraPart({ canAdmin }: { canAdmin: boolean }) {
  const { view } = useUpdates();
  const version = view?.version;
  const sync = usePoll<{ peers: unknown[] }>(canAdmin ? "/api/sync" : null, 0).data;
  const next = version?.latest && version.latest !== version.current && view?.updates.some((item) => item.kind === "myinfra") ? version.latest : "";
  const peers = sync?.peers.length;
  return (
    <section id="myinfra" className="flex flex-col gap-4">
      <RowGroup title="MyInfra">
        {version ? (
          <ListRow
            to="/mises-a-jour"
            chevron={!next}
            title={text("settings.version", { version: version.current })}
            sub={next ? text("settings.versionNew", { version: next }) : text("settings.versionCurrent")}
            end={next ? <RowState tone="link">{text("settings.seeUpdates")}</RowState> : null}
          />
        ) : null}
        {canAdmin ? <ListRow to="/transfert" icon={ArrowRightLeft} phoneTile title={text("nav.transfer")} sub={<span className="hidden md:inline">{text("settings.transferHint")}</span>} /> : null}
        {canAdmin ? (
          <ListRow
            to="/copies"
            icon={Waypoints}
            phoneTile
            title={text("nav.copies")}
            sub={<span className="hidden md:inline">{text("settings.copiesHint")}</span>}
            end={peers === undefined ? null : <RowState>{peers === 0 ? text("copies.none") : text("copies.count", { count: peers + 1 })}</RowState>}
          />
        ) : null}
      </RowGroup>
      {canAdmin ? <StopMyInfra /> : null}
    </section>
  );
}

// MyInfra.app on macOS runs without a window: this is how it stops.
function StopMyInfra() {
  const { view } = useUpdates();
  const [confirming, setConfirming] = useState(false);
  const [stopped, setStopped] = useState(false);
  if (!view?.version.stoppable) return null;

  async function stop() {
    await api("/api/myinfra/stop", { method: "POST" });
    setStopped(true);
    setConfirming(false);
  }

  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 rounded-md border border-line px-4 py-3.5">
      <span className="flex flex-col">
        <span>{text("settings.stop")}</span>
        <span className="text-[13px] text-muted">{text(stopped ? "settings.stopped" : "settings.stopRowHint")}</span>
      </span>
      {stopped ? null : (
        <TextButton type="button" tone="danger" onClick={() => setConfirming(true)}>
          {text("settings.stopShort")}
        </TextButton>
      )}
      {confirming ? (
        <ConfirmDialog title={text("settings.stop")} confirmLabel={text("settings.stopConfirm")} danger onConfirm={stop} onClose={() => setConfirming(false)}>
          {text("settings.stopRowHint")}
        </ConfirmDialog>
      ) : null}
    </div>
  );
}

