import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { PageTitle, TextButton, focus } from "./ui";
import { text } from "./i18n";

export function ServerTerminal() {
  const { id = "" } = useParams();
  return <TerminalView title={text("server.terminal")} socket={`/api/servers/${id}/terminal`} />;
}

export function PodTerminal() {
  const { id = "", pod = "" } = useParams();
  return <TerminalView title={text("server.terminal")} socket={`/api/applications/${id}/pods/${encodeURIComponent(pod)}/terminal`} />;
}

type Link = "connecting" | "open" | "closed" | "failed";

// Keys a phone keyboard does not have, sent as a terminal expects them.
const keys: { label: string; data: string }[] = [
  { label: "Esc", data: "\x1b" },
  { label: "Tab", data: "\t" },
  { label: "←", data: "\x1b[D" },
  { label: "↑", data: "\x1b[A" },
  { label: "↓", data: "\x1b[B" },
  { label: "→", data: "\x1b[C" },
];

function TerminalView({ title, socket }: { title: string; socket: string }) {
  const host = useRef<HTMLDivElement>(null);
  const send = useRef<(data: string) => void>(() => undefined);
  const ctrl = useRef(false);
  const [ctrlOn, setCtrlOn] = useState(false);
  const [link, setLink] = useState<Link>("connecting");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const node = host.current;
    if (!node) return;
    setLink("connecting");
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: '"Geist Mono Variable", ui-monospace, monospace',
      fontSize: window.matchMedia("(max-width: 639px)").matches ? 12 : 13,
      theme: { background: "#1c1917", foreground: "#fafaf9", cursor: "#fafaf9" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(node);
    fit.fit();
    let cancelled = false;
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const conn = new WebSocket(`${protocol}//${window.location.host}${socket}`);
    conn.binaryType = "arraybuffer";
    const sendSize = () => {
      fit.fit();
      const bytes = new Uint8Array(5);
      bytes[0] = 1;
      bytes[1] = term.cols >> 8;
      bytes[2] = term.cols & 0xff;
      bytes[3] = term.rows >> 8;
      bytes[4] = term.rows & 0xff;
      if (conn.readyState === WebSocket.OPEN) conn.send(bytes);
    };
    let received = false;
    conn.onopen = () => {
      setLink("open");
      sendSize();
      term.focus();
    };
    conn.onmessage = (event) => {
      received = true;
      if (typeof event.data === "string") {
        term.write(event.data);
        return;
      }
      term.write(new Uint8Array(event.data as ArrayBuffer));
    };
    conn.onclose = () => {
      if (!cancelled) setLink(received ? "closed" : "failed");
    };
    send.current = (data: string) => {
      let out = data;
      // Ctrl from the key bar applies to the next letter typed.
      if (ctrl.current && data.length === 1 && /[a-z@[\\\]^_]/i.test(data)) {
        out = String.fromCharCode(data.toUpperCase().charCodeAt(0) & 0x1f);
        ctrl.current = false;
        setCtrlOn(false);
      }
      const encoded = new TextEncoder().encode(out);
      const bytes = new Uint8Array(encoded.length + 1);
      bytes[0] = 0;
      bytes.set(encoded, 1);
      if (conn.readyState === WebSocket.OPEN) conn.send(bytes);
    };
    const input = term.onData((data) => send.current(data));
    const resized = new ResizeObserver(sendSize);
    resized.observe(node);
    return () => {
      cancelled = true;
      input.dispose();
      resized.disconnect();
      conn.close();
      term.dispose();
    };
  }, [socket, attempt]);

  async function paste() {
    try {
      send.current(await navigator.clipboard.readText());
    } catch {
      // Reading the clipboard was refused: the system paste still works.
    }
  }

  const state =
    link === "open" ? { tone: "text-ok", label: text("terminal.open") } :
    link === "connecting" ? { tone: "text-muted", label: text("terminal.connecting") } :
    link === "closed" ? { tone: "text-muted", label: text("terminal.closed") } :
    { tone: "text-bad", label: text("ui.laConnexionAuTerminalAEchoue") };

  return (
    <>
      <PageTitle
        title={title}
        action={
          <span className="flex flex-wrap items-center gap-4" role="status">
            <span className={state.tone}>{state.label}</span>
            {link === "closed" || link === "failed" ? (
              <TextButton type="button" onClick={() => setAttempt((current) => current + 1)}>
                {text("terminal.reconnect")}
              </TextButton>
            ) : null}
          </span>
        }
      />
      <div className="-mx-4 flex h-[calc(100dvh-17rem-env(safe-area-inset-bottom))] flex-col md:mx-0 md:h-[32rem]">
        <div ref={host} className="min-h-0 flex-1 overflow-hidden border-y border-line bg-[#1c1917] md:rounded-md md:border" />
        <div className="flex gap-1 overflow-x-auto border-b border-line bg-surface p-1 md:hidden" aria-label={text("terminal.keys")}>
          {keys.map((key) => (
            <button key={key.label} type="button" onClick={() => send.current(key.data)} className={`min-h-10 min-w-11 shrink-0 rounded-md border border-line px-2 font-mono text-[13px] ${focus}`}>
              {key.label}
            </button>
          ))}
          <button
            type="button"
            aria-pressed={ctrlOn}
            onClick={() => {
              ctrl.current = !ctrl.current;
              setCtrlOn(ctrl.current);
            }}
            className={`min-h-10 min-w-11 shrink-0 rounded-md border px-2 font-mono text-[13px] ${ctrlOn ? "border-ink bg-ink text-canvas" : "border-line"} ${focus}`}
          >
            Ctrl
          </button>
          <button type="button" onClick={() => void paste()} className={`min-h-10 shrink-0 rounded-md border border-line px-3 text-[13px] ${focus}`}>
            {text("terminal.paste")}
          </button>
        </div>
      </div>
    </>
  );
}
