import { FormEvent, useState } from "react";
import { Lock, LogOut } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { api } from "./api";
import { text } from "./i18n";
import { Button, ErrorText, Field, Icon, Input, PageTitle } from "./ui";

export function Account() {
  const navigate = useNavigate();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

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

  return (
    <>
      <PageTitle title={text("account.title")} icon={Lock} />
      <form onSubmit={onSubmit} className="flex max-w-[720px] flex-col gap-6">
        <Field label={text("field.currentPassword")}>
          <Input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required autoComplete="current-password" />
        </Field>
        <Field label={text("field.newPassword")}>
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="new-password" />
        </Field>
        <Field label={text("field.confirm")}>
          <Input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required autoComplete="new-password" />
        </Field>
        {error ? <ErrorText>{error}</ErrorText> : null}
        {done ? <p className="text-ok">{text("account.changed")}</p> : null}
        <div>
          <Button type="submit">{text("ui.changerLeMotDePasse")}</Button>
        </div>
      </form>
      <div className="mt-16">
        <Button
          type="button"
          onClick={async () => {
            await api("/api/session", { method: "DELETE" });
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
