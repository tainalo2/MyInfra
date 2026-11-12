import { useEffect, useState } from 'react';

type Price = { provider: string; model: string; in_per_m: number; out_per_m: number };
type ProviderCapability = { provider: string; supports_subscription_auth: boolean };

/** BYOK settings: key entry (write-only) and pricing table. */
export function AssistantSettings() {
  const [provider, setProvider] = useState('claude');
  const [key, setKey] = useState('');
  const [keyType, setKeyType] = useState<'api_key' | 'subscription_token'>('api_key');
  const [prices, setPrices] = useState<Price[]>([]);
  const [providers, setProviders] = useState<ProviderCapability[]>([]);
  
  const load = () => fetch('/api/assistant/pricing').then((r) => r.json()).then(setPrices);
  
  const loadProviders = () => fetch('/api/assistant/providers').then((r) => r.json()).then(setProviders);
  
  useEffect(() => { 
    void load(); 
    void loadProviders();
  }, []);

  async function saveKey() {
    await fetch(`/api/assistant/keys/${provider}`, { 
      method: 'PUT', 
      body: JSON.stringify({ key, type: keyType }) 
    });
    setKey('');
  }
  
  async function savePrice(p: Price) {
    await fetch('/api/assistant/pricing', { method: 'PUT', body: JSON.stringify(p) });
    await load();
  }

  const currentProvider = providers.find(p => p.provider === provider);
  const supportsSubscription = currentProvider?.supports_subscription_auth ?? false;

  return (
    <section>
      <h2>Assistant IA — clés (BYOK)</h2>
      <select value={provider} onChange={(e) => { setProvider(e.target.value); setKeyType('api_key'); }}>
        <option>claude</option><option>codex</option>
      </select>
      
      <div>
        <label>
          <input 
            type="radio" 
            name="keyType" 
            value="api_key" 
            checked={keyType === 'api_key'} 
            onChange={() => setKeyType('api_key')} 
          />
          Clé API
        </label>
        {supportsSubscription && (
          <label>
            <input 
              type="radio" 
              name="keyType" 
              value="subscription_token" 
              checked={keyType === 'subscription_token'} 
              onChange={() => setKeyType('subscription_token')} 
            />
            Token d'abonnement
          </label>
        )}
      </div>
      
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
