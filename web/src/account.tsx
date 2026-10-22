import { FormEvent, useEffect, useState } from "react";
import { LogOut, User } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { api } from "./api";
import { text } from "./i18n";
import { ConfirmDialog, Spinner } from "./kit";
import { roleLabel, useSession } from "./session";
import { TotpQr } from "./totp-qr";
import { Button, ErrorText, Field, Icon, Input, PageTitle, SectionTitle, TextButton } from "./ui";

export function Account() {
  const navigate = useNavigate();
  const { role, login, totp, refresh, clear } = useSession();
  const [username, setUsername] = useState(login);
  const [loginBusy, setLoginBusy] = useState(false);
  const [loginDone, setLoginDone] = useState(false);
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [enrollSecret, setEnrollSecret] = useState("");
  const [enrollUri, setEnrollUri] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [totpBusy, setTotpBusy] = useState(false);
  const [disableTotpAsk, setDisableTotpAsk] = useState(false);

  useEffect(() => {
    setUsername(login);
  }, [login]);

  async function saveLogin(event: FormEvent) {
    event.preventDefault();
    setLoginBusy(true);
    setError("");
    setLoginDone(false);
    try {
      const res = await api<{ login: string }>("/api/account", { method: "PATCH", body: JSON.stringify({ login: username.trim() }) });
      await refresh();
      setUsername(res.login);
      setLoginDone(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setLoginBusy(false);
    }
  }

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setDone(false);
    if (password !== confirm) {
      setError(text("ui.lesDeuxSaisiesDuNouveauMot"));
      return;
    }
    try {
      await api("/api/password", { method: "POST", body: JSON.stringify({ current, password }) });
      setCurrent("");
      setPassword("");
      setConfirm("");
      setDone(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  function cancelTotpEnroll() {
    setEnrollSecret("");
    setEnrollUri("");
    setTotpCode("");
    setError("");
  }

  async function startTotpEnroll() {
    setTotpBusy(true);
    setError("");
    try {
      const res = await api<{ secret: string; uri?: string }>("/api/account/totp", { method: "POST" });
      setEnrollSecret(res.secret);
      setEnrollUri(res.uri ?? "");
      setTotpCode("");
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setTotpBusy(false);
    }
  }

  async function confirmTotpEnroll() {
    setTotpBusy(true);
    setError("");
    try {
      const code = totpCode.replace(/\D/g, "");
      await api("/api/account/totp/confirm", {
        method: "POST",
        body: JSON.stringify({ secret: enrollSecret, totp: code }),
      });
      setEnrollSecret("");
      setTotpCode("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setTotpBusy(false);
    }
  }

  async function disableTotp() {
    setTotpBusy(true);
    setError("");
    try {
      await api("/api/account/totp", { method: "DELETE" });
      cancelTotpEnroll();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setTotpBusy(false);
    }
  }

  const enrolling = enrollSecret !== "";

  return (
    <>
      <PageTitle title={text("account.title")} icon={User} />
      {role ? <p className="mb-8 text-muted">{text("role.current", { role: roleLabel(role) })}</p> : null}

      <section className="max-w-[720px]">
        <SectionTitle>{text("account.profile")}</SectionTitle>
        <form onSubmit={saveLogin} className="mt-4 flex flex-col gap-6">
          <Field label={text("auth.login")}>
            <Input value={username} onChange={(e) => setUsername(e.target.value)} required autoComplete="username" />
          </Field>
          {loginDone ? <p className="text-ok">{text("account.loginChanged")}</p> : null}
          <div>
            <Button type="submit" variant="secondary" disabled={loginBusy || username.trim() === ""}>
              {loginBusy ? <Spinner /> : null}
              {text("account.saveLogin")}
            </Button>
          </div>
        </form>
      </section>

      <section className="mt-16 max-w-[720px]">
        <SectionTitle>{text("account.passwordTitle")}</SectionTitle>
        <form onSubmit={onSubmit} className="mt-4 flex flex-col gap-6">
          <Field label={text("field.currentPassword")}>
            <Input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required autoComplete="current-password" />
          </Field>
          <Field label={text("field.newPassword")}>
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="new-password" />
          </Field>
          <Field label={text("field.confirm")}>
            <Input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required autoComplete="new-password" />
          </Field>
          {done ? <p className="text-ok">{text("account.changed")}</p> : null}
          <div>
            <Button type="submit">{text("ui.changerLeMotDePasse")}</Button>
          </div>
        </form>
      </section>

      <section className="mt-16 max-w-[720px]">
        <SectionTitle>{text("account.totpTitle")}</SectionTitle>
        <p className="mt-4 text-muted">{totp ? text("account.totpOn") : enrolling ? text("account.totpEnrolling") : text("account.totpOff")}</p>
        {enrolling ? (
          <div className="mt-4 rounded-md border border-line p-4">
            <TotpQr secret={enrollSecret} account={username || login} uri={enrollUri} />
            <p className="mt-4 text-muted">{text("account.totpSecret")}</p>
            <code className="mt-2 block break-all font-mono text-[13px]">{enrollSecret}</code>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void confirmTotpEnroll();
              }}
              className="mt-6 flex flex-col gap-4"
            >
              <Field label={text("auth.totp")}>
                <Input value={totpCode} inputMode="numeric" autoComplete="one-time-code" onChange={(e) => setTotpCode(e.target.value)} required />
              </Field>
              <div className="flex flex-wrap gap-3">
                <Button type="submit" disabled={totpBusy || totpCode.trim().length < 6}>
                  {totpBusy ? <Spinner /> : null}
                  {text("account.totpConfirm")}
                </Button>
                <TextButton type="button" onClick={cancelTotpEnroll} disabled={totpBusy}>
                  {text("account.totpCancel")}
                </TextButton>
              </div>
            </form>
          </div>
        ) : null}
        {!enrolling ? (
          <div className="mt-6 flex flex-wrap gap-3">
            {totp ? (
              <Button type="button" variant="secondary" disabled={totpBusy} onClick={() => setDisableTotpAsk(true)}>
                {text("account.totpDisable")}
              </Button>
            ) : (
              <Button type="button" variant="secondary" disabled={totpBusy} onClick={() => void startTotpEnroll()}>
                {totpBusy ? <Spinner /> : null}
                {text("account.totpEnable")}
              </Button>
            )}
          </div>
        ) : null}
      </section>

      {error ? (
        <div className="mt-8 max-w-[720px]">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}

      {disableTotpAsk ? (
        <ConfirmDialog
          title={text("account.totpDisableTitle")}
          confirmLabel={text("account.totpDisable")}
          danger
          onConfirm={async () => {
            await disableTotp();
            setDisableTotpAsk(false);
          }}
          onClose={() => setDisableTotpAsk(false)}
        >
          {text("account.totpDisableHint")}
        </ConfirmDialog>
      ) : null}
      <div className="mt-16">
        <Button
          type="button"
          onClick={async () => {
            await api("/api/session", { method: "DELETE" });
            clear();
            navigate("/entree");
          }}
        >
          <Icon icon={LogOut} />
          {text("footer.logout")}
        </Button>
      </div>
    </>
  );
}
