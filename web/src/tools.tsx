import { useEffect, useState } from "react";
import { api } from "./api";
import { Button, ErrorText } from "./ui";
import { text } from "./i18n";

type HostTools = { ansible: boolean; tofu: boolean; tofuEnabled: boolean };

export function ToolInstall({ ansible, tofu, onChange }: { ansible?: boolean; tofu?: boolean; onChange?: (tools: HostTools) => void }) {
  const [tools, setTools] = useState<HostTools | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");

  useEffect(() => {
    if (!ansible && !tofu) return;
    let cancel = false;
    api<HostTools>("/api/tools")
      .then((next) => {
        if (!cancel) {
          setTools(next);
          onChange?.(next);
        }
      })
      .catch(() => {});
    return () => {
      cancel = true;
    };
  }, [ansible, tofu, onChange]);

  if (!tools) return null;
  const showAnsible = Boolean(ansible) && !tools.ansible;
  const showTofu = Boolean(tofu) && tools.tofuEnabled && !tools.tofu;
  if (!showAnsible && !showTofu) return null;

  async function install(name: "ansible" | "tofu") {
    setBusy(name);
    setError("");
    try {
      const next = await api<HostTools>(`/api/tools/${name}`, { method: "POST" });
      setTools(next);
      onChange?.(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="flex flex-col items-start gap-3">
      {error ? <ErrorText>{error}</ErrorText> : null}
      {showAnsible ? (
        <Button type="button" onClick={() => void install("ansible")} disabled={busy !== ""}>
          {busy === "ansible" ? "Installation…" : "Installer Ansible"}
        </Button>
      ) : null}
      {showTofu ? (
        <Button type="button" onClick={() => void install("tofu")} disabled={busy !== ""}>
          {busy === "tofu" ? "Installation…" : "Installer OpenTofu"}
        </Button>
      ) : null}
    </div>
  );
}
