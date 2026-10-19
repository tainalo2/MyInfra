import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Trash2 } from "lucide-react";
import { api, type Destruction } from "./api";
import { text, knownMessage } from "./i18n";
import { ErrorText, LoadingLines, PageTitle, TextButton, useDestructions } from "./ui";

export function DestructionPage() {
  const { id = "" } = useParams();
  const [job, setJob] = useState<Destruction | null>(null);
  const [error, setError] = useState("");
  const [watch, setWatch] = useState(0);

  useEffect(() => {
    let stop = false;
    const load = () => {
      api<Destruction>(`/api/destructions/${id}`)
        .then((next) => {
          if (stop) return;
          setJob(next);
          if (next.status !== "running") window.clearInterval(timer);
        })
        .catch((err) => {
          if (!stop) setError(err instanceof Error ? err.message : text("common.error"));
        });
    };
    const timer = window.setInterval(load, 2000);
    load();
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [id, watch]);

  async function resume() {
    setError("");
    try {
      setJob(await api<Destruction>(`/api/destructions/${id}/resume`, { method: "POST" }));
      setWatch((current) => current + 1);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  if (!job && !error) return <LoadingLines lines={4} />;
  if (!job) return <ErrorText>{error}</ErrorText>;
  const label = job.status === "done" ? "Terminé" : job.status === "failed" ? "Échec" : text("ui.enCours");

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
  if (status === "done") return "Fait";
  if (status === "failed") return "Échec";
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
