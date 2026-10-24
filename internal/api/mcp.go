package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// The MCP server lets an AI assistant drive MyInfra. It speaks JSON-RPC over
// Streamable HTTP without sessions: each POST carries its own access token
// and gets its answer as plain JSON, so nothing is held between requests.
//
// A tool does nothing by itself: it calls the API routes, through the same
// router and with the same token, so that roles, replicas, running
// operations and every check of the API hold exactly as for the interface.

// mcpVersions are the protocol revisions understood, newest first.
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

const mcpInstructions = `MyInfra pilote des serveurs, des clusters Kubernetes, des applications et des noms DNS.
Règles :
- Désignez les clusters, machines, applications, paquets et comptes par leur nom : les outils les retrouvent. Si un nom est ambigu ou inconnu, l'outil liste les choix ; demandez à l'utilisateur, ne devinez pas.
- Achats et destructions se font en deux temps. Le premier appel ne fait rien : il rend un récapitulatif et un code de confirmation. Montrez le récapitulatif tel quel à l'utilisateur et n'envoyez le code qu'après son accord explicite, avec exactement les mêmes arguments.
- Les modifications DNS qui touchent un enregistrement existant suivent la même règle.
- La redondance réelle est celle que rapporte l'outil, service par service : n'en promettez pas davantage.
- Quand une demande est impossible, présentez les alternatives proposées par l'outil.
- Pour écrire un paquet, lisez la ressource myinfra://guide/packages et vérifiez le texte avec check_package avant de l'enregistrer.
- Les identifiants (comptes provider, DNS, mots de passe) ne passent jamais par ici : renvoyez l'utilisateur vers l'interface.`

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcNoMethod       = -32601
	rpcInvalidParams  = -32602
)

// mcpServer answers POST /mcp; inner is the router the tools call through.
func (a *API) mcpServer(inner http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "Le serveur MCP répond en POST, sans flux.")
			return
		}
		if foreignBrowser(r) {
			writeError(w, http.StatusForbidden, "Origine refusée.")
			return
		}
		bearer, ok := bearerToken(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="MyInfra"`)
			writeError(w, http.StatusUnauthorized, "Jeton d'accès requis : créez-en un dans Réglages › MCP.")
			return
		}
		token, err := a.Store.UserByAPIToken(r.Context(), hashToken(bearer))
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="MyInfra", error="invalid_token"`)
			writeError(w, http.StatusUnauthorized, "Jeton d'accès invalide ou expiré.")
			return
		}
		ctx := withToken(r.Context(), token)
		owner, err := a.Store.InfrastructureOwner(ctx)
		if err != nil {
			owner = token.UserID
		}
		ctx = withSource(withOwner(withUser(ctx, token.UserID), owner), "mcp")
		r = r.WithContext(ctx)

		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusOK, rpcReply{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{rpcParseError, "Message trop gros ou illisible."}})
			return
		}
		raw = bytes.TrimSpace(raw)
		server := &mcpSession{a: a, inner: inner, outer: r}
		if len(raw) > 0 && raw[0] == '[' {
			var batch []json.RawMessage
			if err := json.Unmarshal(raw, &batch); err != nil || len(batch) == 0 {
				writeJSON(w, http.StatusOK, rpcReply{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{rpcInvalidRequest, "Lot illisible."}})
				return
			}
			var replies []rpcReply
			for _, item := range batch {
				if reply, ok := server.handle(item); ok {
					replies = append(replies, reply)
				}
			}
			if len(replies) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			writeJSON(w, http.StatusOK, replies)
			return
		}
		reply, ok := server.handle(raw)
		if !ok {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, reply)
	}
}

// mcpSession is one POST to /mcp: the person, their token and the router.
type mcpSession struct {
	a     *API
	inner http.Handler
	outer *http.Request
}

