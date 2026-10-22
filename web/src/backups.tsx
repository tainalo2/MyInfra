import { useEffect, useRef, useState } from "react";
import { Download, Upload } from "lucide-react";
import { api, type Application } from "./api";
import { text, type MsgKey } from "./i18n";
import { Spinner } from "./kit";
import { useSession } from "./session";
import { Button, ErrorText, Icon, LoadingLines, SectionTitle } from "./ui";

type BackupSource = { id: string; label: string; available: boolean; restore?: boolean; detail?: string };
type BackupService = { name: string; sources: BackupSource[] };
type BackupList = { services: BackupService[] };

export function AppBackups({ app, locked }: { app: Application; locked?: boolean }) {
  const { canAdmin } = useSession();
  const [list, setList] = useState<BackupList | null>(null);
  const [error, setError] = useState("");
  const [restoring, setRestoring] = useState("");
  const [restoreError, setRestoreError] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);
  const [pick, setPick] = useState<{ service: string; source: string } | null>(null);

  useEffect(() => {
    let stop = false;
    api<BackupList>(`/api/applications/${app.id}/backups`)
      .then((body) => {
        if (!stop) setList(body);
      })
      .catch((err) => {
        if (!stop) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      stop = true;
    };
  }, [app.id]);

  async function uploadRestore(service: string, source: string, file: File) {
    setRestoring(`${service}/${source}`);
    setRestoreError("");
    try {
      const url = `/api/applications/${app.id}/backups/${encodeURIComponent(service)}?source=${encodeURIComponent(source)}`;
      const res = await fetch(url, { method: "POST", body: file, credentials: "same-origin" });
      if (!res.ok) {
        let message = text("backup.restoreFailed");
        try {
          const body = await res.json();
          if (body.error) message = body.error;
        } catch {
          // ignore
        }
        throw new Error(message);
      }
    } catch (err) {
      setRestoreError(err instanceof Error ? err.message : String(err));
    } finally {
      setRestoring("");
      setPick(null);
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  const rows = (list?.services ?? []).filter((svc) => svc.sources.some((s) => s.available));
  if (!list && !error) return <LoadingLines lines={1} label={text("backup.loading")} />;
  if (error) {
    return (
      <section className="mt-10">
        <SectionTitle>{text("backup.title")}</SectionTitle>
        <ErrorText>{error}</ErrorText>
      </section>
    );
  }
  if (rows.length === 0) return null;

  return (
    <section className="mt-10">
      <SectionTitle>{text("backup.title")}</SectionTitle>
      <p className="mt-2 text-muted">{text("backup.hint")}</p>
      <p className="mt-1 text-[13px] text-faint">{text("backup.restoreHint")}</p>
      {restoreError ? <ErrorText>{restoreError}</ErrorText> : null}
      <input
        ref={fileRef}
        type="file"
        className="hidden"
        accept={pick?.source === "postgres" ? ".dump,.backup" : ".tar,.gz,.tgz,application/gzip,application/x-tar"}
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (file && pick) void uploadRestore(pick.service, pick.source, file);
        }}
      />
      <ul className="mt-4 flex flex-col gap-4">
        {rows.map((svc) => (
          <li key={svc.name} className="border-t border-line pt-4">
            <p className="font-mono text-[13px]">{svc.name}</p>
            <ul className="mt-2 flex flex-col gap-2">
              {svc.sources
                .filter((s) => s.available)
                .map((source) => (
                  <li key={source.id} className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                      <p>{sourceLabel(source.id, source.label)}</p>
                      {source.detail ? <p className="text-[13px] text-faint">{source.detail}</p> : null}
                    </div>
                    <div className="flex flex-wrap gap-2">
                      {canAdmin ? (
                      <Button
                        type="button"
                        variant="secondary"
                        onClick={() => {
                          const url = `/api/applications/${app.id}/backups/${encodeURIComponent(svc.name)}?source=${encodeURIComponent(source.id)}`;
                          window.open(url, "_blank", "noopener,noreferrer");
                        }}
                      >
                        <Icon icon={Download} />
                        {text("backup.download")}
                      </Button>
                      ) : null}
                      {source.restore ? (
                        <Button
                          type="button"
                          variant="secondary"
                          disabled={Boolean(locked) || restoring !== ""}
                          onClick={() => {
                            setPick({ service: svc.name, source: source.id });
                            fileRef.current?.click();
                          }}
                        >
                          {restoring === `${svc.name}/${source.id}` ? <Spinner /> : <Icon icon={Upload} />}
                          {text("backup.restore")}
                        </Button>
                      ) : null}
                    </div>
                  </li>
                ))}
            </ul>
          </li>
        ))}
      </ul>
    </section>
  );
}

function sourceLabel(id: string, fallback: string) {
  const key = `backup.source.${id}` as MsgKey;
  const msg = text(key);
  return msg === key ? fallback : msg;
}
