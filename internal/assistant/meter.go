package assistant

import "sync"

// Snapshot is what the frontend banner renders.
type Snapshot struct {
	SessionID    string  `json:"session_id"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	PriceKnown   bool    `json:"price_known"`
}

// Meter sums tokens per session and publishes a snapshot after every call.
// Cost is recomputed from the totals with the provider/model of the latest call.
type Meter struct {
	mu      sync.Mutex
	pricing *Pricing
	sess    map[string]*Snapshot
	subs    map[string]map[chan Snapshot]struct{}
}

func NewMeter(p *Pricing) *Meter {
	return &Meter{pricing: p, sess: map[string]*Snapshot{}, subs: map[string]map[chan Snapshot]struct{}{}}
}

// Record adds usage from a provider call. Cost accumulates per call so that a
// provider switch mid-session is priced with the right table.
func (m *Meter) Record(session, provider, model string, u Usage) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sess[session]
	if s == nil {
		s = &Snapshot{SessionID: session, PriceKnown: true}
		m.sess[session] = s
	}
	s.Provider, s.Model = provider, model
	s.InputTokens += u.InputTokens
	s.OutputTokens += u.OutputTokens
	if c, err := m.pricing.Cost(provider, model, u.InputTokens, u.OutputTokens); err == nil {
		s.CostUSD += c
	} else {
		s.PriceKnown = false
	}
	snap := *s
	for ch := range m.subs[session] {
		select {
		case ch <- snap:
		default:
		}
	}
	return snap
}

func (m *Meter) Get(session string) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sess[session]; s != nil {
		return *s
	}
	return Snapshot{SessionID: session, PriceKnown: true}
}

func (m *Meter) Subscribe(session string) (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 16)
	m.mu.Lock()
	if m.subs[session] == nil {
		m.subs[session] = map[chan Snapshot]struct{}{}
	}
	m.subs[session][ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() { m.mu.Lock(); delete(m.subs[session], ch); m.mu.Unlock() }
}