// handle answers one JSON-RPC message; a notification gets no answer.
func (s *mcpSession) handle(raw json.RawMessage) (rpcReply, bool) {
	var msg rpcMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return rpcReply{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{rpcParseError, "Message illisible."}}, true
	}
	if len(msg.ID) == 0 || string(msg.ID) == "null" {
		// Notifications (initialized, cancelled…) and answers to requests
		// this server never sends: nothing to say back.
		return rpcReply{}, false
	}
	reply := rpcReply{JSONRPC: "2.0", ID: msg.ID}
	if msg.JSONRPC != "2.0" || msg.Method == "" {
		reply.Error = &rpcError{rpcInvalidRequest, "Requête JSON-RPC 2.0 attendue."}
		return reply, true
	}
	result, rpcErr := s.dispatch(msg.Method, msg.Params)
	if rpcErr != nil {
		reply.Error = rpcErr
	} else {
		reply.Result = result
	}
	return reply, true
}

func (s *mcpSession) dispatch(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var in struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &in)
		version := mcpVersions[0]
		for _, known := range mcpVersions {
			if in.ProtocolVersion == known {
				version = known
			}
		}
		release := s.a.Version
		if release == "" {
			release = "dev"
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"listChanged": false, "subscribe": false},
			},
			"serverInfo":   map[string]any{"name": "myinfra", "title": "MyInfra", "version": release},
			"instructions": mcpInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		role := s.a.effectiveRole(s.outer.Context())
		out := []map[string]any{}
		for _, tool := range s.a.mcpTools() {
			if roleRank(role) < roleRank(tool.Role) {
				continue
			}
			out = append(out, tool.listing())
		}
		return map[string]any{"tools": out}, nil
	case "tools/call":
		var in struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &in); err != nil || in.Name == "" {
			return nil, &rpcError{rpcInvalidParams, "Nom d'outil attendu."}
		}
		tool, ok := s.a.mcpTool(in.Name)
		if !ok {
			return nil, &rpcError{rpcInvalidParams, "Outil inconnu : " + in.Name + "."}
		}
		if roleRank(s.a.effectiveRole(s.outer.Context())) < roleRank(tool.Role) {
			s.journal(tool, in.Arguments, "Droits insuffisants.", false)
			return toolFailure("Droits insuffisants : " + tool.Name + " demande le rôle " + tool.Role + "."), nil
		}
		if len(in.Arguments) == 0 || string(in.Arguments) == "null" {
			in.Arguments = json.RawMessage("{}")
		}
		var args any
		if err := json.Unmarshal(in.Arguments, &args); err != nil {
			return nil, &rpcError{rpcInvalidParams, "Arguments illisibles."}
		}
		if err := checkSchema(tool.Schema, args, ""); err != nil {
			return toolFailure(err.Error()), nil
		}
		return s.run(tool, in.Arguments), nil
	case "resources/list":
		return map[string]any{"resources": mcpResourceList()}, nil
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": mcpResourceTemplates()}, nil
	case "resources/read":
		var in struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(params, &in); err != nil || in.URI == "" {
			return nil, &rpcError{rpcInvalidParams, "URI attendue."}
		}
		contents, err := s.readResource(in.URI)
		if err != nil {
			return nil, &rpcError{rpcInvalidParams, err.Error()}
		}
		return map[string]any{"contents": contents}, nil
	}
	return nil, &rpcError{rpcNoMethod, "Méthode inconnue : " + method + "."}
}

// run calls one tool. A failure is an answer the assistant can read and act
// on, never a protocol error; a panic is caught and told as such.
func (s *mcpSession) run(tool mcpTool, args json.RawMessage) (result map[string]any) {
	ctx := s.outer.Context()
	if roleRank(s.a.effectiveRole(ctx)) < roleRank(tool.Role) {
		s.journal(tool, args, "Droits insuffisants.", false)
		return toolFailure("Droits insuffisants.")
	}
	call := &mcpCall{session: s, tool: tool, ctx: ctx}
	if tool.Write {
		// A change, once started, goes to its end even if the assistant
		// stops waiting: a half-made order would be worse than a late one.
		detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
		defer cancel()
		call.ctx = detached
	}
	defer func() {
		if v := recover(); v != nil {
			slog.Error("outil MCP", "tool", tool.Name, "panic", v)
			s.journal(tool, args, "erreur interne", false)
			result = toolFailure("Erreur interne de MyInfra pendant " + tool.Name + " : rien n'a été confirmé. Consultez l'historique avant de réessayer.")
		}
	}()
	text, err := tool.Run(call, args)
	if err != nil {
		var fail *toolError
		if errors.As(err, &fail) && fail.pending {
			// A recap waiting for agreement: nothing done, nothing logged.
			return toolText(fail.Error(), false)
		}
		s.journal(tool, args, err.Error(), false)
		return toolFailure(err.Error())
	}
	if call.acted {
		s.journal(tool, args, "", true)
	}
	return toolText(text, false)
}

