import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "./api";
import { TextButton } from "./ui";
import { text as say } from "./i18n";

export function PodActions({ appId, name, failed }: { appId: string; name: string; failed: boolean }) {
  const [open, setOpen] = useState(failed);
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  const [resetting, setResetting] = useState(false);

  useEffect(() => {
    if (!open) return;
    let stop = false;
    api<{ text: string }>(`/api/applications/${appId}/pods/${encodeURIComponent(name)}/logs`)
      .then((body) => {
        if (!stop) setText(body.text);
      })
      .catch((err) => {
        if (!stop) setError(err instanceof Error ? err.message : say("common.error"));
      });
    return () => {
      stop = true;
    };
  }, [open, appId, name]);

  async function reset() {
    setResetting(true);
    setError("");
    try {
      await api(`/api/applications/${appId}/pods/${encodeURIComponent(name)}/reset`, { method: "POST" });
      setText("");
      setOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : say("common.error"));
    } finally {
      setResetting(false);
    }
  }

  return (
    <div className="mt-3 flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-4">
        <TextButton type="button" onClick={() => setOpen((value) => !value)}>
          {open ? say("ui.masquerLeJournal") : "Journal"}
        </TextButton>
        <TextButton type="button" onClick={() => void reset()} disabled={resetting}>
          {say("ui.relancer")}
        </TextButton>
        <Link to={`/applications/${appId}/terminal/${encodeURIComponent(name)}`} className="text-muted underline">
          Terminal
        </Link>
      </div>
      {error ? <p className="text-bad">{error}</p> : null}
      {open ? (
        <pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded-md border border-line bg-canvas p-3 font-mono text-[13px]">
          {text || say("ui.lectureDuJournal")}
        </pre>
      ) : null}
    </div>
  );
}
