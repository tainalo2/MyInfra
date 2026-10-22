package api

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"

	"github.com/coder/websocket"
	"myinfra/internal/deploy"
)

func (a *API) serverTerminal(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	server, err := a.Store.Server(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if server.Status != "linked" || server.Address == "" {
		writeError(w, http.StatusConflict, "Ce serveur n'est pas lié.")
		return
	}
	keyIDs, err := a.Store.ServerKeyIDs(r.Context(), server.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	key := a.privateKey(r.Context(), owner, keyIDs)
	if key == "" {
		writeError(w, http.StatusConflict, "Aucune clé privée pour ouvrir la session SSH.")
		return
	}
	a.serveTerminal(w, r, key, server.Address, "")
}

func (a *API) podTerminal(w http.ResponseWriter, r *http.Request) {
	host, namespace, name, err := a.podTarget(r.Context(), ownerID(r.Context()), r.PathValue("id"), r.PathValue("pod"))
	if err != nil {
		a.writePodErr(w, err)
		return
	}
	command := "k3s kubectl exec -it -n '" + namespace + "' '" + name + "' -- sh"
	a.serveTerminal(w, r, host.PrivateKey, host.Address, command)
}

func (a *API) serveTerminal(w http.ResponseWriter, r *http.Request, key, address, command string) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	shell, err := deploy.OpenShell(key, address, command, 80, 24)
	if err != nil {
		_ = conn.Write(ctx, websocket.MessageText, []byte(err.Error()+"\r\n"))
		_ = conn.Close(websocket.StatusInternalError, "terminal")
		return
	}
	defer shell.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := shell.Stdout.Read(buf)
			if n > 0 {
				if writeErr := conn.Write(ctx, websocket.MessageBinary, append([]byte(nil), buf[:n]...)); writeErr != nil {
					cancel()
					return
				}
			}
			if readErr != nil {
				cancel()
				return
			}
		}
	}()
	for {
		_, data, readErr := conn.Read(ctx)
		if readErr != nil {
			return
		}
		if len(data) == 0 {
			continue
		}
		if data[0] == 1 && len(data) >= 5 {
			shell.Resize(int(binary.BigEndian.Uint16(data[1:3])), int(binary.BigEndian.Uint16(data[3:5])))
			continue
		}
		if data[0] == 0 {
			if _, err := shell.Stdin.Write(data[1:]); err != nil && err != io.EOF {
				return
			}
		}
	}
}
