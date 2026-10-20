import { FormEvent, useEffect, useRef, useState } from "react";
import { HardDrive, Link2, Trash2, Upload } from "lucide-react";
import { api, type LocalImage } from "./api";
import { currentLocale, text, textError } from "./i18n";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { usePoll } from "./poll";
import { Button, ErrorText, Field, Group, Icon, Input, LoadingCards, PageTitle, SectionTitle, Select, TextButton, focus } from "./ui";

// An image a person built is brought to MyInfra as the archive docker save
// writes, sent from this page or fetched from a raw URL. MyInfra loads it on
// the servers itself: they never need to reach MyInfra or a registry for it.

export function size(bytes: number) {
  const units = [text("unit.mb"), text("unit.gb")];
  const mb = bytes / (1 << 20);
  if (mb < 1024) return `${mb.toLocaleString(currentLocale(), { maximumFractionDigits: 1 })} ${units[0]}`;
  return `${(mb / 1024).toLocaleString(currentLocale(), { maximumFractionDigits: 2 })} ${units[1]}`;
}

// send posts the archive with its progress, which fetch does not report.
function send(file: File, name: string, tag: string, onProgress: (share: number) => void) {
  return new Promise<LocalImage>((resolve, reject) => {
    const query = new URLSearchParams();
    if (name.trim()) query.set("name", name.trim());
    if (tag.trim()) query.set("tag", tag.trim());
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `/api/images?${query}`);
    xhr.setRequestHeader("Content-Type", "application/octet-stream");
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress(event.loaded / event.total);
    };
    xhr.onload = () => {
      let body: { code?: string; params?: Record<string, string>; error?: string } & Partial<LocalImage> = {};
      try {
        body = JSON.parse(xhr.responseText || "{}");
      } catch {
        // An answer that is not JSON: the message below says it failed.
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as LocalImage);
      else reject(new Error(textError(body.code, body.params, body.error || "")));
    };
    xhr.onerror = () => reject(new Error(text("err.network")));
    xhr.send(file);
  });
}

