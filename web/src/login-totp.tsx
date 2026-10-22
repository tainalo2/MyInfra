import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Shield } from "lucide-react";
import { api, returnPath } from "./api";
import { text } from "./i18n";
import { useI18n } from "./lang";
import { Spinner } from "./kit";
import { useSession } from "./session";
import { TotpInput } from "./totp-input";
import { AuthFrame, Button, ErrorText, Icon, TextButton } from "./ui";

const challengeKey = "myinfra.totpChallenge";

export function saveTotpChallenge(challenge: string) {
  try {
    sessionStorage.setItem(challengeKey, challenge);
  } catch {
    /* sessionStorage indisponible */
  }
}

export function readTotpChallenge() {
  try {
    return sessionStorage.getItem(challengeKey) ?? "";
  } catch {
    return "";
  }
}

export function clearTotpChallenge() {
  try {
    sessionStorage.removeItem(challengeKey);
  } catch {
    /* sessionStorage indisponible */
  }
}

export function LoginTotpPage() {
  useI18n();
  const navigate = useNavigate();
  const { refresh } = useSession();
  const [challenge, setChallenge] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const stored = readTotpChallenge();
    if (!stored) {
      navigate("/entree", { replace: true });
      return;
    }
    setChallenge(stored);
  }, [navigate]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (busy || code.length < 6 || !challenge) return;
    setBusy(true);
    setError("");
    try {
      await api("/api/session/totp", {
        method: "POST",
        body: JSON.stringify({ challenge, totp: code }),
      });
      clearTotpChallenge();
      await refresh();
      navigate(returnPath(), { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  return (
    <AuthFrame>
      <h1 className="mb-2 flex items-center gap-3 text-2xl font-medium">
        <Icon icon={Shield} />
        {text("auth.totpStepTitle")}
      </h1>
      <p className="mb-12 text-muted">{text("auth.totpStepHint")}</p>
      <form onSubmit={onSubmit} className="flex max-w-[420px] flex-col gap-8">
        <TotpInput value={code} onChange={setCode} disabled={busy} />
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div className="flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={busy || code.length < 6}>
            {busy ? <Spinner /> : null}
            {text("auth.totpStepSubmit")}
          </Button>
          <TextButton
            type="button"
            onClick={() => {
              clearTotpChallenge();
              navigate("/entree", { replace: true });
            }}
            disabled={busy}
          >
            {text("auth.totpStepBack")}
          </TextButton>
        </div>
      </form>
    </AuthFrame>
  );
}
