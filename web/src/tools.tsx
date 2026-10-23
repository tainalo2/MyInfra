import { useEffect, useState } from "react";
import { api } from "./api";
import { Button, ErrorText } from "./ui";
import { text } from "./i18n";

type HostTools = { ansible: boolean; tofu: boolean; tofuEnabled: boolean; helm?: boolean };

export function ToolInstall({ ansible, tofu, helm, onChange }: { ansible?: boolean; tofu?: boolean; helm?: boolean; onChange?: (tools: HostTools) => void }) {
  const [tools, setTools] = useState<HostTools | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");

  useEffect(() => {
    if (!ansible && !tofu && !helm) return;
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
  }, [ansible, tofu, helm, onChange]);

  if (!tools) return null;
  const showAnsible = Boolean(ansible) && !tools.ansible;
  const showTofu = Boolean(tofu) && tools.tofuEnabled && !tools.tofu;
  const showHelm = Boolean(helm) && !tools.helm;
  if (!showAnsible && !showTofu && !showHelm) return null;

  async function install(name: "ansible" | "tofu" | "helm") {
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
      {showHelm ? (
        <Button type="button" onClick={() => void install("helm")} disabled={busy !== ""}>
          {busy === "helm" ? "Installation…" : "Installer Helm"}
        </Button>
      ) : null}
    </div>
  );
}
