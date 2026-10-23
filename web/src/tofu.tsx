import { useEffect, useState } from "react";
import { api } from "./api";
import { Spinner } from "./kit";
import { Button, ErrorText, TextButton } from "./ui";
import { text } from "./i18n";
import { Code, languageOf } from "./code-lazy";

export function TofuFiles({ path, readOnly = false }: { path: string; readOnly?: boolean }) {
  const [files, setFiles] = useState<Record<string, string> | null>(null);
  const [name, setName] = useState("main.tf.json");
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let stop = false;
    void api<{ files: Record<string, string> }>(path).then((body) => {
      if (stop) return;
      const first = Object.keys(body.files)[0] ?? "main.tf.json";
      setFiles(body.files);
      setName(first);
      setDraft(body.files[first] ?? "");
    }).catch(() => {
      if (!stop) setFiles(null);
    });
    return () => {
      stop = true;
    };
  }, [path]);

  if (!files) return null;

  async function save() {
    setBusy(true);
    setError("");
    const next = { ...files, [name]: draft };
    try {
      await api(path, { method: "PUT", body: JSON.stringify({ files: next }) });
      setFiles(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("ui.enregistrementRefuse"));
    } finally {
      setBusy(false);
    }
  }

  function load(file: File) {
    void file.text().then((text) => {
      const chosen = file.name.endsWith(".tf") || file.name.endsWith(".tf.json") ? file.name : "main.tf.json";
      const next = { ...files, [chosen]: text };
      setFiles(next);
      setName(chosen);
      setDraft(next[chosen] ?? "");
    });
  }

  if (readOnly) {
    return (
      <div className="mb-8 flex flex-col gap-3">
        <a className="text-muted underline" href={`${path}?export=1`}>{text("template.export")}</a>
        <Code value={draft} language={languageOf(name)} label={name} readOnly minHeight="12rem" />
      </div>
    );
  }

  return (
    <div className="mb-8 flex flex-col gap-3">
      <div className="flex items-center gap-4">
        <a className="text-muted underline" href={`${path}?export=1`}>{text("template.export")}</a>
        <label className="text-muted underline">
          {text("cluster.import")}
          <input className="hidden" type="file" accept=".tf,.tf.json,.json" onChange={(e) => {
            const file = e.target.files?.[0];
            if (file) load(file);
          }} />
        </label>
      </div>
      <Code value={draft} onChange={setDraft} language={languageOf(name)} label={name} minHeight="12rem" />
      <div className="flex items-center gap-4">
        <Button type="button" onClick={() => void save()} disabled={busy}>{busy ? <Spinner /> : null}
          {text("common.save")}</Button>
        <TextButton type="button" onClick={() => setDraft(files[name] ?? "")}>{text("common.cancel")}</TextButton>
      </div>
      {error ? <ErrorText>{error}</ErrorText> : null}
    </div>
  );
}
