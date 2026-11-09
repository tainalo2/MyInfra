import { useEffect, useState } from 'react';

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

  const inT = u?.input_tokens ?? 0;
  const outT = u?.output_tokens ?? 0;
  return (
    <div role="status" data-testid="token-banner" className="flex gap-4 border-b px-3 py-1 text-xs">
      <span>{u?.provider ? `${u.provider} · ${u.model}` : '—'}</span>
      <span>in {inT.toLocaleString()}</span>
      <span>out {outT.toLocaleString()}</span>
      <span>total {(inT + outT).toLocaleString()}</span>
      <span>{u && !u.price_known ? 'coût: tarif inconnu' : `≈ $${(u?.cost_usd ?? 0).toFixed(4)}`}</span>
    </div>
  );
}
