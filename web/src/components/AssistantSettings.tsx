import { FormEvent, useEffect, useState } from "react";
import { KeyRound, LogIn } from "lucide-react";
import { api } from "../api";
import { text } from "../i18n";
import { ConfirmDialog, Spinner, useToast } from "../kit";
import { RowState } from "../rows";
import { Button, ErrorText, Field, Icon, Input, LoadingLines, Select, TextButton } from "../ui";

type Price = { provider: string; model: string; in_per_m: number; out_per_m: number };
type ProviderCapability = { provider: string; supports_subscription_auth: boolean };

export type AssistantStatus = {
  enabled: boolean;
  providers: string[];
  api_keys: string[];
  models: Record<string, unknown[]>;
  chatgpt_signin?: boolean;
  chatgpt_connected?: boolean;
};

const providerInfo: Record<string, { name: string; maker: string }> = {
  claude: { name: "Claude", maker: "Anthropic" },
  codex: { name: "Codex", maker: "OpenAI" },
};

/** The providers the assistant can use: a write-only key each, and the ChatGPT sign-in for Codex. */
export function AssistantProviders({ status, onChange }: { status: AssistantStatus; onChange: () => Promise<unknown> }) {
  const [caps, setCaps] = useState<ProviderCapability[] | null>(null);

  useEffect(() => {
    api<ProviderCapability[]>("/api/assistant/providers")
      .then(setCaps)
      .catch(() => setCaps([]));
  }, []);

  const ids = caps?.length ? caps.map((cap) => cap.provider) : Object.keys(providerInfo);
  return (
    <div className="flex max-w-[720px] flex-col gap-4">
      <p className="text-muted">{text("assistant.providersHint")}</p>
      {caps === null ? (
        <LoadingLines lines={4} />
      ) : (
        ids.map((id) => (
          <ProviderCard
            key={id}
            id={id}
            hasKey={status.api_keys.includes(id)}
            subscription={caps.find((cap) => cap.provider === id)?.supports_subscription_auth ?? false}
            chatgpt={id === "codex" && Boolean(status.chatgpt_signin)}
            connected={id === "codex" && Boolean(status.chatgpt_connected)}
            onChange={onChange}
          />
        ))
      )}
    </div>
  );
}

function ProviderCard({
  id,
  hasKey,
  subscription,
  chatgpt,
  connected,
  onChange,
}: {
  id: string;
  hasKey: boolean;
  subscription: boolean;
  chatgpt: boolean;
  connected: boolean;
  onChange: () => Promise<unknown>;
}) {
  const toast = useToast();
  const info = providerInfo[id] ?? { name: id, maker: "" };
  const [key, setKey] = useState("");
  const [type, setType] = useState<"api_key" | "subscription_token">("api_key");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [removing, setRemoving] = useState(false);
  const [leaving, setLeaving] = useState(false);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!key.trim() || busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`/api/assistant/keys/${id}`, { method: "PUT", body: JSON.stringify({ key: key.trim(), type }) });
      setKey("");
      toast(text("assistant.saved", { provider: info.name }));
      await onChange();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setBusy(false);
    }
  }

  const state = hasKey
    ? { label: text("assistant.stateKey"), tone: "ok" as const }
    : connected
      ? { label: text("assistant.stateChatgpt"), tone: "ok" as const }
      : { label: text("assistant.stateNone"), tone: "muted" as const };

  return (
    <section className="flex flex-col gap-4 rounded-md border border-line bg-surface p-4" aria-label={info.name}>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <h2>
          {info.name}
          {info.maker ? <span className="text-muted"> · {info.maker}</span> : null}
        </h2>
        <RowState tone={state.tone}>{state.label}</RowState>
      </div>

      <form onSubmit={save} className="flex flex-col gap-4">
        {subscription ? (
          <Field label={text("assistant.keyType")}>
            <Select value={type} onChange={(e) => setType(e.target.value as typeof type)}>
              <option value="api_key">{text("assistant.apiKey")}</option>
              <option value="subscription_token">{text("assistant.subscriptionToken")}</option>
            </Select>
          </Field>
        ) : null}
        <Field label={hasKey ? text("assistant.replaceKey") : text(type === "api_key" ? "assistant.apiKey" : "assistant.subscriptionToken")}>
          <div className="flex gap-3">
            <Input type="password" autoComplete="off" spellCheck={false} value={key} onChange={(e) => setKey(e.target.value)} placeholder={text("assistant.keyPlaceholder")} />
            <Button type="submit" variant="secondary" disabled={!key.trim() || busy}>
              {busy ? <Spinner /> : <Icon icon={KeyRound} />}
              {text("assistant.save")}
            </Button>
          </div>
        </Field>
        {error ? <ErrorText>{error}</ErrorText> : null}
        {hasKey ? (
          <div>
            <TextButton type="button" tone="danger" onClick={() => setRemoving(true)}>
              {text("assistant.remove")}
            </TextButton>
          </div>
        ) : null}
      </form>

      {chatgpt ? (
        <div className="flex flex-col gap-3 border-t border-line pt-4">
          <p className="text-[13px] text-muted">{text("assistant.chatgptHint")}</p>
          <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
            {/* Full-page navigation: the OAuth flow goes through OpenAI and comes back. */}
            <Button type="button" variant={connected ? "secondary" : "primary"} onClick={() => window.location.assign("/api/assistant/openai/start")} data-testid="continue-with-chatgpt">
              <Icon icon={LogIn} />
              {text(connected ? "assistant.chatgptReconnect" : "assistant.chatgpt")}
            </Button>
            {connected ? (
              <TextButton type="button" tone="danger" onClick={() => setLeaving(true)}>
                {text("assistant.chatgptDisconnect")}
              </TextButton>
            ) : null}
          </div>
        </div>
      ) : null}

      {removing ? (
        <ConfirmDialog
          danger
          title={text("assistant.removeTitle", { provider: info.name })}
          confirmLabel={text("assistant.remove")}
          onClose={() => setRemoving(false)}
          onConfirm={async () => {
            await api(`/api/assistant/keys/${id}`, { method: "DELETE" });
            setRemoving(false);
            toast(text("assistant.removed", { provider: info.name }));
            await onChange();
          }}
        >
          {text("assistant.removeHint", { provider: info.name })}
        </ConfirmDialog>
      ) : null}
      {leaving ? (
        <ConfirmDialog
          danger
          title={text("assistant.chatgptDisconnectTitle")}
          confirmLabel={text("assistant.chatgptDisconnect")}
          onClose={() => setLeaving(false)}
          onConfirm={async () => {
            await api("/api/assistant/openai", { method: "DELETE" });
            setLeaving(false);
            toast(text("assistant.chatgptOff"));
            await onChange();
          }}
        >
          {text("assistant.chatgptDisconnectHint")}
        </ConfirmDialog>
      ) : null}
    </section>
  );
}