export function Images() {
  const { data: images, setData: setImages, error, reload } = usePoll<LocalImage[]>("/api/images", 0);
  const [removing, setRemoving] = useState<LocalImage | null>(null);
  const toast = useToast();

  async function remove(image: LocalImage) {
    await api(`/api/images/${image.id}`, { method: "DELETE" });
    setImages((current) => current?.filter((item) => item.id !== image.id) ?? current);
    setRemoving(null);
    toast(text("images.removed", { ref: image.ref }));
  }

  function added(image: LocalImage) {
    setImages((current) => [image, ...(current ?? [])]);
    toast(text("images.added", { ref: image.ref }));
    void reload();
  }

  return (
    <div className="max-w-[860px]">
      <PageTitle title={text("images.title")} icon={HardDrive} />
      <p className="mb-8 text-muted">{text("images.intro")}</p>
      <AddImage onAdded={added} />
      <section className="mt-12">
        <SectionTitle>{text("images.list")}</SectionTitle>
        <div className="mt-4">
          {error && !images ? <ErrorText>{error}</ErrorText> : null}
          {images === null && !error ? <LoadingCards /> : null}
          {images && images.length === 0 ? <p className="text-muted">{text("images.empty")}</p> : null}
          {images && images.length > 0 ? (
            <ul className="flex flex-col">
              {images.map((image) => (
                <li key={image.id} className="flex flex-wrap items-start justify-between gap-4 border-t border-line py-4">
                  <div className="min-w-0 flex-1">
                    <p className="break-all font-mono text-[13px]">{image.ref}</p>
                    <p className="mt-1 text-muted">
                      {[size(image.size), image.platform, image.ports.length > 0 ? text("images.ports", { ports: image.ports.join(", ") }) : ""]
                        .filter(Boolean)
                        .join(" · ")}
                    </p>
                    <p className="mt-1 break-all text-faint">
                      {image.source === "url" ? text("images.fromUrl", { url: image.sourceUrl ?? "" }) : text("images.fromFile")} ·{" "}
                      {new Date(image.createdAt).toLocaleDateString(currentLocale())}
                    </p>
                    {image.usedBy.length > 0 ? <p className="mt-1">{text("images.usedBy", { apps: image.usedBy.join(", ") })}</p> : null}
                  </div>
                  <TextButton
                    type="button"
                    tone="danger"
                    disabled={image.usedBy.length > 0}
                    title={image.usedBy.length > 0 ? text("images.inUse") : undefined}
                    onClick={() => setRemoving(image)}
                  >
                    <Icon icon={Trash2} />
                    {text("common.delete")}
                  </TextButton>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      </section>
      {removing ? (
        <ConfirmDialog
          title={text("server.removeNamed", { name: removing.ref })}
          confirmLabel={text("common.delete")}
          danger
          onConfirm={() => remove(removing)}
          onClose={() => setRemoving(null)}
        >
          {text("images.deleteHint")}
        </ConfirmDialog>
      ) : null}
    </div>
  );
}

function AddImage({ onAdded }: { onAdded: (image: LocalImage) => void }) {
  const [mode, setMode] = useState<"file" | "url">("file");
  const [file, setFile] = useState<File | null>(null);
  const [address, setAddress] = useState("");
  const [name, setName] = useState("");
  const [tag, setTag] = useState("");
  const [progress, setProgress] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setProgress(mode === "file" ? 0 : null);
    try {
      const image =
        mode === "file"
          ? await send(file!, name, tag, setProgress)
          : await api<LocalImage>("/api/images/fetch", { method: "POST", body: JSON.stringify({ url: address.trim(), name: name.trim(), tag: tag.trim() }) });
      onAdded(image);
      setFile(null);
      setAddress("");
      setName("");
      setTag("");
      if (input.current) input.current.value = "";
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy(false);
      setProgress(null);
    }
  }

  const ready = mode === "file" ? file !== null : address.trim() !== "";
  // Sent, the archive is still read, renamed and stored: the bar stops at the end.
  const stored = progress !== null && progress >= 1;

  return (
    <form onSubmit={submit} className="flex flex-col gap-6 rounded-md border border-line bg-surface p-4 sm:p-6">
      <Group label={text("images.add")}>
        <div className="flex flex-wrap gap-2" role="radiogroup">
          {(["file", "url"] as const).map((item) => (
            <button
              key={item}
              type="button"
              role="radio"
              aria-checked={mode === item}
              onClick={() => setMode(item)}
              className={`inline-flex min-h-11 items-center gap-2 rounded-md border px-3 py-2 sm:min-h-0 ${mode === item ? "border-ink bg-status" : "border-line"} ${focus}`}
            >
              <Icon icon={item === "file" ? Upload : Link2} />
              {item === "file" ? text("images.byFile") : text("images.byUrl")}
            </button>
          ))}
        </div>
      </Group>
      {mode === "file" ? (
        <Field label={text("images.file")} hint={text("images.fileHint")}>
          <input
            ref={input}
            type="file"
            accept=".tar,.tar.gz,.tgz,application/x-tar,application/gzip"
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
            className={`block w-full rounded-md border border-line bg-canvas px-3 py-2 ${focus}`}
          />
        </Field>
      ) : (
        <Field label={text("images.url")} hint={text("images.urlHint")}>
          <Input value={address} onChange={(e) => setAddress(e.target.value)} placeholder="https://exemple.fr/mon-image.tar.gz" inputMode="url" autoCapitalize="off" spellCheck={false} />
        </Field>
      )}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
        <Field label={text("images.name")} hint={text("images.nameHint")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="mon-site" autoCapitalize="off" spellCheck={false} />
        </Field>
        <Field label={text("images.tag")} hint={text("images.tagHint")}>
          <Input value={tag} onChange={(e) => setTag(e.target.value)} placeholder="1.0.0" autoCapitalize="off" spellCheck={false} />
        </Field>
      </div>
      {progress !== null ? (
        <div>
          <div className="h-2 overflow-hidden rounded-full bg-line" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(progress * 100)} aria-label={text("images.sending")}>
            <div className="h-full rounded-full bg-ink" style={{ width: `${progress * 100}%` }} />
          </div>
          <p className="mt-2 text-muted">{stored ? text("images.storing") : text("images.sendingShare", { share: Math.round(progress * 100) })}</p>
        </div>
      ) : busy ? (
        <p className="text-muted">{text("images.fetching")}</p>
      ) : null}
      {error ? <ErrorText>{error}</ErrorText> : null}
      <div>
        <Button type="submit" disabled={!ready || busy}>
          {busy ? <Spinner /> : <Icon icon={mode === "file" ? Upload : Link2} />}
          {mode === "file" ? text("images.send") : text("images.fetch")}
        </Button>
      </div>
    </form>
  );
}

// LocalImagePicker offers the imported images in place of a registry image.
export function LocalImagePicker({ value, onPick, disabled }: { value: string; onPick: (ref: string) => void; disabled?: boolean }) {
  const [images, setImages] = useState<LocalImage[]>([]);
  useEffect(() => {
    api<LocalImage[]>("/api/images")
      .then(setImages)
      .catch(() => setImages([]));
  }, []);
  if (images.length === 0) return null;
  return (
    <Select
      value={images.some((image) => image.ref === value) ? value : ""}
      onChange={(event) => event.target.value && onPick(event.target.value)}
      aria-label={text("images.pick")}
      disabled={disabled}
    >
      <option value="">{text("images.pick")}</option>
      {images.map((image) => (
        <option key={image.id} value={image.ref}>
          {image.ref.replace("myinfra.local/", "")}
        </option>
      ))}
    </Select>
  );
}
