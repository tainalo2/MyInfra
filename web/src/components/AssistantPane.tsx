import { useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { ArrowLeft, Check, Clock, History, KeyRound, Plus, SendHorizontal, Trash2, X } from 'lucide-react';
import { Markdown } from './Markdown';
import { TokenBudgetBanner } from './TokenBudgetBanner';
import { useSession } from '../session';
import { useAssistant, type Action, type ActionState, type Msg } from '../assistant-context';
import { patchesFromReply } from './diff-blocks';
import { currentLang, currentLocale, text, type MsgKey } from '../i18n';
import { ConfirmDialog, Spinner, useToast } from '../kit';
import { useI18n } from '../lang';
import { Button, Icon, IconButton, Input, LoadingLines, Select, focus } from '../ui';

// What the provider is sent: text only, consecutive turns of one side joined.
function forModel(msgs: Msg[]) {
  const out: { role: Msg['role']; content: string }[] = [];
  for (const m of msgs) {
    if (!m.content) continue;
    const last = out[out.length - 1];
    if (last && last.role === m.role) last.content += `\n\n${m.content}`;
    else out.push({ role: m.role, content: m.content });
  }
  return out;
}
// The provider says which levels each model takes: low to ultra for OpenAI.
type ReasoningEffort = string;

interface ModelCapability {
  model: string;
  name?: string;
  supported_reasons: ReasoningEffort[];
  default_reason: ReasoningEffort;
}

type Summary = { id: string; title: string; updated_at: string };

// One line of what the assistant is doing: what it says, or a tool and its state.
type Live =
  | { kind: 'step'; text: string }
  | { kind: 'tool'; id: string; title: string; write?: boolean; state: 'running' | 'done' | 'error' | 'waiting' };

type ChatAnswer = { text?: string; error?: string; tools?: Msg['tools']; actions?: Action[]; assistant_unavailable?: boolean; status?: number };

const knownReasons = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'];

function reasonLabel(effort: string) {
  return knownReasons.includes(effort) ? text(`assistant.reasoning.${effort}` as MsgKey) : effort;
}

/**
 * Assistant chat scoped to a functional context. The whole pane hides behind one flag (/api/assistant/status).
 * A read-only account sees the pane in consultation mode: the history stays, the form that would open a new
 * turn with the provider is absent. The server refuses that turn anyway (403 on /api/assistant/chat).
 *
 * The conversation itself lives in the assistant context, above the pages: the side panel and the
 * assistant's page show the same one, and it follows the person from page to page.
 */
export function AssistantPane() {
  useI18n();
  const { canChat, canAdmin } = useSession();
  const { pageContext, setIsOpen, conversationId, msgs, setMsgs, settings, setSettings, newConversation, restore } = useAssistant();
  const location = useLocation();
  const [loaded, setLoaded] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [providers, setProviders] = useState<string[]>([]);
  const [models, setModels] = useState<Record<string, ModelCapability[]>>({});
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const [browsing, setBrowsing] = useState(false);
  // What the assistant is doing while it works: what it says, each tool.
  const [live, setLive] = useState<Live[]>([]);
  const listRef = useRef<HTMLUListElement>(null);

  // The latest stays in sight: a new message, a step, the answer.
  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [msgs, live, busy, browsing]);

  useEffect(() => {
    fetch('/api/assistant/status')
      .then((r) => r.json())
      .then((s: { enabled: boolean; providers: string[]; models: Record<string, ModelCapability[]> }) => {
        setEnabled(s.enabled);
        setProviders(s.providers ?? []);
        setModels(s.models || {});
      })
      .catch(() => setEnabled(false))
      .finally(() => setLoaded(true));
    // The conversation of the last visit comes back, once per visit.
    restore();
  }, [restore]);

  // The choice is the conversation's, kept with it: what it names is used
  // while the provider still offers it, else the provider's first and its
  // default level. Nothing here resets it when the pane is drawn again.
  const provider = settings.provider && providers.includes(settings.provider) ? settings.provider : providers[0] ?? '';
  const offered = models[provider] ?? [];
  const current = offered.find((m) => m.model === settings.model) ?? offered[0];
  const model = current?.model ?? '';
  const supportedReasons: ReasoningEffort[] = current?.supported_reasons ?? [];
  const reasoningEffort = settings.effort && supportedReasons.includes(settings.effort) ? settings.effort : current?.default_reason ?? 'medium';

  if (!loaded) return null;
  if (!enabled) {
    // Nothing to chat with yet: say so, and where it is set up.
    return (
      <div className="flex flex-col items-start gap-4 p-4">
        <p className="text-muted">{text(canAdmin ? 'assistant.off' : 'assistant.offAsk')}</p>
        {canAdmin ? (
          <Link to="/assistant-ia" onClick={() => setIsOpen(false)} className={`inline-flex items-center gap-2 rounded-md border border-line bg-surface px-4 py-2 text-ink ${focus}`}>
            <Icon icon={KeyRound} />
            {text('assistant.setup')}
          </Link>
        ) : null}
      </div>
    );
  }

  async function send() {
    if (!canChat || !input.trim() || busy || !model) return;
    const next = [...msgs, { role: 'user' as const, content: input }];
    setMsgs(next);
    setInput('');
    setBusy(true);
    setLive([]);
    try {
      const r = await fetch('/api/assistant/chat', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          session: conversationId,
          provider,
          model,
          reasoning_effort: reasoningEffort,
          // Where the user is: the server tells the model, with the infrastructure.
          path: location.pathname,
          lang: currentLang(),
          ...pageContext,
          messages: forModel(next),
          stream: true,
        }),
      });
      // The answer comes as a stream of JSON lines: progress, then "done" or
      // "error". A refusal before the turn starts is plain JSON.
      let d: ChatAnswer = {};
      let status = r.status;
      if (r.body && (r.headers.get('content-type') ?? '').includes('ndjson')) {
        const reader = r.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        const handle = (raw: string) => {
          if (!raw.trim()) return;
          const ev = JSON.parse(raw);
          if (ev.type === 'step') setLive((all) => [...all, { kind: 'step', text: ev.text }]);
          else if (ev.type === 'tool') {
            setLive((all) => {
              const at = all.findIndex((l) => l.kind === 'tool' && l.id === ev.id);
              const step: Live = { kind: 'tool', id: ev.id, title: ev.title, write: ev.write, state: ev.state };
              return at < 0 ? [...all, step] : all.map((l, i) => (i === at ? step : l));
            });
          } else if (ev.type === 'done' || ev.type === 'error') {
            d = ev;
            if (ev.type === 'error') status = ev.status ?? 502;
          }
        };
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          let at = buffer.indexOf('\n');
          while (at >= 0) {
            handle(buffer.slice(0, at));
            buffer = buffer.slice(at + 1);
            at = buffer.indexOf('\n');
          }
        }
        handle(buffer);
      } else {
        d = await r.json();
      }
      const reply = status === 403
        ? text('assistant.forbidden')
        : d.assistant_unavailable ? text('assistant.unavailable') : d.text ?? d.error ?? '';
      const actions: Action[] = (d.actions ?? []).map((a: Action) => ({ ...a, state: 'pending' as const }));
      setMsgs([...next, { role: 'assistant', content: reply, tools: d.tools, actions }]);
      // A reply carrying ```diff blocks is a patch: it joins the staging,
      // and the split view (stream) shows the hunks as they land.
      const patches = patchesFromReply(reply);
      if (patches.length > 0) {
        const staged = await fetch('/api/diff/staging', {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ kind: 'template', name: pageContext?.resource_id || 'unknown', ...pageContext, patches }),
        });
        if (!staged.ok) setMsgs([...next, { role: 'assistant', content: `${reply}\n\n${text('assistant.notStaged', { status: staged.status })}` }]);
      }
    } finally {
      setBusy(false);
    }
  }

  function updateAction(id: string, patch: Partial<Action>) {
    setMsgs((all) => all.map((m) => (m.actions?.some((a) => a.id === id)
      ? { ...m, actions: m.actions.map((a) => (a.id === id ? { ...a, ...patch } : a)) }
      : m)));
  }

  // The person's decision on a purchase or a destruction. Its outcome joins
  // the conversation as a note, so the model knows on the next turn what was done.
  async function decide(action: Action, confirm: boolean) {
    updateAction(action.id, { state: 'busy' });
    try {
      const r = await fetch(`/api/assistant/actions/${encodeURIComponent(action.id)}`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ confirm }),
      });
      const d = await r.json().catch(() => ({}));
      if (r.status === 404) {
        updateAction(action.id, { state: 'expired' });
        return;
      }
      if (!r.ok) {
        updateAction(action.id, { state: 'failed', result: d.error ?? String(r.status) });
        return;
      }
      if (d.action) {
        // What the recap showed changed meanwhile: a new recap to agree to.
        updateAction(action.id, { ...d.action, state: 'pending' });
        return;
      }
      const state: ActionState = !confirm ? 'cancelled' : d.done ? 'done' : 'failed';
      updateAction(action.id, { state, result: d.text });
      const noteState = text(state === 'done' ? 'assistant.actionNoteDone' : state === 'cancelled' ? 'assistant.actionNoteCancelled' : 'assistant.actionNoteFailed');
      const note = text('assistant.actionNote', { title: action.title, state: noteState }) + (d.text ? `\n${d.text}` : '');
      setMsgs((all) => [...all, { role: 'assistant', content: note, note: true }]);
    } catch {
      updateAction(action.id, { state: 'pending' });
    }
  }

  const title = msgs.find((m) => m.role === 'user' && m.content.trim())?.content.split('\n')[0] ?? text('assistant.newConversation');

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Which conversation this is, the others, and a new one. */}
      <div className="flex items-center gap-3 border-b border-line px-4 py-2">
        {browsing ? (
          <button type="button" onClick={() => setBrowsing(false)} className={`-mx-1 flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 py-1 text-left ${focus}`}>
            <Icon icon={ArrowLeft} />
            <span className="truncate">{text('assistant.backToChat')}</span>
          </button>
        ) : (
          <span className="min-w-0 flex-1 truncate text-[13px] text-muted" title={title}>{title}</span>
        )}
        <div className="flex shrink-0 items-center gap-4">
          {!browsing ? <IconButton icon={History} label={text('assistant.conversations')} onClick={() => setBrowsing(true)} /> : null}
          <IconButton icon={Plus} label={text('assistant.newConversation')} onClick={() => { newConversation(); setBrowsing(false); }} />
        </div>
      </div>
      {browsing ? (
        <Conversations current={conversationId} onOpened={() => setBrowsing(false)} />
      ) : (
        <>
          <TokenBudgetBanner sessionId={conversationId} />
          {/* The model takes a line of its own: its names are long. */}
          <div className="flex flex-col gap-2 border-b border-line px-4 py-3">
            <Select aria-label={text('assistant.model')} value={model} onChange={(e) => setSettings({ provider, model: e.target.value })}>
              {models[provider]?.map((m) => <option key={m.model} value={m.model}>{m.name || m.model}</option>)}
            </Select>
            <div className={`grid gap-2 ${providers.length > 1 ? 'grid-cols-2' : ''}`}>
              {providers.length > 1 ? (
                <Select aria-label={text('assistant.provider')} value={provider} onChange={(e) => setSettings({ provider: e.target.value, model: undefined, effort: undefined })}>
                  {providers.map((p) => <option key={p} value={p}>{providerName(p)}</option>)}
                </Select>
              ) : null}
              <Select aria-label={text('assistant.reasoning')} value={reasoningEffort} onChange={(e) => setSettings({ provider, model, effort: e.target.value })}>
                {supportedReasons.map((r) => <option key={r} value={r}>{`${text('assistant.reasoning')} · ${reasonLabel(r)}`}</option>)}
              </Select>
            </div>
          </div>
          <ul ref={listRef} className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-4 py-4" aria-live="polite">
            {msgs.length === 0 ? <li className="text-muted">{text('assistant.empty')}</li> : null}
            {msgs.map((m, i) =>
              m.role === 'user' ? (
                <li key={i} className="flex max-w-[85%] flex-col gap-1 self-end">
                  <span className="text-right text-[13px] text-muted">{text('assistant.you')}</span>
                  <span className="whitespace-pre-wrap break-words rounded-md bg-status px-3 py-2">{m.content}</span>
                </li>
              ) : m.note ? (
                <li key={i} className="whitespace-pre-wrap break-words border-l-2 border-line pl-3 text-[13px] text-muted">{m.content}</li>
              ) : (
                <li key={i} className="flex flex-col gap-2">
                  <span className="text-[13px] text-muted">{text('nav.assistant')}</span>
                  <ToolsLine tools={m.tools} />
                  {m.content ? <Markdown text={m.content} /> : null}
                  {m.actions?.map((a) => <ActionCard key={a.id} action={a} canAct={canChat} onDecide={(ok) => void decide(a, ok)} />)}
                </li>
              ),
            )}
            {busy ? <LiveSteps live={live} /> : null}
          </ul>
          {canChat ? (
            <form className="flex gap-2 border-t border-line px-4 py-3" onSubmit={(e) => { e.preventDefault(); void send(); }}>
              <Input aria-label={text('assistant.placeholder')} placeholder={text('assistant.placeholder')} value={input} onChange={(e) => setInput(e.target.value)} />
              <Button type="submit" disabled={busy || !input.trim() || !model}>
                <Icon icon={SendHorizontal} />
                <span className="sr-only sm:not-sr-only">{text('assistant.send')}</span>
              </Button>
            </form>
          ) : (
            <p className="border-t border-line px-4 py-3 text-[13px] text-muted">{text('assistant.readOnly')}</p>
          )}
        </>
      )}
    </div>
  );
}

