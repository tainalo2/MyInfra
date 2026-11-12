import { useEffect, useState } from 'react';
import { TokenBudgetBanner } from './TokenBudgetBanner';
import { useSession } from '../session';
import { useAssistant } from '../assistant-context';
import { patchesFromReply } from './diff-blocks';

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
      const text = r.status === 403
        ? 'Droits insuffisants.'
        : d.assistant_unavailable ? 'Assistant indisponible (clé manquante).' : d.text ?? d.error ?? '';
      setMsgs([...next, { role: 'assistant', content: text }]);
      // A reply carrying ```diff blocks is a patch: it joins the staging,
      // and the split view (stream) shows the hunks as they land.
      const patches = patchesFromReply(text);
      if (patches.length > 0) {
        const staged = await fetch('/api/diff/staging', {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ kind: 'template', name: pageContext?.resource_id || 'unknown', ...pageContext, patches }),
        });
        if (!staged.ok) setMsgs([...next, { role: 'assistant', content: `${text}\n\n(Patch non mis en attente : ${staged.status})` }]);
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <aside aria-label="Assistant IA" className="flex h-full flex-col">
      <TokenBudgetBanner sessionId={session} />
      <div className="flex gap-2 p-2">
        <select value={provider} onChange={(e) => setProvider(e.target.value)}>
          {providers.map((p) => <option key={p}>{p}</option>)}
        </select>
        <select value={model} onChange={(e) => setModel(e.target.value)}>
          {models[provider]?.map((m) => <option key={m.model}>{m.model}</option>)}
        </select>
        <select value={reasoningEffort} onChange={(e) => setReasoningEffort(e.target.value as ReasoningEffort)}>
          {supportedReasons.map((r) => <option key={r}>{r}</option>)}
        </select>
      </div>
      <ul className="flex-1 overflow-auto p-2">
        {msgs.map((m, i) => <li key={i}><b>{m.role}</b> {m.content}</li>)}
      </ul>
      {canChat ? (
        <form className="flex gap-2 p-2" onSubmit={(e) => { e.preventDefault(); void send(); }}>
          <input className="flex-1" value={input} onChange={(e) => setInput(e.target.value)} />
          <button disabled={busy}>Envoyer</button>
        </form>
      ) : (
        <p className="p-2 text-[13px] text-muted">Mode consultation : votre rôle ne permet pas de solliciter l'assistant.</p>
      )}
    </aside>
  );
}
