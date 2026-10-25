import { FormEvent, useEffect, useState } from "react";
import { Save, Server as ServerIcon } from "lucide-react";
import { api, type Hosting, type Server } from "./api";
import { providerOptions } from "./accounts";
import { text } from "./i18n";
import { Spinner, useToast } from "./kit";
import { ProviderLogo } from "./logos";
import { ProviderSelect } from "./provider-select";
import { Button, ErrorText, Field, Group, Icon, Input, Select } from "./ui";

export const emptyHosting: Hosting = { provider: "", name: "", price: "", period: "month", currency: "EUR" };

function known(provider?: string) {
  return providerOptions.find((option) => option.value === provider);
}

// hostingLabel names who rents a machine: its provider account when MyInfra
// bought it, what the owner said otherwise.
export function hostingLabel(server: Server) {
  if (server.providerName) return server.providerName;
  const hosting = server.hosting;
  if (hosting?.provider === "other" && hosting.name) return hosting.name;
  return known(hosting?.provider)?.label ?? text("server.manual");
}

export function HostingMark({ server }: { server: Server }) {
  const logo = server.provider || known(server.hosting?.provider)?.value;
  return logo ? <ProviderLogo id={logo} /> : <Icon icon={ServerIcon} />;
}

// HostingFields asks who rents a machine added by hand and its price, so the
// machine counts in the cost like a bought one.
export function HostingFields({ value, onChange }: { value: Hosting; onChange: (value: Hosting) => void }) {
  const set = (patch: Partial<Hosting>) => onChange({ ...value, ...patch });
  return (
    <>
      <ProviderSelect
        label={text("server.provider")}
        value={value.provider ?? ""}
        onChange={(provider) => set({ provider })}
        options={[
          { value: "", label: text("hosting.none"), logo: "manual" },
          ...providerOptions.map((option) => ({ value: option.value, label: option.label, logo: option.value })),
          { value: "other", label: text("hosting.other"), logo: "manual" },
        ]}
      />
      {value.provider === "other" ? (
        <Field label={text("hosting.name")}>
          <Input value={value.name ?? ""} maxLength={60} required onChange={(e) => set({ name: e.target.value })} />
        </Field>
      ) : null}
      <Group label={text("hosting.price")} hint={text("hosting.priceHint")}>
        <div className="flex flex-wrap gap-2">
          <div className="min-w-28 flex-1">
            <Input
              value={value.price ?? ""}
              inputMode="decimal"
              placeholder="0.00"
              aria-label={text("hosting.price")}
              pattern="[0-9]+([.,][0-9]+)?"
              onChange={(e) => set({ price: e.target.value })}
            />
          </div>
          <div className="w-24">
            <Select value={value.currency || "EUR"} aria-label={text("hosting.currency")} onChange={(e) => set({ currency: e.target.value })}>
              <option value="EUR">€</option>
              <option value="USD">$</option>
            </Select>
          </div>
          <div className="w-36">
            <Select value={value.period || "month"} aria-label={text("server.billing")} onChange={(e) => set({ period: e.target.value })}>
              <option value="month">{text("hosting.perMonth")}</option>
              <option value="hour">{text("hosting.perHour")}</option>
            </Select>
          </div>
        </div>
      </Group>
    </>
  );
}

function draftOf(server: Server): Hosting {
  return { ...emptyHosting, ...server.hosting };
}

function same(a: Hosting, b: Hosting) {
  return (
    (a.provider ?? "") === (b.provider ?? "") &&
    (a.name ?? "") === (b.name ?? "") &&
    (a.price ?? "") === (b.price ?? "") &&
    (a.period || "month") === (b.period || "month") &&
    (a.currency || "EUR") === (b.currency || "EUR")
  );
}

// ServerHosting changes, on the page of a machine added by hand, who rents it and its price.
export function ServerHosting({ server, onSaved }: { server: Server; onSaved: (server: Server) => void }) {
  const toast = useToast();
  const [draft, setDraft] = useState(() => draftOf(server));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const saved = draftOf(server);

  useEffect(() => {
    setDraft(draftOf(server));
  }, [server.id]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const next = await api<Server>(`/api/servers/${server.id}/hosting`, { method: "PUT", body: JSON.stringify(draft) });
      onSaved(next);
      setDraft(draftOf(next));
      toast(text("hosting.saved"));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="mt-6 flex max-w-[720px] flex-col gap-4">
      <HostingFields value={draft} onChange={setDraft} />
      {error ? <ErrorText>{error}</ErrorText> : null}
      <div>
        <Button type="submit" variant="secondary" disabled={busy || same(draft, saved)}>
          {busy ? <Spinner /> : <Icon icon={Save} />}
          {text("common.save")}
        </Button>
      </div>
    </form>
  );
}

// ServerSSHUser is the account that prepares root's access on a machine that
// refuses root at first. It serves the next installation or retry.
export function ServerSSHUser({ server, onSaved }: { server: Server; onSaved: (server: Server) => void }) {
  const toast = useToast();
  const [draft, setDraft] = useState(server.sshUser ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setDraft(server.sshUser ?? "");
  }, [server.id]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const next = await api<Server>(`/api/servers/${server.id}/ssh-user`, { method: "PUT", body: JSON.stringify({ sshUser: draft.trim() }) });
      onSaved(next);
      setDraft(next.sshUser ?? "");
      toast(text("server.sshUserSaved"));
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="mt-6 flex max-w-[720px] flex-col gap-4">
      <Field label={text("server.sshUser")} hint={text("server.sshUserEditHint")}>
        <Input value={draft} placeholder="root" autoCapitalize="off" autoComplete="off" spellCheck={false} onChange={(e) => setDraft(e.target.value)} />
      </Field>
      {error ? <ErrorText>{error}</ErrorText> : null}
      <div>
        <Button type="submit" variant="secondary" disabled={busy || draft.trim() === (server.sshUser ?? "")}>
          {busy ? <Spinner /> : <Icon icon={Save} />}
          {text("common.save")}
        </Button>
      </div>
    </form>
  );
}
