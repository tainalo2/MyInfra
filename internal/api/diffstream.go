package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// diffEvent is one step of a patch as the stream tells it: a hunk added to
// the staging, a hunk replaced (same id, new content), or the end of the
// patch (name "done", no data).
type diffEvent struct {
	Name string // "" = hunk added (default SSE message), "modified", "done"
	Hunk *diffHunk
}

// diffHub fans the staging's events out to the SSE connections of the
// account that owns it. Nothing is kept: a connection opened later reads
// the staging through GET /api/diff/staging.
type diffHub struct {
	mu   sync.Mutex
	subs map[string]map[chan diffEvent]struct{}
}

func (h *diffHub) subscribe(owner string) (chan diffEvent, func()) {
	ch := make(chan diffEvent, 64)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[string]map[chan diffEvent]struct{}{}
	}
	if h.subs[owner] == nil {
		h.subs[owner] = map[chan diffEvent]struct{}{}
	}
	h.subs[owner][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[owner], ch)
		h.mu.Unlock()
	}
}

// publish never blocks: a slow connection loses the event and recovers it
// from the staging on its next reload.
func (h *diffHub) publish(owner string, evs ...diffEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[owner] {
		for _, ev := range evs {
			select {
			case ch <- ev:
			default:
			}
		}
	}
}

// announce tells the owner's open streams about a batch of hunks, then that
// the patch is over. known holds the ids already in the staging before the
// batch: those come out as "modified".
func (a *API) announce(owner string, added []diffHunk, known map[string]bool) {
	evs := make([]diffEvent, 0, len(added)+1)
	for i := range added {
		ev := diffEvent{Hunk: &added[i]}
		if known[added[i].ID] {
			ev.Name = "modified"
		}
		evs = append(evs, ev)
	}
	evs = append(evs, diffEvent{Name: "done"})
	a.diffs.publish(owner, evs...)
}

// diffStream is GET /api/diff/stream (SSE), consumed by SplitViewDiff.
func (a *API) diffStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Flux non pris en charge.")
		return
	}
	ch, cancel := a.diffs.subscribe(userID(r.Context()))
	defer cancel()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": ouvert\n\n")
	fl.Flush()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case ev := <-ch:
			if ev.Hunk != nil {
				b, _ := json.Marshal(ev.Hunk)
				if ev.Name != "" {
					fmt.Fprintf(w, "event: %s\n", ev.Name)
				}
				fmt.Fprintf(w, "data: %s\n\n", b)
			} else {
				fmt.Fprintf(w, "event: %s\ndata: {}\n\n", ev.Name)
			}
			fl.Flush()
		}
	}
}
