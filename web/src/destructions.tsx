import { useState } from "react";
import { usePoll } from "./poll";
import { Link, useParams } from "react-router-dom";
import { Trash2 } from "lucide-react";
import { api, type Destruction } from "./api";
import { text, knownMessage } from "./i18n";
import { ErrorText, LoadingLines, PageTitle, TextButton, useDestructions } from "./ui";

export function DestructionPage() {
  const { id = "" } = useParams();
  const { data: job, setData: setJob, error: loadError, reload } = usePoll<Destruction>(`/api/destructions/${id}`, 2000, {
    stopWhen: (next) => next.status !== "running",
  });
  const [actionError, setError] = useState("");
  const error = actionError || loadError;

  async function resume() {
    setError("");
    try {
      setJob(await api<Destruction>(`/api/destructions/${id}/resume`, { method: "POST" }));
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  if (!job && !error) return <LoadingLines lines={4} />;
  if (!job) return <ErrorText>{error}</ErrorText>;
  const label = job.status === "done" ? text("status.done") : job.status === "failed" ? text("status.failed") : text("ui.enCours");

  return (
    <>
      <PageTitle title={job.name} icon={Trash2} />
      <p className="mb-2 text-muted">{kindLabel(job.kind)}</p>
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      <p className={`mb-8 ${job.status === "failed" ? "text-bad" : job.status === "done" ? "text-ok" : "text-muted"}`}>{label}</p>
      <ol className="max-w-[720px]">
        {(job.steps ?? []).map((step, index) => (
          <li key={index} className="border-t border-line py-4">
            <div className="flex items-baseline justify-between gap-4">
              <span>{knownMessage(step.label)}</span>
              <span className={step.status === "failed" ? "text-bad" : step.status === "done" ? "text-ok" : "text-muted"}>{stepText(step.status)}</span>
            </div>
            {step.detail ? <p className="mt-1 text-faint">{knownMessage(step.detail)}</p> : null}
          </li>
        ))}
      </ol>
      {job.status === "failed" ? (
        <div className="mt-8">
          <TextButton type="button" onClick={() => void resume()}>
            {text("home.resume")}
          </TextButton>
        </div>
      ) : null}
    </>
  );
}

function kindLabel(kind: string) {
  if (kind === "cluster") return text("ui.destructionDuCluster");
  if (kind === "server") return text("ui.destructionDuServeur");
  return text("ui.destructionDeLApplication");
}

function stepText(status: string) {
  if (status === "done") return text("status.done");
  if (status === "failed") return text("status.failed");
  if (status === "running") return text("ui.enCours");
  return text("status.pending");
}

export function RemovalNotice({ job }: { job?: Destruction }) {
  if (!job) return null;
  return (
    <Link to={`/destructions/${job.id}`} className="text-warn">
      {text("ui.suppressionEnCours")}
    </Link>
  );
}

export function DestructionsPage() {
  const jobs = useDestructions();
  return (
    <>
      <PageTitle title={text("job.title")} icon={Trash2} />
      {jobs.length === 0 ? (
        <p className="text-muted">{text("job.empty")}</p>
      ) : (
        <ul className="max-w-[720px]">
          {jobs.map((job) => (
            <li key={job.id} className="border-t border-line py-4">
              <Link to={`/destructions/${job.id}`} className="flex items-baseline justify-between gap-4">
                <span>{job.name}</span>
                <span className="text-muted">{shortKind(job.kind)}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

function shortKind(kind: string) {
  if (kind === "cluster") return text("field.cluster");
  if (kind === "server") return "Serveur";
  return text("crumb.application");
}