// LiveSteps follows the turn while it runs: what the model says it is doing,
// then each tool, with its state in a word and a mark, never in colour alone.
function LiveSteps({ live }: { live: Live[] }) {
  return (
    <li className="flex flex-col gap-2">
      <span className="text-[13px] text-muted">{text('nav.assistant')}</span>
      {live.map((l, i) =>
        l.kind === 'step' ? (
          <span key={i} className="border-l-2 border-line pl-3 text-muted">{l.text}</span>
        ) : (
          <span key={l.id || i} className="flex items-center gap-2 text-[13px]">
            {l.state === 'running' ? <Spinner /> : null}
            {l.state === 'done' ? <Icon icon={Check} className="text-ok" /> : null}
            {l.state === 'error' ? <Icon icon={X} className="text-bad" /> : null}
            {l.state === 'waiting' ? <Icon icon={Clock} className="text-warn" /> : null}
            <span className={l.state === 'running' ? 'text-ink' : 'text-muted'}>{l.title}</span>
            <span className="text-faint">· {text(`assistant.live.${l.state}` as MsgKey)}</span>
          </span>
        ),
      )}
      <span className="flex items-center gap-2 text-muted">
        <Spinner />
        {text(live.length ? 'assistant.working' : 'assistant.thinking')}
      </span>
    </li>
  );
}

