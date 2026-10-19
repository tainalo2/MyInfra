import { useState } from "react";
import { Link } from "react-router-dom";
import { Rocket } from "lucide-react";
import { api, type Deployment } from "./api";
import { text, type MsgKey } from "./i18n";
import { ErrorText, PageTitle, TextButton, statusLabel, useDeployments } from "./ui";

function kindKey(kind: string): MsgKey {
  if (kind === "server") return "deploy.kind.server";
  if (kind === "template") return "deploy.kind.template";
  if (kind === "transfer") return "deploy.kind.transfer";
  return "deploy.kind.application";
}

export function CancelDeployment({ kind, id, active }: { kind: string; id: string; active: boolean }) {
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState("");
  if (!active) return null;

  async function cancel() {
    setError("");
    try {
      await api(`/api/deployments/cancel`, { method: "POST", body: JSON.stringify({ kind, id }) });
      setConfirm(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("err.generic"));
    }
  }

  return (
    <div className="mt-8">
      {error ? (
        <div className="mb-4">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {confirm ? (
        <TextButton type="button" onClick={() => void cancel()}>
          {text("deploy.cancelConfirm")}
        </TextButton>
      ) : (
        <TextButton type="button" onClick={() => setConfirm(true)}>
          {text("common.cancel")}
        </TextButton>
      )}
    </div>
  );
}

export function DeploymentsPage() {
  const items = useDeployments();
  const [confirm, setConfirm] = useState("");
  const [hidden, setHidden] = useState<string[]>([]);
  const [error, setError] = useState("");
  const visible = items.filter((item) => !hidden.includes(`${item.kind}:${item.id}`));

  async function cancel(item: Deployment) {
    setError("");
    try {
      await api("/api/deployments/cancel", { method: "POST", body: JSON.stringify({ kind: item.kind, id: item.id }) });
      setHidden((current) => [...current, `${item.kind}:${item.id}`]);
      setConfirm("");
    } catch (err) {
      setError(err instanceof Error ? err.message : text("err.generic"));
    }
  }

  return (
    <>
      <PageTitle title={text("deploy.title")} icon={Rocket} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {visible.length === 0 ? (
        <p className="text-muted">{text("deploy.empty")}</p>
      ) : (
        <ul className="max-w-[720px]">
          {visible.map((item) => {
            const label = statusLabel(item.status);
            const key = `${item.kind}:${item.id}`;
            return (
              <li key={key} className="border-t border-line py-4">
                <div className="flex items-baseline justify-between gap-4">
                  <Link to={item.to}>
                    <span className="block">{item.name || text(kindKey(item.kind))}</span>
                    {item.step ? <span className="mt-1 block text-faint">{item.step}</span> : null}
                  </Link>
                  <span className="shrink-0 text-muted">{text(kindKey(item.kind))}</span>
                </div>
                <div className="mt-1 flex items-baseline justify-between gap-4">
                  <p className={label.tone}>{label.text}</p>
                  {confirm === key ? (
                    <TextButton type="button" onClick={() => void cancel(item)}>
                      {text("deploy.cancelConfirm")}
                    </TextButton>
                  ) : (
                    <TextButton type="button" onClick={() => setConfirm(key)}>
                      {text("common.cancel")}
                    </TextButton>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
