import { useEffect, useState } from "react";
import { api, type Application, type ReleaseService } from "./api";
import { knownMessage, text } from "./i18n";
import { LocalImagePicker } from "./localimages";
import { ImageOrigin } from "./registries";
import { Button, Input, TextButton } from "./ui";

export function ImageEditor({ app, onChange }: { app: Application; onChange: (next: Application) => void }) {
  const services = app.services ?? [];
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [releases, setReleases] = useState<ReleaseService[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const next: Record<string, string> = {};
    for (const service of services) next[service.name] = service.image;
    setDraft(next);
  }, [app.id, app.source, services.map((service) => service.image).join("|")]);

  useEffect(() => {
    let stop = false;
    api<{ services: ReleaseService[] }>(`/api/applications/${app.id}/releases`)
      .then((body) => {
        if (!stop) setReleases(body.services ?? []);
      })
      .catch(() => {
        if (!stop) setReleases([]);
      });
    return () => {
      stop = true;
    };
  }, [app.id, app.source, app.rollout]);

  const dirty = services.some((service) => (draft[service.name] ?? service.image) !== service.image);
  const updates = releases.filter((service) => service.update && service.update !== service.image);
  const updating = app.rollout === "updating" || busy;

  async function save(next: { name: string; image: string }[]) {
    setBusy(true);
    setError("");
    try {
      await api(`/api/applications/${app.id}/images`, { method: "POST", body: JSON.stringify({ services: next }) });
      const fresh = await api<Application>(`/api/applications/${app.id}`);
      onChange(fresh);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-4 flex flex-col gap-4">
      {app.rollout === "updating" ? <p className="text-warn">{text("ui.miseAJourEnCoursSi")}</p> : null}
      {app.rollout === "rolled_back" && app.rolloutDetail ? <p className="text-bad">{app.rolloutDetail}</p> : null}
      {updates.length > 0 && app.rollout !== "updating" ? (
        <div className="flex flex-wrap items-center gap-4">
          <p>{text("ui.miseAJourDisponible")}</p>
          <TextButton
            type="button"
            disabled={updating}
            onClick={() =>
              void save(
                services.map((service) => {
                  const release = releases.find((item) => item.name === service.name);
                  return { name: service.name, image: release?.update || service.image };
                }),
              )
            }
          >
            {text("ui.mettreAJour")}
          </TextButton>
        </div>
      ) : null}
      <ul className="flex flex-col">
        {services.map((service) => {
          const release = releases.find((item) => item.name === service.name);
          return (
            <li key={service.name} className="border-t border-line py-4">
              <div className="flex items-baseline justify-between gap-6">
                <span>{service.name}</span>
                <span className="text-muted">
                  {service.published ? text("common.published") : text("common.internal")}
                  {service.disk ? ` · ${text("common.disk")}` : ""}
                  {(service.replicas ?? 1) > 1 ? ` · ${text("template.copies", { count: service.replicas ?? 1 })}` : ""}
                </span>
              </div>
              <div className="mt-3">
                <Input
                  value={draft[service.name] ?? service.image}
                  aria-label={text("images.of", { name: service.name })}
                  disabled={updating}
                  onChange={(event) => setDraft((current) => ({ ...current, [service.name]: event.target.value }))}
                />
              </div>
              <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
                <ImageOrigin image={draft[service.name] ?? service.image} />
                <div className="w-full sm:w-64">
                  <LocalImagePicker
                    value={draft[service.name] ?? service.image}
                    disabled={updating}
                    onPick={(ref) => setDraft((current) => ({ ...current, [service.name]: ref }))}
                  />
                </div>
              </div>
              {release?.update ? <p className="mt-2 text-muted">{text("images.proposed", { version: release.update })}</p> : null}
              {release?.detail ? <p className="mt-2 text-bad">{knownMessage(release.detail)}</p> : null}
            </li>
          );
        })}
      </ul>
      {dirty ? (
        <div>
          <Button
            type="button"
            disabled={updating}
            onClick={() => void save(services.map((service) => ({ name: service.name, image: draft[service.name] ?? service.image })))}
          >
            {text("common.save")}
          </Button>
        </div>
      ) : null}
      {error ? <p className="text-bad">{error}</p> : null}
    </div>
  );
}
