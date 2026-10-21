package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"myinfra/internal/store"
)

func syncToken(master []byte) string {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("myinfra-sync-v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *API) syncAuthorized(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	got, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return false
	}
	want := syncToken(a.Master)
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (a *API) allowSync(w http.ResponseWriter, r *http.Request) bool {
	if devMaster(a.Master) {
		writeError(w, http.StatusForbidden, "La clé de développement ne peut pas synchroniser.")
		return false
	}
	if !a.syncAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "Pair refusé.")
		return false
	}
	return true
}

func (a *API) syncState(w http.ResponseWriter, r *http.Request) {
	if !a.allowSync(w, r) {
		return
	}
	a.syncMu.Lock()
	doc, err := a.Store.SyncExport(r.Context())
	id, idErr := a.Store.InstanceID(r.Context())
	a.syncMu.Unlock()
	if err != nil || idErr != nil {
		writeError(w, http.StatusInternalServerError, "La synchro a échoué.")
		return
	}
	role, _ := a.Store.CopyRole(r.Context())
	writeJSON(w, http.StatusOK, peerState{InstanceID: id, Hash: doc.Hash(), Driving: !a.Replica, Epoch: role.Epoch})
}

func (a *API) syncSnapshot(w http.ResponseWriter, r *http.Request) {
	if !a.allowSync(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	var remote store.SyncDoc
	if err := json.NewDecoder(r.Body).Decode(&remote); err != nil {
		writeError(w, http.StatusBadRequest, "Instantané illisible.")
		return
	}
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	merged, err := a.Store.SyncMerge(r.Context(), remote)
	if err != nil {
		slog.Error("synchro", "err", err)
		writeError(w, http.StatusInternalServerError, "La synchro a échoué.")
		return
	}
	writeJSON(w, http.StatusOK, merged)
}

type syncPeerView struct {
	URL       string     `json:"url"`
	LastOK    *time.Time `json:"lastOk,omitempty"`
	LastError string     `json:"lastError,omitempty"`
}

func (a *API) syncStatus(w http.ResponseWriter, r *http.Request) {
	id, err := a.Store.InstanceID(r.Context())
	if err != nil {
		a.mapErr(w, err)
		return
	}
	peers, err := a.Store.ListPeers(r.Context())
	if err != nil {
		a.mapErr(w, err)
		return
	}
	view := make([]syncPeerView, 0, len(peers))
	for _, peer := range peers {
		view = append(view, syncPeerView{URL: peer.URL, LastOK: peer.LastOK, LastError: peer.LastError})
	}
	role, _ := a.Store.CopyRole(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"instanceId": id,
		"replica":    a.Replica,
		"fixed":      a.RoleFixed,
		"peers":      view,
		"other":      a.otherCopy(r.Context(), role),
	})
}

func (a *API) addSyncPeer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Adresse du pair invalide.")
		return
	}
	if err := a.Store.AddPeer(r.Context(), body.URL); err != nil {
		writeError(w, http.StatusBadRequest, "Adresse du pair invalide.")
		return
	}
	a.syncStatus(w, r)
}

func (a *API) deleteSyncPeer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Adresse du pair invalide.")
		return
	}
	if err := a.Store.DeletePeer(r.Context(), body.URL); err != nil {
		writeError(w, http.StatusBadRequest, "Adresse du pair invalide.")
		return
	}
	a.syncStatus(w, r)
}

func (a *API) Sync(ctx context.Context) {
	if devMaster(a.Master) {
		slog.Warn("synchronisation inactive avec la clé de développement")
		return
	}
	for _, raw := range strings.Split(os.Getenv("MYINFRA_PEERS"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if err := a.Store.AddPeer(ctx, raw); err != nil {
			slog.Warn("pair ignoré")
		}
	}
	a.syncPeers(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.syncPeers(ctx)
		}
	}
}

func (a *API) syncPeers(ctx context.Context) {
	// A replica always syncs with the copy it follows: without it, it would
	// neither get its changes nor see it fail.
	if a.Replica {
		if role, err := a.Store.CopyRole(ctx); err == nil && role.MainURL != "" {
			_ = a.Store.AddPeer(ctx, role.MainURL)
		}
	}
	peers, err := a.Store.ListPeers(ctx)
	if err != nil || len(peers) == 0 {
		return
	}
	a.syncMu.Lock()
	local, err := a.Store.SyncExport(ctx)
	a.syncMu.Unlock()
	if err != nil {
		slog.Error("synchro", "err", err)
		return
	}
	for _, peer := range peers {
		if err := a.exchange(ctx, peer.URL, local); err != nil {
			_ = a.Store.MarkPeer(ctx, peer.URL, err.Error())
			continue
		}
		_ = a.Store.MarkPeer(ctx, peer.URL, "")
	}
}

func (a *API) exchange(ctx context.Context, peer string, local store.SyncDoc) error {
	state, err := a.peerState(ctx, peer)
	if err != nil {
		return err
	}
	id, err := a.Store.InstanceID(ctx)
	if err != nil {
		return err
	}
	if state.InstanceID == id {
		return errString("Cette adresse est cette copie.")
	}
	a.fence(ctx, peer, state)
	if state.Hash == local.Hash() {
		return nil
	}
	merged, err := a.peerSnapshot(ctx, peer, local)
	if err != nil {
		return err
	}
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	_, err = a.Store.SyncMerge(ctx, merged)
	if err != nil {
		return err
	}
	_ = a.Store.AddPeer(ctx, peer)
	return nil
}

type peerState struct {
	InstanceID string `json:"instanceId"`
	Hash       string `json:"hash"`
	// Driving and Epoch tell whether that copy drives the machines, and
	// under which term.
	Driving bool  `json:"driving"`
	Epoch   int64 `json:"epoch"`
}

var syncClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{DialContext: dialPeer},
}

func dialPeer(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("adresse refusée")
	}
	var allowed []net.IP
	for _, item := range ips {
		if store.PeerIPAllowed(item.IP) {
			allowed = append(allowed, item.IP)
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("adresse refusée")
	}
	var dialer net.Dialer
	var last error
	for _, ip := range allowed {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

func (a *API) peerState(ctx context.Context, peer string) (peerState, error) {
	var state peerState
	res, err := a.peerRequest(ctx, http.MethodGet, peer, "/api/sync/state", nil)
	if err != nil {
		return state, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return state, syncHTTPError(res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&state); err != nil {
		return state, errString("Réponse illisible.")
	}
	return state, nil
}

func (a *API) peerSnapshot(ctx context.Context, peer string, local store.SyncDoc) (store.SyncDoc, error) {
	var merged store.SyncDoc
	body, err := json.Marshal(local)
	if err != nil {
		return merged, err
	}
	res, err := a.peerRequest(ctx, http.MethodPost, peer, "/api/sync/snapshot", body)
	if err != nil {
		return merged, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return merged, syncHTTPError(res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&merged); err != nil {
		return merged, errString("Réponse illisible.")
	}
	return merged, nil
}

func (a *API) peerRequest(ctx context.Context, method, peer, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(peer, "/")+path, reader)
	if err != nil {
		return nil, errString("Adresse du pair invalide.")
	}
	req.Header.Set("Authorization", "Bearer "+syncToken(a.Master))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := syncClient.Do(req)
	if err != nil {
		return nil, errString("Injoignable.")
	}
	return res, nil
}

func syncHTTPError(code int) error {
	switch code {
	case http.StatusUnauthorized:
		return errString("Pair refusé.")
	case http.StatusForbidden:
		return errString("Clé refusée.")
	default:
		return errString(fmt.Sprintf("Réponse %d.", code))
	}
}
