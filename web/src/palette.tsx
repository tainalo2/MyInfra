import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useNavigate } from "react-router-dom";
import { Search } from "lucide-react";
import { api, type Application, type Cluster, type Server } from "./api";
import { text, type MsgKey } from "./i18n";
import { useI18n } from "./lang";
import { Icon } from "./ui";

const openEvent = "myinfra:palette";

type Entry = { key: string; kind: MsgKey; label: string; note?: string; to: string };

const pages: { label: MsgKey; to: string }[] = [
  { label: "nav.overview", to: "/" },
  { label: "nav.apps", to: "/applications" },
  { label: "crumb.newApp", to: "/nouveau" },
  { label: "nav.clusters", to: "/clusters" },
  { label: "nav.history", to: "/historique" },
  { label: "nav.templates", to: "/templates" },
  { label: "nav.providers", to: "/providers" },
  { label: "nav.dns", to: "/dns" },
  { label: "nav.git", to: "/git" },
  { label: "nav.ssh", to: "/cles" },
  { label: "nav.secrets", to: "/secrets" },
  { label: "nav.transfer", to: "/transfert" },
  { label: "nav.copies", to: "/copies" },
  { label: "nav.account", to: "/compte" },
];

function clean(value: string) {
  return value.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();
}

export function SearchButton() {
  useI18n();
  const mac = /Mac|iPhone|iPad/.test(navigator.platform);
  return (
    <button
      type="button"
      onClick={() => window.dispatchEvent(new Event(openEvent))}
      aria-label={text("palette.open")}
      aria-keyshortcuts={mac ? "Meta+K" : "Control+K"}
      className="inline-flex items-center gap-2 rounded-md border border-line px-2 py-1 text-muted focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-2 focus-visible:outline-focus"
    >
      <Icon icon={Search} />
      <span className="hidden sm:inline">{text("palette.search")}</span>
      <kbd className="hidden rounded border border-line px-1 font-sans text-[12px] sm:inline">{mac ? "⌘K" : "Ctrl K"}</kbd>
    </button>
  );
}

// Palette jumps to an app, a server, a cluster or a page from anywhere.
export function Palette() {
  useI18n();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [entries, setEntries] = useState<Entry[]>([]);
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const before = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const show = () => {
      before.current = document.activeElement as HTMLElement | null;
      setOpen(true);
    };
    const onKey = (event: globalThis.KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        show();
      }
    };
    window.addEventListener(openEvent, show);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener(openEvent, show);
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setActive(0);
    input.current?.focus();
    let stop = false;
    const fixed: Entry[] = pages.map((page) => ({ key: `page:${page.to}`, kind: "palette.page", label: text(page.label), to: page.to }));
    setEntries(fixed);
    Promise.all([
      api<Application[]>("/api/applications").catch(() => []),
      api<Server[]>("/api/servers").catch(() => []),
      api<Cluster[]>("/api/clusters").catch(() => []),
    ]).then(([apps, servers, clusters]) => {
      if (stop) return;
      setEntries([
        ...apps.map((app) => ({ key: `app:${app.id}`, kind: "palette.app" as MsgKey, label: app.name, note: app.hostname, to: `/applications/${app.id}` })),
        ...servers.map((server) => ({ key: `server:${server.id}`, kind: "palette.server" as MsgKey, label: server.name, note: server.clusterName || server.address, to: `/serveurs/${server.id}` })),
        ...clusters.map((cluster) => ({ key: `cluster:${cluster.id}`, kind: "palette.cluster" as MsgKey, label: cluster.name, to: "/clusters" })),
        ...fixed,
      ]);
    });
    return () => {
      stop = true;
    };
  }, [open]);

  function close() {
    setOpen(false);
    before.current?.focus();
  }

  if (!open) return null;

  const wanted = clean(query.trim());
  const found = entries.filter((entry) => !wanted || clean(`${entry.label} ${entry.note ?? ""}`).includes(wanted)).slice(0, 12);
  const current = Math.min(active, Math.max(0, found.length - 1));

  function go(entry: Entry | undefined) {
    if (!entry) return;
    setOpen(false);
    navigate(entry.to);
  }

  function onKey(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      close();
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive((current + 1) % Math.max(1, found.length));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((current - 1 + found.length) % Math.max(1, found.length));
    } else if (event.key === "Enter") {
      event.preventDefault();
      go(found[current]);
    } else if (event.key === "Tab") {
      event.preventDefault();
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/30 px-4 pt-[12vh]" onMouseDown={close}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={text("palette.title")}
        className="w-full max-w-[560px] overflow-hidden rounded-md border border-line bg-surface shadow-lg"
        onMouseDown={(event) => event.stopPropagation()}
        onKeyDown={onKey}
      >
        <label className="flex items-center gap-3 border-b border-line px-4 py-3">
          <Icon icon={Search} className="text-muted" />
          <input
            ref={input}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setActive(0);
            }}
            placeholder={text("palette.placeholder")}
            aria-label={text("palette.placeholder")}
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-results"
            aria-activedescendant={found[current] ? `palette-${current}` : undefined}
            className="min-w-0 flex-1 bg-transparent outline-none"
          />
        </label>
        <ul id="palette-results" role="listbox" className="max-h-[50vh] overflow-y-auto p-1">
          {found.length === 0 ? <li className="px-3 py-2 text-muted">{text("palette.empty")}</li> : null}
          {found.map((entry, index) => (
            <li
              key={entry.key}
              id={`palette-${index}`}
              role="option"
              aria-selected={index === current}
              onMouseEnter={() => setActive(index)}
              onClick={() => go(entry)}
              className={`flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 ${index === current ? "bg-status" : ""}`}
            >
              <span className="w-16 shrink-0 text-muted">{text(entry.kind)}</span>
              <span className="min-w-0 flex-1 truncate">{entry.label}</span>
              {entry.note ? <span className="shrink-0 truncate text-muted">{entry.note}</span> : null}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