/** Prices per model, to turn the tokens of a chat into a cost. Saved when a field is left. */
export function AssistantPrices() {
  const toast = useToast();
  const [prices, setPrices] = useState<Price[] | null>(null);
  const [error, setError] = useState("");

  const load = () =>
    api<Price[]>("/api/assistant/pricing")
      .then(setPrices)
      .catch((err) => setError(err instanceof Error ? err.message : text("common.error")));

  useEffect(() => {
    void load();
  }, []);

  async function save(price: Price, change: Partial<Price>) {
    const next = { ...price, ...change };
    if (next.in_per_m === price.in_per_m && next.out_per_m === price.out_per_m) return;
    try {
      await api("/api/assistant/pricing", { method: "PUT", body: JSON.stringify(next) });
      toast(text("assistant.priceSaved", { model: price.model }));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  return (
    <div className="flex max-w-[720px] flex-col gap-4">
      <p className="text-muted">{text("assistant.pricesHint")}</p>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {prices === null ? (
        <LoadingLines lines={3} />
      ) : prices.length === 0 ? (
        <p className="text-muted">{text("assistant.pricesEmpty")}</p>
      ) : (
        <div className="overflow-x-auto rounded-md border border-line bg-surface">
          <table className="w-full text-left">
            <thead className="text-[13px] text-muted">
              <tr>
                <th className="px-4 py-2 font-normal">{text("assistant.model")}</th>
                <th className="w-32 px-4 py-2 font-normal">{text("assistant.priceIn")}</th>
                <th className="w-32 px-4 py-2 font-normal">{text("assistant.priceOut")}</th>
              </tr>
            </thead>
            <tbody>
              {prices.map((price) => (
                <tr key={price.provider + price.model} className="border-t border-line">
                  <td className="px-4 py-2">
                    {price.model}
                    <span className="block text-[13px] text-muted">{providerInfo[price.provider]?.name ?? price.provider}</span>
                  </td>
                  <td className="px-4 py-2">
                    <Input type="number" min={0} step="0.01" defaultValue={price.in_per_m} aria-label={`${price.model} · ${text("assistant.priceIn")}`} onBlur={(e) => void save(price, { in_per_m: Number(e.target.value) })} />
                  </td>
                  <td className="px-4 py-2">
                    <Input type="number" min={0} step="0.01" defaultValue={price.out_per_m} aria-label={`${price.model} · ${text("assistant.priceOut")}`} onBlur={(e) => void save(price, { out_per_m: Number(e.target.value) })} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
