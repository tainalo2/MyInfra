package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"myinfra/internal/cost"
	"myinfra/internal/store"
)

type billView struct {
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Theoretical bool   `json:"theoretical"`
}

type eventView struct {
	ID          string    `json:"id"`
	At          time.Time `json:"at"`
	Kind        string    `json:"kind"`
	Level       string    `json:"level"`
	Message     string    `json:"message"`
	ServerID    string    `json:"serverId,omitempty"`
	ServerName  string    `json:"serverName,omitempty"`
	ClusterID   string    `json:"clusterId,omitempty"`
	ClusterName string    `json:"clusterName,omitempty"`
	Source      string    `json:"source"`
	Actor       string    `json:"actor,omitempty"`
}

func (a *API) listEvents(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	if err := a.Store.BackfillEvents(r.Context(), owner); err != nil {
		a.mapErr(w, err)
		return
	}
	query := r.URL.Query()
	rows, err := a.Store.ListEvents(r.Context(), owner, store.EventFilter{
		ServerID: query.Get("server"), ClusterID: query.Get("cluster"), Source: query.Get("source"),
	})
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]eventView, 0, len(rows))
	for _, row := range rows {
		out = append(out, eventView{
			ID: row.ID, At: row.At, Kind: row.Kind, Level: row.Level, Message: row.Message,
			ServerID: row.ServerID, ServerName: row.ServerName, ClusterID: row.ClusterID, ClusterName: row.ClusterName,
			Source: row.Source, Actor: row.Actor,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func billOf(hourly, monthly, currency string, from time.Time) *billView {
	amount, ok := cost.Since(hourly, monthly, from, time.Now())
	if !ok {
		return nil
	}
	return &billView{Amount: amount, Currency: currency, Theoretical: true}
}

func (a *API) note(ctx context.Context, owner, id, kind, level, message, serverID, clusterID string) {
	message = clip(message, 200)
	if message == "" || id == "" {
		return
	}
	_ = a.Store.AddEvent(ctx, store.Event{
		ID: id, OwnerID: owner, Kind: kind, Level: level, Message: message,
		ServerID: serverID, ClusterID: clusterID,
		Source: sourceOf(ctx), Actor: actorOf(ctx),
	})
}

func (a *API) noteServer(ctx context.Context, owner, serverID, kind, level, message string) {
	clusterID := ""
	if row, err := a.Store.Server(ctx, owner, serverID); err == nil {
		clusterID = row.ClusterID
	}
	a.note(ctx, owner, newID(), kind, level, message, serverID, clusterID)
}

func clip(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