// journal leaves in the history what a token changed through the MCP server.
func (s *mcpSession) journal(tool mcpTool, args json.RawMessage, failure string, ok bool) {
	if !tool.Write {
		return
	}
	ctx := s.outer.Context()
	token, _ := tokenOf(ctx)
	message := "Via MCP (" + token.Name + ") : " + tool.Title
	if subject := argsSubject(args); subject != "" {
		message += " — " + subject
	}
	level := "info"
	kind := "mcp.call"
	if !ok {
		level = "warn"
		message += " — refusé : " + failure
		if strings.Contains(failure, "confirmation") {
			kind = "mcp.refused"
		}
	}
	s.a.note(context.WithoutCancel(ctx), ownerID(ctx), newID(), kind, level, message, "", "")
}

// argsSubject names what a call was about, from the arguments that name
// things, for the history.
func argsSubject(raw json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return ""
	}
	var parts []string
	for _, key := range []string{"name", "article", "package", "application", "cluster", "server", "zone", "domain", "domains", "type", "repository", "copies", "quantity"} {
		value, ok := args[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			if v != "" {
				parts = append(parts, key+"="+v)
			}
		case float64:
			parts = append(parts, fmt.Sprintf("%s=%g", key, v))
		case []any:
			var items []string
			for _, item := range v {
				if s, ok := item.(string); ok {
					items = append(items, s)
				}
			}
			if len(items) > 0 {
				parts = append(parts, key+"="+strings.Join(items, ","))
			}
		}
	}
	return strings.Join(parts, ", ")
}

