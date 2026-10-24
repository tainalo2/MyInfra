import { FormEvent, useEffect, useState } from "react";
import { BookOpen, Bot, KeyRound, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { api, type ApiToken, type McpInfo } from "./api";
import { Code } from "./code-lazy";
import { currentLocale, text, type MsgKey } from "./i18n";
import { ConfirmDialog, CopyButton, Sheet, Tabs, useToast } from "./kit";
import { useI18n } from "./lang";
import { useSession } from "./session";
import { Button, ErrorText, Field, Fold, Icon, Input, LoadingLines, PageTitle, Select, TextButton } from "./ui";
import { EventList } from "./world";

type Role = ApiToken["role"];

const roles: Role[] = ["read", "deploy", "admin"];
const rank: Record<Role, number> = { read: 1, deploy: 2, admin: 3 };

const lifetimes: { days: number; label: MsgKey }[] = [
  { days: 30, label: "mcp.days30" },
  { days: 90, label: "mcp.days90" },
  { days: 365, label: "mcp.days365" },
  { days: 0, label: "mcp.daysNever" },
];

// Where the token goes in each snippet before one is made.
const slot = "VOTRE_JETON";

function date(iso?: string) {
  if (!iso) return "";
  const value = new Date(iso);
  if (Number.isNaN(value.getTime())) return "";
  return value.toLocaleDateString(currentLocale(), { dateStyle: "medium" });
}

export function McpPage() {
  useI18n();
  const { role: myRole, canAdmin } = useSession();
  const toast = useToast();
  const [info, setInfo] = useState<McpInfo | null>(null);
  const [tokens, setTokens] = useState<ApiToken[] | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("read");
  const [days, setDays] = useState(90);
  const [made, setMade] = useState<ApiToken | null>(null);
  const [revoking, setRevoking] = useState<ApiToken | null>(null);
  const [guide, setGuide] = useState(false);

  async function reload() {
    setTokens(await api<ApiToken[]>("/api/mcp/tokens"));
  }

  useEffect(() => {
    api<McpInfo>("/api/mcp")
      .then(setInfo)
      .catch((err) => setError(err instanceof Error ? err.message : text("common.error")));
    reload().catch((err) => setError(err instanceof Error ? err.message : text("common.error")));
  }, []);

  const allowed = roles.filter((item) => myRole && rank[item] <= rank[myRole as Role]);

  async function create(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const token = await api<ApiToken>("/api/mcp/tokens", { method: "POST", body: JSON.stringify({ name, role, days }) });
      setMade(token);
      setCreating(false);
      setName("");
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  async function revoke(token: ApiToken) {
    await api(`/api/mcp/tokens/${token.id}`, { method: "DELETE" });
    setRevoking(null);
    if (made?.id === token.id) setMade(null);
    toast(text("mcp.revoked", { name: token.name }));
    await reload();
  }

  const url = info?.url ?? "";

  return (
    <>
      <PageTitle
        title={text("nav.mcp")}
        icon={Bot}
        action={
          <TextButton type="button" onClick={() => setGuide(true)}>
            <Icon icon={BookOpen} />
            {text("mcp.guide")}
          </TextButton>
        }
      />
      <div className="flex max-w-[860px] flex-col gap-10">
        <p className="text-muted">{text("mcp.intro")}</p>
        {error ? <ErrorText>{error}</ErrorText> : null}

        <section className="flex flex-col gap-3">
          <h2 className="text-base">{text("mcp.address")}</h2>
          <p className="text-muted">{text("mcp.addressHint")}</p>
          {url ? (
            <div className="flex items-center gap-3 rounded-md border border-line bg-surface px-3 py-2">
              <code className="min-w-0 flex-1 break-all font-mono text-[13px]">{url}</code>
              <CopyButton value={url} />
            </div>
          ) : (
            <LoadingLines lines={1} />
          )}
        </section>

        <section className="flex flex-col gap-4">
          <div className="flex flex-wrap items-baseline justify-between gap-4">
            <h2 className="text-base">{text("mcp.tokens")}</h2>
            {!creating ? (
              <Button type="button" variant="secondary" onClick={() => setCreating(true)}>
                <Icon icon={Plus} />
                {text("mcp.tokenNew")}
              </Button>
            ) : null}
          </div>
          <p className="text-muted">{text("mcp.tokensHint")}</p>

          {creating ? (
            <form onSubmit={create} className="flex flex-col gap-5 rounded-md border border-line p-4">
              <Field label={text("mcp.tokenName")} hint={text("mcp.tokenNameHint")}>
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={text("mcp.tokenNamePlaceholder")} required maxLength={80} autoFocus />
              </Field>
              <Field label={text("users.role")} hint={text(`mcp.roleHint.${role}` as MsgKey)}>
                <Select value={role} onChange={(e) => setRole(e.target.value as Role)}>
                  {allowed.map((item) => (
                    <option key={item} value={item}>
                      {text(`role.${item}` as MsgKey)}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label={text("mcp.tokenDays")}>
                <Select value={String(days)} onChange={(e) => setDays(Number(e.target.value))}>
                  {lifetimes.map((item) => (
                    <option key={item.days} value={item.days}>
                      {text(item.label)}
                    </option>
                  ))}
                </Select>
              </Field>
              <div className="flex items-center gap-6">
                <Button type="submit">
                  <Icon icon={KeyRound} />
                  {text("mcp.create")}
                </Button>
                <TextButton type="button" onClick={() => setCreating(false)}>
                  {text("common.cancel")}
                </TextButton>
              </div>
            </form>
          ) : null}

          {made?.token ? (
            <div className="flex flex-col gap-3 rounded-md border border-warn p-4" role="status">
              <p className="font-medium">{text("mcp.shownOnce", { name: made.name })}</p>
              <div className="flex items-center gap-3 rounded-md border border-line bg-surface px-3 py-2">
                <code className="min-w-0 flex-1 break-all font-mono text-[13px]" data-testid="mcp-secret">
                  {made.token}
                </code>
                <CopyButton value={made.token} />
              </div>
              <div className="flex flex-wrap items-center gap-6">
                <Button type="button" onClick={() => setGuide(true)}>
                  <Icon icon={BookOpen} />
                  {text("mcp.setup")}
                </Button>
                <TextButton type="button" onClick={() => setMade(null)}>
                  {text("mcp.saved")}
                </TextButton>
              </div>
            </div>
          ) : null}

          {tokens === null ? (
            <LoadingLines lines={2} />
          ) : tokens.length === 0 ? (
            <p className="text-muted">{text("mcp.tokenEmpty")}</p>
          ) : (
            <ul>
              {tokens.map((token) => (
                <li key={token.id} className="flex flex-wrap items-start justify-between gap-4 border-t border-line py-3">
                  <span className="flex min-w-0 items-start gap-3">
                    <Icon icon={KeyRound} className="mt-1 text-muted" />
                    <span className="min-w-0">
                      <span className="block">
                        {token.name} <span className="text-muted">· {text(`role.${token.role}` as MsgKey)}</span>
                      </span>
                      <span className="block text-[13px] text-muted">
                        {[
                          !token.mine && token.user ? text("mcp.owner", { user: token.user }) : "",
                          text("mcp.createdOn", { date: date(token.createdAt) }),
                          token.lastUsedAt ? text("mcp.lastUsed", { date: date(token.lastUsedAt) }) : text("mcp.neverUsed"),
                          token.expiresAt ? text("mcp.expires", { date: date(token.expiresAt) }) : text("mcp.noExpiry"),
                        ]
                          .filter(Boolean)
                          .join(" · ")}
                      </span>
                    </span>
                  </span>
                  {token.mine || canAdmin ? (
                    <TextButton type="button" tone="danger" onClick={() => setRevoking(token)}>
                      <Icon icon={Trash2} />
                      {text("mcp.revoke")}
                    </TextButton>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className="flex flex-col gap-3">
          <h2 className="text-base">{text("mcp.examples")}</h2>
          <ul className="flex list-disc flex-col gap-1 pl-5 text-muted">
            {(["mcp.ex1", "mcp.ex2", "mcp.ex3", "mcp.ex4", "mcp.ex5", "mcp.ex6"] as MsgKey[]).map((key) => (
              <li key={key}>« {text(key)} »</li>
            ))}
          </ul>
          <p className="flex items-start gap-2 text-muted">
            <Icon icon={ShieldCheck} className="mt-1 shrink-0" />
            {text("mcp.safety")}
          </p>
        </section>

        {info ? (
          <Fold title={text("mcp.tools", { count: String(info.tools.length) })}>
            <p className="mb-3 text-muted">{text("mcp.toolsHint")}</p>
            <ul>
              {info.tools.map((tool) => (
                <li key={tool.name} className="flex flex-wrap items-baseline justify-between gap-3 border-t border-line py-2">
                  <span>
                    {tool.title} <span className="font-mono text-[12px] text-muted">{tool.name}</span>
                  </span>
                  <span className="text-[13px] text-muted">
                    {[text(`role.${tool.role}` as MsgKey), tool.destructive ? text("mcp.toolAsk") : tool.write ? text("mcp.toolWrite") : text("mcp.toolRead")].join(" · ")}
                  </span>
                </li>
              ))}
            </ul>
          </Fold>
        ) : null}

        <section className="flex flex-col gap-3">
          <h2 className="text-base">{text("mcp.recent")}</h2>
          <EventList source="mcp" limit={10} />
        </section>
      </div>

      {guide ? <McpGuide url={url} token={made?.token ?? ""} onClose={() => setGuide(false)} /> : null}
      {revoking ? (
        <ConfirmDialog
          title={text("mcp.revokeTitle", { name: revoking.name })}
          confirmLabel={text("mcp.revoke")}
          danger
          onConfirm={() => revoke(revoking)}
          onClose={() => setRevoking(null)}
        >
          {text("mcp.revokeHint")}
        </ConfirmDialog>
      ) : null}
    </>
  );
}

type Client = "code" | "desktop" | "cursor" | "vscode" | "other";

const clients: { key: Client; label: string; steps: MsgKey[]; language: "shell" | "json"; file?: MsgKey }[] = [
  { key: "code", label: "Claude Code", steps: ["mcp.code1", "mcp.code2", "mcp.code3"], language: "shell" },
  { key: "desktop", label: "Claude Desktop", steps: ["mcp.desktop1", "mcp.desktop2", "mcp.desktop3", "mcp.desktop4"], language: "json" },
  { key: "cursor", label: "Cursor", steps: ["mcp.cursor1", "mcp.cursor2", "mcp.cursor3"], language: "json" },
  { key: "vscode", label: "VS Code", steps: ["mcp.vscode1", "mcp.vscode2", "mcp.vscode3"], language: "json" },
  { key: "other", label: "", steps: ["mcp.other1", "mcp.other2", "mcp.other3"], language: "shell" },
];

// snippet is what to paste in each client, filled with the address and,
// right after it is made, the token.
export function snippet(client: Client, url: string, token: string) {
  const secret = token || slot;
  switch (client) {
    case "code":
      return `claude mcp add --transport http myinfra ${url} \\\n  --header "Authorization: Bearer ${secret}"`;
    case "desktop":
      return JSON.stringify(
        {
          mcpServers: {
            myinfra: {
              command: "npx",
              args: ["-y", "mcp-remote", url, "--header", "Authorization:${MYINFRA_AUTH}"],
              env: { MYINFRA_AUTH: `Bearer ${secret}` },
            },
          },
        },
        null,
        2,
      );
    case "cursor":
      return JSON.stringify({ mcpServers: { myinfra: { url, headers: { Authorization: `Bearer ${secret}` } } } }, null, 2);
    case "vscode":
      return JSON.stringify({ servers: { myinfra: { type: "http", url, headers: { Authorization: `Bearer ${secret}` } } } }, null, 2);
    default:
      return `URL: ${url}\nTransport: Streamable HTTP\nHeader: Authorization: Bearer ${secret}`;
  }
}

// McpGuide walks through plugging an assistant in, one client at a time.
export function McpGuide({ url, token, onClose }: { url: string; token: string; onClose: () => void }) {
  useI18n();
  const [active, setActive] = useState<Client>("code");
  const client = clients.find((item) => item.key === active) ?? clients[0];
  const value = snippet(active, url, token);
  return (
    <Sheet title={text("mcp.guideTitle")} onClose={onClose} wide>
      <div className="flex flex-col gap-5">
        <p className="text-muted">{text("mcp.guideIntro")}</p>
        <Tabs
          tabs={clients.map((item) => ({ key: item.key, label: item.label || text("mcp.otherClient") }))}
          active={active}
          onChange={setActive}
          label={text("mcp.guideTitle")}
        />
        <ol className="flex list-decimal flex-col gap-2 pl-5" role="tabpanel">
          {client.steps.map((step) => (
            <li key={step}>{text(step)}</li>
          ))}
        </ol>
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-3">
            <span className="text-muted">{token ? text("mcp.snippetFilled") : text("mcp.snippetSlot", { slot })}</span>
            <CopyButton value={value} />
          </div>
          <Code value={value} language={client.language} readOnly label={client.label || text("mcp.otherClient")} minHeight="4rem" maxHeight="18rem" />
        </div>
        <p className="flex items-start gap-2 text-muted">
          <Icon icon={ShieldCheck} className="mt-1 shrink-0" />
          {text("mcp.guideSafety")}
        </p>
      </div>
    </Sheet>
  );
}
