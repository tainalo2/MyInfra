import { useEffect, useState } from 'react';
import { currentLocale, text } from '../i18n';

export interface UsageSnapshot {
  session_id: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  price_known: boolean;
}

/** Live token/cost banner. Provider-agnostic: renders whatever the backend meter reports. */
export function TokenBudgetBanner({ sessionId }: { sessionId: string }) {
  const [u, setU] = useState<UsageSnapshot | null>(null);

  useEffect(() => {
    setU(null);
    const es = new EventSource(`/api/assistant/usage/stream?session=${encodeURIComponent(sessionId)}`);
    es.onmessage = (e) => setU(JSON.parse(e.data) as UsageSnapshot);
    return () => es.close();
  }, [sessionId]);

  // Nothing to say before the first turn.
  if (!u?.provider) return null;
  const n = (v: number) => v.toLocaleString(currentLocale());
  return (
    <div role="status" data-testid="token-banner" className="flex flex-wrap gap-x-3 gap-y-1 border-b border-line px-4 py-2 text-[13px] text-muted">
      <span>{`${u.provider} · ${u.model}`}</span>
      <span>{text('assistant.usage', { in: n(u.input_tokens), out: n(u.output_tokens) })}</span>
      <span>{u.price_known ? text('assistant.cost', { cost: u.cost_usd.toFixed(4) }) : text('assistant.costUnknown')}</span>
    </div>
  );
}