func toolText(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func toolFailure(message string) map[string]any { return toolText(message, true) }

// toolError is an answer of the API refused, as the assistant should read it.
type toolError struct {
	Message  string
	Problems json.RawMessage
	Status   int
	// pending marks a recap that waits for the person's agreement: it is not
	// a failure and leaves nothing in the history.
	pending bool
}

func (e *toolError) Error() string {
	if len(e.Problems) > 0 && string(e.Problems) != "null" {
		return e.Message + "\nProblèmes : " + string(e.Problems)
	}
	return e.Message
}

func failf(format string, args ...any) error {
	return &toolError{Message: fmt.Sprintf(format, args...)}
}

// mcpCall is one tool call: it reaches the API as the token's person.
type mcpCall struct {
	session *mcpSession
	tool    mcpTool
	ctx     context.Context
	// acted is set once a call changed something, for the history.
	acted bool
}

func (c *mcpCall) api() *API { return c.session.a }

// do sends one request through the router and returns its status and body.
func (c *mcpCall) do(method, path string, body any) (int, []byte) {
	var reader io.Reader = http.NoBody
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return http.StatusBadRequest, []byte(`{"error":"Requête illisible."}`)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(c.ctx, method, path, reader)
	if err != nil {
		return http.StatusBadRequest, []byte(`{"error":"Chemin illisible."}`)
	}
	outer := c.session.outer
	req.Host = outer.Host
	req.RemoteAddr = outer.RemoteAddr
	req.Header.Set("Authorization", outer.Header.Get("Authorization"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "fr")
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	c.session.inner.ServeHTTP(rec, req)
	if method != http.MethodGet && rec.status < 400 {
		c.acted = true
	}
	return rec.status, rec.body.Bytes()
}

// send calls the API and decodes its answer into out; a refusal becomes a
// toolError with the API's own message.
func (c *mcpCall) send(method, path string, body, out any) error {
	status, raw := c.do(method, path, body)
	if status >= 400 {
		var refusal struct {
			Error    string          `json:"error"`
			Problems json.RawMessage `json:"problems"`
		}
		_ = json.Unmarshal(raw, &refusal)
		if refusal.Error == "" {
			refusal.Error = fmt.Sprintf("MyInfra a refusé (%d).", status)
		}
		return &toolError{Message: refusal.Error, Problems: refusal.Problems, Status: status}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return failf("Réponse illisible de %s %s.", method, path)
	}
	return nil
}

func (c *mcpCall) get(path string, out any) error { return c.send(http.MethodGet, path, nil, out) }

// recorder keeps the answer of an inner request.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
	wrote  bool
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.status, r.wrote = status, true
}

func (r *recorder) Write(p []byte) (int, error) {
	r.wrote = true
	if r.body.Len()+len(p) > 4<<20 {
		return 0, errors.New("réponse trop longue")
	}
	return r.body.Write(p)
}

func (r *recorder) Flush() {}

// asJSON renders a result for the assistant.
func asJSON(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

// named is any API row with an id and a name.
type named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// pick finds one row by id or by name, ignoring case; it never guesses
// between several, and says which names exist when none matches.
func pick[T any](what, wanted string, rows []T, key func(T) named) (T, error) {
	var zero T
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return zero, failf("Précisez %s.", what)
	}
	var names []string
	var found []T
	for _, row := range rows {
		k := key(row)
		if k.ID == wanted {
			return row, nil
		}
		names = append(names, k.Name)
		if strings.EqualFold(strings.TrimSpace(k.Name), wanted) {
			found = append(found, row)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		sort.Strings(names)
		if len(names) == 0 {
			return zero, failf("Aucun(e) %s « %s » : il n'y en a aucun(e) pour l'instant.", what, wanted)
		}
		return zero, failf("Aucun(e) %s « %s ». Disponibles : %s.", what, wanted, strings.Join(names, ", "))
	}
	return zero, failf("Plusieurs %s s'appellent « %s » : précisez par l'identifiant.", what, wanted)
}

func pickNamed(what, wanted string, rows []named) (named, error) {
	return pick(what, wanted, rows, func(n named) named { return n })
}

func (c *mcpCall) cluster(name string) (named, error) {
	var rows []struct {
		named
		Origin string `json:"origin"`
	}
	if err := c.get("/api/clusters", &rows); err != nil {
		return named{}, err
	}
	list := make([]named, 0, len(rows))
	for _, row := range rows {
		list = append(list, row.named)
	}
	return pickNamed("cluster", name, list)
}

func (c *mcpCall) server(name string) (serverView, error) {
	var rows []serverView
	if err := c.get("/api/servers", &rows); err != nil {
		return serverView{}, err
	}
	return pick("machine", name, rows, func(s serverView) named { return named{s.ID, s.Name} })
}

func (c *mcpCall) application(name string) (named, error) {
	var rows []named
	if err := c.get("/api/applications", &rows); err != nil {
		return named{}, err
	}
	return pickNamed("application", name, rows)
}

func (c *mcpCall) savedPackage(name string) (named, error) {
	var rows []named
	if err := c.get("/api/packages", &rows); err != nil {
		return named{}, err
	}
	return pickNamed("paquet", name, rows)
}

// secretsOf lists the saved accounts and keys of one kind.
func (c *mcpCall) secretsOf(kind string) ([]secretView, error) {
	var rows []secretView
	if err := c.get("/api/secrets", &rows); err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, row := range rows {
		if row.Kind == kind {
			out = append(out, row)
		}
	}
	return out, nil
}

// decodeArgs reads the arguments of a tool into its own struct.
func decodeArgs[T any](raw json.RawMessage) (T, error) {
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, failf("Arguments illisibles : %v", err)
	}
	return out, nil
}
