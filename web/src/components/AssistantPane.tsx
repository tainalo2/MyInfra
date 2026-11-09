import { useEffect, useState } from 'react';
import { TokenBudgetBanner } from './TokenBudgetBanner';

type Msg = { role: 'user' | 'assistant'; content: string };

/** Assistant chat scoped to a functional context. The whole pane hides behind one flag (/api/assistant/status). */
export function AssistantPane({ context = 'template_edit' }: { context?: string }) {
  const [enabled, setEnabled] = useState(false);
  const [providers, setProviders] = useState<string[]>([]);
  const [provider, setProvider] = useState('');
  const [model, setModel] = useState('');
  const [session] = useState(() => crypto.randomUUID());
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    fetch('/api/assistant/status')
      .then((r) => r.json())
      .then((s: { enabled: boolean; providers: string[] }) => {
        setEnabled(s.enabled);
        setProviders(s.providers);
        setProvider(s.providers[0] ?? '');
      })
      .catch(() => setEnabled(false));
  }, []);

  if (!enabled) return null;

  async function send() {
    if (!input.trim() || busy || !model) return;
    const next = [...msgs, { role: 'user' as const, content: input }];
    setMsgs(next);
    setInput('');
    setBusy(true);
    try {
      const r = await fetch('/api/assistant/chat', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ session, provider, model, context, messages: next }),
      });
      const d = await r.json();
      const text = d.assistant_unavailable ? 'Assistant indisponible (clé manquante).' : d.text ?? d.error ?? '';
      setMsgs([...next, { role: 'assistant', content: text }]);
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
        <input placeholder="modèle" value={model} onChange={(e) => setModel(e.target.value)} />
      </div>
      <ul className="flex-1 overflow-auto p-2">
        {msgs.map((m, i) => <li key={i}><b>{m.role}</b> {m.content}</li>)}
      </ul>
      <form className="flex gap-2 p-2" onSubmit={(e) => { e.preventDefault(); void send(); }}>
        <input className="flex-1" value={input} onChange={(e) => setInput(e.target.value)} />
        <button disabled={busy}>Envoyer</button>
      </form>
    </aside>
  );
}
