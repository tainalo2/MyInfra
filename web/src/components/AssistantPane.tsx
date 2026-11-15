import { useEffect, useState } from 'react';
import { SendHorizontal } from 'lucide-react';
import { TokenBudgetBanner } from './TokenBudgetBanner';
import { useSession } from '../session';
import { useAssistant } from '../assistant-context';
import { patchesFromReply } from './diff-blocks';
import { text, type MsgKey } from '../i18n';
import { Spinner } from '../kit';
import { useI18n } from '../lang';
import { Button, Icon, Input, Select } from '../ui';

type Msg = { role: 'user' | 'assistant'; content: string };
type ReasoningEffort = 'low' | 'medium' | 'high';

interface ModelCapability {
  model: string;
  supported_reasons: ReasoningEffort[];
  default_reason: ReasoningEffort;
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
  const { canChat } = useSession();
  const { pageContext } = useAssistant();
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
      .catch(() => setEnabled(false));
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

  if (!enabled) return null;

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
          ...pageContext,
          messages: next
        }),
      });
      const d = await r.json();
      const reply = r.status === 403
        ? text('assistant.forbidden')
        : d.assistant_unavailable ? text('assistant.unavailable') : d.text ?? d.error ?? '';
      setMsgs([...next, { role: 'assistant', content: reply }]);
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

  return (
    <aside aria-label={text('nav.assistant')} className="flex h-full min-h-0 flex-col">
      <TokenBudgetBanner sessionId={session} />
      <div className="grid grid-cols-3 gap-2 border-b border-line px-4 py-3">
        <Select aria-label={text('assistant.provider')} value={provider} onChange={(e) => setProvider(e.target.value)}>
          {providers.map((p) => <option key={p} value={p}>{providerName(p)}</option>)}
        </Select>
        <Select aria-label={text('assistant.model')} value={model} onChange={(e) => setModel(e.target.value)}>
          {models[provider]?.map((m) => <option key={m.model} value={m.model}>{m.model}</option>)}
        </Select>
        <Select aria-label={text('assistant.reasoning')} value={reasoningEffort} onChange={(e) => setReasoningEffort(e.target.value as ReasoningEffort)}>
          {supportedReasons.map((r) => <option key={r} value={r}>{text(`assistant.reasoning.${r}` as MsgKey)}</option>)}
        </Select>
      </div>
      <ul className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-4 py-4" aria-live="polite">
        {msgs.length === 0 ? <li className="text-muted">{text('assistant.empty')}</li> : null}
        {msgs.map((m, i) =>
          m.role === 'user' ? (
            <li key={i} className="flex max-w-[85%] flex-col gap-1 self-end">
              <span className="text-right text-[13px] text-muted">{text('assistant.you')}</span>
              <span className="whitespace-pre-wrap break-words rounded-md bg-status px-3 py-2">{m.content}</span>
            </li>
          ) : (
            <li key={i} className="flex flex-col gap-1">
              <span className="text-[13px] text-muted">{text('nav.assistant')}</span>
              <span className="whitespace-pre-wrap break-words">{m.content}</span>
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
    </aside>
  );
}

function providerName(id: string) {
  return id === 'claude' ? 'Claude' : id === 'codex' ? 'Codex' : id;
}
