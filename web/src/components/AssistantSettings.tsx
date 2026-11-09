import { useEffect, useState } from 'react';

type Price = { provider: string; model: string; in_per_m: number; out_per_m: number };

/** BYOK settings: key entry (write-only) and pricing table. */
export function AssistantSettings() {
  const [provider, setProvider] = useState('claude');
  const [key, setKey] = useState('');
  const [prices, setPrices] = useState<Price[]>([]);
  const load = () => fetch('/api/assistant/pricing').then((r) => r.json()).then(setPrices);
  useEffect(() => { void load(); }, []);

  async function saveKey() {
    await fetch(`/api/assistant/keys/${provider}`, { method: 'PUT', body: JSON.stringify({ key }) });
    setKey('');
  }
  async function savePrice(p: Price) {
    await fetch('/api/assistant/pricing', { method: 'PUT', body: JSON.stringify(p) });
    await load();
  }

  return (
    <section>
      <h2>Assistant IA — clés (BYOK)</h2>
      <select value={provider} onChange={(e) => setProvider(e.target.value)}>
        <option>claude</option><option>codex</option>
      </select>
      <input type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} />
      <button onClick={() => void saveKey()}>Enregistrer</button>
      <h3>Tarifs ($ / M tokens)</h3>
      <table>
        <tbody>
          {prices.map((p) => (
            <tr key={p.provider + p.model}>
              <td>{p.provider}/{p.model}</td>
              <td><input type="number" defaultValue={p.in_per_m} onBlur={(e) => void savePrice({ ...p, in_per_m: +e.target.value })} /></td>
              <td><input type="number" defaultValue={p.out_per_m} onBlur={(e) => void savePrice({ ...p, out_per_m: +e.target.value })} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
