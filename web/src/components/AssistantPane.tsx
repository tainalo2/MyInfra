import { useEffect, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { KeyRound, SendHorizontal } from 'lucide-react';
import { TokenBudgetBanner } from './TokenBudgetBanner';
import { useSession } from '../session';
import { useAssistant } from '../assistant-context';
import { patchesFromReply } from './diff-blocks';
import { currentLang, text, type MsgKey } from '../i18n';
import { Spinner } from '../kit';
import { useI18n } from '../lang';
import { Button, Icon, Input, Select, focus } from '../ui';

// A change the model asked for: nothing runs until the person confirms here.
type ActionState = 'pending' | 'busy' | 'done' | 'cancelled' | 'failed' | 'expired';
type Action = { id: string; tool: string; title: string; recap: string; state?: ActionState; result?: string };
type UsedTool = { name: string; title: string; is_error?: boolean };
type Msg = { role: 'user' | 'assistant'; content: string; tools?: UsedTool[]; actions?: Action[]; note?: boolean };

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

const knownReasons = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'];

function reasonLabel(effort: string) {
  return knownReasons.includes(effort) ? text(`assistant.reasoning.${effort}` as MsgKey) : effort;
}

/**
 * Assistant chat scoped to a functional context. The whole pane hides behind one flag (/api/assistant/status).
 * A read-only account sees the pane in consultation mode: the history stays, the form that would open a new
 * turn with the provider is absent. The server refuses that turn anyway (403 on /api/assistant/chat).
 *
 * The context is resolved dynamically by the Harness Router based on the current page/resource.
 */
export function AssistantPane() {
  useI18n();
  const { canChat, canAdmin } = useSession();
  const { pageContext, setIsOpen } = useAssistant();
  const location = useLocation();
  const [loaded, setLoaded] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [providers, setProviders] = useState<string[]>([]);
  const [models, setModels] = useState<Record<string, ModelCapability[]>>({});
  const [provider, setProvider] = useState('');
  const [model, setModel] = useState('');
  const [reasoningEffort, setReasoningEffort] = useState<ReasoningEffort>('medium');
  const [supportedReasons, setSupportedReasons] = useState<ReasoningEffort[]>([]);
  const [session] = useState(() => crypto.randomUUID());
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    fetch('/api/assistant/status')
      .then((r) => r.json())
      .then((s: { enabled: boolean; providers: string[]; models: Record<string, ModelCapability[]> }) => {
        setEnabled(s.enabled);
        setProviders(s.providers);
        setModels(s.models || {});

        // Set initial provider
        if (s.providers && s.providers.length > 0) {
          setProvider(s.providers[0]);

          // Set initial model from first provider
          const providerModels = s.models?.[s.providers[0]];
          if (providerModels && providerModels.length > 0) {
            const firstModel = providerModels[0];
            setModel(firstModel.model);
            setSupportedReasons(firstModel.supported_reasons);
            setReasoningEffort(firstModel.default_reason);
          }
        }
      })
      .catch(() => setEnabled(false))
      .finally(() => setLoaded(true));
  }, []);

  // Update model list and reset model selection when provider changes
  useEffect(() => {
    if (provider && models[provider]) {
      const providerModels = models[provider];
      if (providerModels.length > 0) {
        const firstModel = providerModels[0];
        setModel(firstModel.model);
        setSupportedReasons(firstModel.supported_reasons);
        setReasoningEffort(firstModel.default_reason);
      }
    }
  }, [provider, models]);

  // Update reasoning support when model changes
  useEffect(() => {
    if (provider && models[provider]) {
      const selectedModel = models[provider].find(m => m.model === model);
      if (selectedModel) {
        setSupportedReasons(selectedModel.supported_reasons);
        // Ensure current reasoning effort is supported
        if (!selectedModel.supported_reasons.includes(reasoningEffort)) {
          setReasoningEffort(selectedModel.default_reason);
        }
      }
    }
  }, [model, provider, models, reasoningEffort]);

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
    try {
      const r = await fetch('/api/assistant/chat', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          session,
          provider,
          model,
          reasoning_effort: reasoningEffort,
          // Where the user is: the server tells the model, with the infrastructure.
          path: location.pathname,
          lang: currentLang(),
          ...pageContext,
          messages: forModel(next)
        }),
      });
      const d = await r.json();
      const reply = r.status === 403
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

  // The person's decision on a change. Its outcome joins the conversation as
  // a note, so the model knows on the next turn what was done.
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

  return (
    <div className="flex h-full min-h-0 flex-col">
      <TokenBudgetBanner sessionId={session} />
      {/* The model takes a line of its own: its names are long. */}
      <div className="flex flex-col gap-2 border-b border-line px-4 py-3">
        <Select aria-label={text('assistant.model')} value={model} onChange={(e) => setModel(e.target.value)}>
          {models[provider]?.map((m) => <option key={m.model} value={m.model}>{m.name || m.model}</option>)}
        </Select>
        <div className={`grid gap-2 ${providers.length > 1 ? 'grid-cols-2' : ''}`}>
          {providers.length > 1 ? (
            <Select aria-label={text('assistant.provider')} value={provider} onChange={(e) => setProvider(e.target.value)}>
              {providers.map((p) => <option key={p} value={p}>{providerName(p)}</option>)}
            </Select>
          ) : null}
          <Select aria-label={text('assistant.reasoning')} value={reasoningEffort} onChange={(e) => setReasoningEffort(e.target.value)}>
            {supportedReasons.map((r) => <option key={r} value={r}>{`${text('assistant.reasoning')} · ${reasonLabel(r)}`}</option>)}
          </Select>
        </div>
      </div>
      <ul className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-4 py-4" aria-live="polite">
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
              {m.tools?.length ? (
                <span className="text-[13px] text-muted">{text('assistant.toolsUsed', { tools: m.tools.map((t) => t.title).join(' · ') })}</span>
              ) : null}
              {m.content ? <span className="whitespace-pre-wrap break-words">{m.content}</span> : null}
              {m.actions?.map((a) => <ActionCard key={a.id} action={a} canAct={canChat} onDecide={(ok) => void decide(a, ok)} />)}
            </li>
          ),
        )}
        {busy ? (
          <li className="flex items-center gap-2 text-muted">
            <Spinner />
            {text('assistant.thinking')}
          </li>
        ) : null}
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
