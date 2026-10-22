import { FormEvent, useEffect, useState } from "react";
import { Plus, ShieldCheck, ShieldOff, Trash2, Users } from "lucide-react";
import { api } from "./api";
import { text } from "./i18n";
import { ConfirmDialog, Spinner, useToast } from "./kit";
import { roleLabel, type UserAccount, type UserRole } from "./session";
import { Button, EmptyList, ErrorText, Field, Icon, IconButton, Input, LoadingLines, PageTitle, Select, TextButton } from "./ui";

const roles: UserRole[] = ["admin", "deploy", "read"];

const roleTone: Record<UserRole, string> = {
  admin: "border-line bg-ink/5 text-ink",
  deploy: "border-line bg-link/10 text-link",
  read: "border-line bg-surface text-muted",
};

function RoleBadge({ role }: { role: UserRole }) {
  return (
    <span className={`inline-flex rounded-md border px-2.5 py-0.5 text-[13px] font-medium ${roleTone[role]}`}>
      {roleLabel(role)}
    </span>
  );
}

export function UsersPage() {
  const [rows, setRows] = useState<UserAccount[] | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<UserRole>("deploy");
  const [busy, setBusy] = useState(false);
  const [remove, setRemove] = useState<UserAccount | null>(null);
  const toast = useToast();

  async function reload() {
    setRows(await api<UserAccount[]>("/api/users"));
  }

  useEffect(() => {
    reload().catch((err) => setError(err instanceof Error ? err.message : text("common.error")));
  }, []);

  function closeCreate() {
    setCreating(false);
    setLogin("");
    setPassword("");
    setRole("deploy");
    setError("");
  }

  async function create(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const created = await api<UserAccount>("/api/users", {
        method: "POST",
        body: JSON.stringify({ login, password, role }),
      });
      setRows((current) => (current ? [...current, { ...created, totp: false }] : [created]));
      closeCreate();
      toast(text("users.created", { login: created.login }));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy(false);
    }
  }

  async function confirmRemove() {
    if (!remove) return;
    try {
      await api(`/api/users/${remove.id}`, { method: "DELETE" });
      setRows((current) => current?.filter((row) => row.id !== remove.id) ?? current);
      toast(text("users.removed", { login: remove.login }));
      setRemove(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setRemove(null);
    }
  }

  const showTable = rows && rows.length > 0;

  return (
    <>
      <PageTitle
        title={text("nav.users")}
        icon={Users}
        action={
          creating ? (
            <TextButton type="button" onClick={closeCreate}>
              {text("common.cancel")}
            </TextButton>
          ) : showTable ? (
            <Button type="button" onClick={() => setCreating(true)}>
              <Icon icon={Plus} />
              {text("users.new")}
            </Button>
          ) : null
        }
      />
      <p className="mb-8 max-w-[960px] text-muted">{text("users.intro")}</p>
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {creating ? (
        <form onSubmit={create} className="mb-10 flex max-w-[720px] flex-col gap-6 rounded-md border border-line bg-surface p-6">
          <Field label={text("auth.login")}>
            <Input value={login} onChange={(e) => setLogin(e.target.value)} required autoComplete="off" />
          </Field>
          <Field label={text("auth.password")}>
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="new-password" minLength={8} />
          </Field>
          <Field label={text("users.role")}>
            <Select value={role} onChange={(e) => setRole(e.target.value as UserRole)}>
              {roles.map((item) => (
                <option key={item} value={item}>
                  {roleLabel(item)}
                </option>
              ))}
            </Select>
          </Field>
          <p className="text-[13px] text-muted">{text(`users.hint.${role}`)}</p>
          <div>
            <Button type="submit" disabled={busy || login.trim() === "" || password.length < 8}>
              {busy ? <Spinner /> : null}
              {text("users.create")}
            </Button>
          </div>
        </form>
      ) : null}
      {rows === null && !error ? <LoadingLines lines={4} label={text("nav.users")} /> : null}
      {rows && rows.length === 0 && !creating ? (
        <EmptyList message={text("users.empty")} label={text("users.new")} onCreate={() => setCreating(true)} />
      ) : null}
      {showTable ? (
        <div className="max-w-[960px] overflow-x-auto rounded-md border border-line">
          <table className="w-full min-w-[520px] border-collapse text-left text-[15px]">
            <thead>
              <tr className="border-b border-line bg-canvas/80 text-[13px] text-muted">
                <th scope="col" className="px-4 py-3 font-medium">{text("auth.login")}</th>
                <th scope="col" className="px-4 py-3 font-medium">{text("users.role")}</th>
                <th scope="col" className="px-4 py-3 font-medium">{text("auth.totp")}</th>
                <th scope="col" className="w-14 px-4 py-3">
                  <span className="sr-only">{text("common.delete")}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows!.map((row) => (
                <tr key={row.id} className="border-b border-line last:border-b-0">
                  <td className="px-4 py-3.5 font-medium">{row.login}</td>
                  <td className="px-4 py-3.5">
                    <RoleBadge role={row.role} />
                  </td>
                  <td className="px-4 py-3.5">
                    <span className="inline-flex items-center gap-2 text-[13px] text-muted">
                      <Icon icon={row.totp ? ShieldCheck : ShieldOff} className={row.totp ? "text-ok" : undefined} />
                      {row.totp ? text("users.totpOn") : text("users.totpOff")}
                    </span>
                  </td>
                  <td className="px-4 py-3.5 text-right">
                    <IconButton icon={Trash2} label={text("common.delete")} className="text-bad" onClick={() => setRemove(row)} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {remove ? (
        <ConfirmDialog
          title={text("users.removeTitle", { login: remove.login })}
          confirmLabel={text("common.delete")}
          danger
          onConfirm={() => void confirmRemove()}
          onClose={() => setRemove(null)}
        >
          {text("users.removeHint")}
        </ConfirmDialog>
      ) : null}
    </>
  );
}
