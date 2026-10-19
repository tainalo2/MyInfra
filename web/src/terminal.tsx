import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { PageTitle } from "./ui";
import { text } from "./i18n";

export function ServerTerminal() {
  const { id = "" } = useParams();
  return <TerminalView title="Terminal" socket={`/api/servers/${id}/terminal`} />;
}

export function PodTerminal() {
  const { id = "", pod = "" } = useParams();
  return <TerminalView title="Terminal" socket={`/api/applications/${id}/pods/${encodeURIComponent(pod)}/terminal`} />;
}

function TerminalView({ title, socket }: { title: string; socket: string }) {
  const host = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState("");

  useEffect(() => {
    const node = host.current;
    if (!node) return;
    setFailed("");
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: '"Geist Mono Variable", ui-monospace, monospace',
      fontSize: 13,
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
    conn.onopen = sendSize;
    conn.onmessage = (event) => {
      received = true;
      if (typeof event.data === "string") {
        term.write(event.data);
        return;
      }
      term.write(new Uint8Array(event.data as ArrayBuffer));
    };
    conn.onclose = () => {
      if (!cancelled && !received) setFailed(text("ui.laConnexionAuTerminalAEchoue"));
    };
    const input = term.onData((data) => {
      const encoded = new TextEncoder().encode(data);
      const bytes = new Uint8Array(encoded.length + 1);
      bytes[0] = 0;
      bytes.set(encoded, 1);
      if (conn.readyState === WebSocket.OPEN) conn.send(bytes);
    });
    const resized = new ResizeObserver(sendSize);
    resized.observe(node);
    return () => {
      cancelled = true;
      input.dispose();
      resized.disconnect();
      conn.close();
      term.dispose();
    };
  }, [socket]);

  return (
    <>
      <PageTitle title={title} />
      {failed ? <p className="mb-4 text-bad">{failed}</p> : null}
      <div ref={host} className="h-[32rem] overflow-hidden rounded-md border border-line" />
    </>
  );
}
