import { createContext, useContext, useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { ArrowRightLeft, LifeBuoy, PowerOff } from "lucide-react";
import { currentLocale, text, type MsgKey } from "./i18n";
import { Sheet } from "./kit";
import { useI18n } from "./lang";
import { usePoll } from "./poll";
import { useSession } from "./session";
import { Button, Icon, focus } from "./ui";

export type Operation = {
  kind: "transfer" | "deploy" | "destroy" | "update" | "rollout";
  id: string;
  name: string;
  to?: string;
  since?: string;
  clusterId?: string;
  servers?: string[];
};

// OtherCopy is the copy a transfer left: the one this copy follows, or the
// one that follows it once this copy took the lead.
export type OtherCopy = {
  url: string;
  method?: string;
  drives: boolean;
  lastOk?: string;
  lastError?: string;
};

// answers tells whether the other copy answered the sync of the last minute.
export function answers(other?: OtherCopy) {
  return !!other && !other.lastError && !!other.lastOk && Date.now() - new Date(other.lastOk).getTime() < 60_000;
}

type Activity = { local: boolean; operations: Operation[]; replica?: boolean; main?: OtherCopy };

type ActivityState = {
  activity: Activity | null;
  transferring: boolean;
  // updating tells whether an update holds a server or a cluster.
  updating: (serverId?: string, clusterId?: string) => boolean;
};

const ActivityContext = createContext<ActivityState>({ activity: null, transferring: false, updating: () => false });

// ActivityHost follows what this copy is doing on machines, every few
// seconds, for the indicator of the header, the warnings and the guards.
export function ActivityHost({ children }: { children: ReactNode }) {
  const { data } = usePoll<Activity>("/api/activity", 4000);
  const value = useMemo<ActivityState>(() => {
    const operations = data?.operations ?? [];
    return {
      activity: data,
      transferring: operations.some((op) => op.kind === "transfer"),
      updating: (serverId, clusterId) =>
        operations.some(
          (op) => (op.kind === "update" || op.kind === "rollout") && ((clusterId && op.clusterId === clusterId) || (serverId && op.servers?.includes(serverId))),
        ),
    };
  }, [data]);
  return (
    <ActivityContext.Provider value={value}>
      {children}
      <KeepOnDialog activity={data} />
    </ActivityContext.Provider>
  );
}

export function useActivity() {
  return useContext(ActivityContext);
}

function opLabel(op: Operation) {
  return text(`activity.kind.${op.kind}` as MsgKey, { name: op.name });
}

function since(op: Operation) {
  if (!op.since) return "";
  return new Date(op.since).toLocaleTimeString(currentLocale(), { hour: "2-digit", minute: "2-digit" });
}

// BusyIndicator is the red mark of the header while this copy, on the
// person's computer, drives machines: switching it off would cut them.
export function BusyIndicator() {
  useI18n();
  const { activity } = useActivity();
  const [open, setOpen] = useState(false);
  const tipId = useId();
  const ops = activity?.operations ?? [];
  if (!activity?.local || ops.length === 0) return null;
  return (
    <div className="relative" onMouseEnter={() => setOpen(true)} onMouseLeave={() => setOpen(false)}>
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        onBlur={() => setOpen(false)}
        aria-describedby={tipId}
        aria-expanded={open}
        className={`inline-flex items-center gap-2 rounded-full border border-bad px-2.5 py-1 text-[12px] text-bad ${focus}`}
      >
        <span className="relative inline-flex size-2.5">
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-bad opacity-60" aria-hidden />
          <span className="relative inline-flex size-2.5 rounded-full bg-bad" aria-hidden />
        </span>
        <span className="hidden sm:inline">{text("activity.keepOn")}</span>
        <span className="sm:hidden">{ops.length}</span>
      </button>
      <div
        id={tipId}
        role="tooltip"
        className={`absolute right-0 top-full z-50 mt-2 w-72 rounded-md border border-line bg-surface p-3 text-[13px] shadow-sm ${open ? "block" : "hidden"}`}
      >
        <p className="mb-2 font-medium text-bad">{text("activity.keepOnTitle")}</p>
        <p className="mb-2 text-muted">{text("activity.keepOnText")}</p>
        <ul className="flex flex-col gap-1">
          {ops.map((op) => (
            <li key={op.kind + op.id} className="flex justify-between gap-3">
              {op.to ? (
                <Link to={op.to} className="truncate underline-offset-2 hover:underline">
                  {opLabel(op)}
                </Link>
              ) : (
                <span className="truncate">{opLabel(op)}</span>
              )}
              <span className="shrink-0 text-faint">{since(op)}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

const seenKey = "myinfra-keep-on-seen";

function seenIds(): Set<string> {
  try {
    return new Set(JSON.parse(sessionStorage.getItem(seenKey) || "[]") as string[]);
  } catch {
    return new Set();
  }
}

// KeepOnDialog warns, the moment an operation starts on a copy running on
// the person's computer, not to switch it off until the operation ends.
function KeepOnDialog({ activity }: { activity: Activity | null }) {
  useI18n();
  const [shown, setShown] = useState<Operation[]>([]);
  useEffect(() => {
    if (!activity?.local) return;
    const seen = seenIds();
    const fresh = activity.operations.filter((op) => !seen.has(op.kind + op.id));
    if (fresh.length === 0) return;
    for (const op of fresh) seen.add(op.kind + op.id);
    try {
      sessionStorage.setItem(seenKey, JSON.stringify([...seen].slice(-200)));
    } catch {
      // Storage blocked: the warning may come back on reload.
    }
    setShown(fresh);
  }, [activity]);
  if (shown.length === 0) return null;
  return (
    <Sheet title={text("activity.keepOnTitle")} onClose={() => setShown([])}>
      <div className="flex flex-col gap-4">
        <p className="flex items-start gap-3">
          <Icon icon={PowerOff} className="mt-0.5 shrink-0 text-bad" />
          <span>{text("activity.keepOnText")}</span>
        </p>
        <ul className="flex flex-col gap-1 text-muted">
          {shown.map((op) => (
            <li key={op.kind + op.id}>{opLabel(op)}</li>
          ))}
        </ul>
        <p className="text-[13px] text-faint">{text("activity.keepOnHint")}</p>
        <div>
          <Button type="button" onClick={() => setShown([])} data-autofocus>
            {text("activity.understood")}
          </Button>
        </div>
      </div>
    </Sheet>
  );
}

// TransferBanner tells, on every page, that a transfer is running and that
// changes wait for its end.
export function TransferBanner() {
  useI18n();
  const { transferring } = useActivity();
  const { pathname } = useLocation();
  const { canAdmin } = useSession();
  if (!transferring || pathname.startsWith("/transfert")) return null;
  return (
    <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-md border border-line bg-status px-4 py-3">
      <p className="flex items-center gap-2">
        <Icon icon={ArrowRightLeft} />
        {text("activity.transferBanner")}
      </p>
      {canAdmin ? (
        <Link to="/transfert/suivi" className="underline">
          {text("activity.follow")}
        </Link>
      ) : null}
    </div>
  );
}

// RunningList shows the operations in progress at the top of the activity.
export function RunningList() {
  useI18n();
  const { activity } = useActivity();
  const ops = activity?.operations ?? [];
  if (ops.length === 0) return null;
  return (
    <section className="mb-10">
      <h2 className="mb-3 text-lg">{text("activity.running")}</h2>
      <ul className="flex flex-col">
        {ops.map((op) => (
          <li key={op.kind + op.id} className="flex items-center justify-between gap-4 border-t border-line py-3">
            <span className="flex items-center gap-3">
              <span className="inline-block size-2 animate-pulse rounded-full bg-warn" aria-hidden />
              {op.to ? (
                <Link to={op.to} className="underline-offset-2 hover:underline">
                  {opLabel(op)}
                </Link>
              ) : (
                opLabel(op)
              )}
            </span>
            <span className="text-[13px] text-faint">{since(op)}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

// UpdateLock says on the page of an application or a server that an update
// holds it: its changes wait for the end, removing it stays possible.
export function UpdateLock({ locked }: { locked: boolean }) {
  useI18n();
  if (!locked) return null;
  return (
    <p className="mt-6 flex items-start gap-3 rounded-md border border-line bg-status px-4 py-3 text-[14px]">
      <span className="mt-1.5 inline-block size-2 shrink-0 animate-pulse rounded-full bg-warn" aria-hidden />
      {text("activity.updateLock")}
    </p>
  );
}

// ReplicaBanner tells, on every page of a copy that follows another, which
// copy drives the machines, and offers the lead when that one stops
// answering.
export function ReplicaBanner() {
  useI18n();
  const { activity } = useActivity();
  const { pathname } = useLocation();
  if (!activity?.replica || pathname.startsWith("/copies")) return null;
  const main = activity.main;
  const down = !!main && !answers(main) && !!main.lastError;
  return (
    <div className={`mb-6 flex flex-wrap items-center justify-between gap-3 rounded-md border px-4 py-3 ${down ? "border-bad" : "border-line bg-status"}`}>
      <p className="flex items-start gap-2">
        <Icon icon={LifeBuoy} className={`mt-0.5 shrink-0 ${down ? "text-bad" : ""}`} />
        <span>{main ? text(down ? "lead.bannerDown" : "lead.banner", { url: main.url }) : text("lead.bannerAway")}</span>
      </p>
      <Link to="/copies" className="underline">
        {text(down ? "lead.take" : "lead.manage")}
      </Link>
    </div>
  );
}