// ToolsLine says what a reply rests on: the tools read, and apart, what it did.
function ToolsLine({ tools }: { tools?: Msg['tools'] }) {
  if (!tools?.length) return null;
  const done = tools.filter((t) => t.write && !t.is_error);
  const read = tools.filter((t) => !t.write || t.is_error);
  return (
    <>
      {read.length ? <span className="text-[13px] text-muted">{text('assistant.toolsUsed', { tools: read.map((t) => t.title).join(' · ') })}</span> : null}
      {done.length ? (
        <span className="inline-flex items-center gap-1.5 text-[13px] text-ok">
          <span aria-hidden className="size-2 rounded-full bg-ok" />
          {text('assistant.toolsDone', { tools: done.map((t) => t.title).join(' · ') })}
        </span>
      ) : null}
    </>
  );
}

// Conversations lists the person's conversations, the latest first: one opens
// it where it stopped, the bin forgets it.
function Conversations({ current, onOpened }: { current: string; onOpened: () => void }) {
  const toast = useToast();
  const { openConversation, newConversation } = useAssistant();
  const [list, setList] = useState<Summary[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [removing, setRemoving] = useState<Summary | null>(null);

  const load = () =>
    fetch('/api/assistant/conversations')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((rows: Summary[]) => setList(rows))
      .catch(() => setFailed(true));

  useEffect(() => {
    void load();
  }, []);

  const when = (iso: string) => {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleString(currentLocale(), { dateStyle: 'medium', timeStyle: 'short' });
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      {failed ? (
        <p className="p-4 text-muted">{text('assistant.conversationsFailed')}</p>
      ) : list === null ? (
        <div className="p-4"><LoadingLines lines={4} /></div>
      ) : list.length === 0 ? (
        <p className="p-4 text-muted">{text('assistant.conversationsEmpty')}</p>
      ) : (
        <ul>
          {list.map((c) => (
            <li key={c.id} className={`flex items-center gap-3 border-b border-line px-4 py-2 ${c.id === current ? 'bg-status' : ''}`}>
              <button
                type="button"
                onClick={() => void openConversation(c.id).then((ok) => ok && onOpened())}
                className={`-mx-1 flex min-w-0 flex-1 flex-col rounded-md px-1 py-1 text-left ${focus}`}
                aria-current={c.id === current ? 'true' : undefined}
              >
                <span className="truncate">{c.title || text('assistant.untitled')}</span>
                <span className="text-[13px] text-muted">{when(c.updated_at)}</span>
              </button>
              <IconButton icon={Trash2} label={text('assistant.deleteConversation')} onClick={() => setRemoving(c)} />
            </li>
          ))}
        </ul>
      )}
      {removing ? (
        <ConfirmDialog
          danger
          title={text('assistant.deleteConversationTitle', { title: removing.title || text('assistant.untitled') })}
          confirmLabel={text('assistant.deleteConversation')}
          onClose={() => setRemoving(null)}
          onConfirm={async () => {
            const r = await fetch(`/api/assistant/conversations/${encodeURIComponent(removing.id)}`, { method: 'DELETE' });
            if (!r.ok) throw new Error(text('common.error'));
            if (removing.id === current) newConversation();
            setRemoving(null);
            toast(text('assistant.conversationDeleted'));
            await load();
          }}
        >
          {text('assistant.deleteConversationHint')}
        </ConfirmDialog>
      ) : null}
    </div>
  );
}

function ActionCard({ action, canAct, onDecide }: { action: Action; canAct: boolean; onDecide: (confirm: boolean) => void }) {
  const state = action.state ?? 'pending';
  const outcome =
    state === 'done' ? text('assistant.actionDone', { title: action.title })
    : state === 'cancelled' ? text('assistant.actionCancelled', { title: action.title })
    : state === 'failed' ? text('assistant.actionFailed', { title: action.title })
    : state === 'expired' ? text('assistant.actionExpired')
    : '';
  return (
    <div className="flex flex-col gap-2 rounded-md border border-line bg-surface px-3 py-2">
      <span className="text-[13px] font-medium">{state === 'pending' || state === 'busy' ? `${text('assistant.toConfirm')} · ${action.title}` : outcome}</span>
      <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words text-[13px]">{action.recap}</pre>
      {action.result && state !== 'pending' ? <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words text-[13px] text-muted">{action.result}</pre> : null}
      {canAct && (state === 'pending' || state === 'busy') ? (
        <div className="flex gap-2">
          <Button type="button" disabled={state === 'busy'} onClick={() => onDecide(true)}>
            {state === 'busy' ? <Spinner /> : null}
            {text('assistant.confirm')}
          </Button>
          <Button type="button" variant="secondary" disabled={state === 'busy'} onClick={() => onDecide(false)}>
            {text('assistant.cancel')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function providerName(id: string) {
  return id === 'claude' ? 'Claude' : id === 'codex' ? 'Codex' : id;
}
