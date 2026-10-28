package api

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"myinfra/internal/market"
	"myinfra/internal/pack"
)

// mcpTool is one action offered to assistants.
type mcpTool struct {
	Name        string
	Title       string
	Description string
	// Role is the least role that may call it: the highest of the routes it
	// calls, so that a tool never offers what its routes would refuse.
	Role string
	// Write marks a tool that changes something: it is written in the
	// history and runs to its end even if the assistant stops waiting.
	Write bool
	// Destructive marks a tool that destroys or buys: it asks for a
	// confirmation code first.
	Destructive bool
	Schema      map[string]any
	// Routes are the API routes the tool calls, checked by the tests to
	// exist and to ask no more than Role.
	Routes []string
	Run    func(c *mcpCall, args json.RawMessage) (string, error)
}

func (t mcpTool) listing() map[string]any {
	readOnly := !t.Write
	return map[string]any{
		"name":        t.Name,
		"title":       t.Title,
		"description": t.Description,
		"inputSchema": t.Schema,
		"annotations": map[string]any{
			"title":           t.Title,
			"readOnlyHint":    readOnly,
			"destructiveHint": t.Destructive,
			"idempotentHint":  readOnly,
			"openWorldHint":   t.Write,
		},
	}
}

// mcpTools lists every tool, in the order assistants see them.
func (a *API) mcpTools() []mcpTool {
	var out []mcpTool
	out = append(out, mcpReadTools()...)
	out = append(out, mcpRegistryTools()...)
	out = append(out, mcpPackageTools()...)
	out = append(out, mcpAppTools()...)
	out = append(out, mcpInfraTools()...)
	out = append(out, mcpDNSTools()...)
	return out
}

func (a *API) mcpTool(name string) (mcpTool, bool) {
	for _, tool := range a.mcpTools() {
		if tool.Name == name {
			return tool, true
		}
	}
	return mcpTool{}, false
}

// Schema helpers: the subset of JSON Schema the tools need, and that
// checkSchema enforces before any call.

func object(required []string, props map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func text(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func choice(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func whole(description string, min, max int) map[string]any {
	return map[string]any{"type": "integer", "description": description, "minimum": min, "maximum": max}
}

func number(description string) map[string]any {
	return map[string]any{"type": "number", "description": description, "minimum": 0}
}

func flag(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func list(description string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": items}
}

func textMap(description string) map[string]any {
	return map[string]any{"type": "object", "description": description, "additionalProperties": map[string]any{"type": "string"}}
}

var confirmationArg = text("Code de confirmation rendu par le premier appel, après l'accord explicite de l'utilisateur. Absent au premier appel.")

// checkSchema checks a value against the subset of JSON Schema above.
func checkSchema(schema map[string]any, value any, path string) error {
	where := path
	if where == "" {
		where = "arguments"
	}
	switch schema["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s : un objet est attendu", where)
		}
		props, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]string); ok {
			for _, key := range required {
				if _, present := obj[key]; !present {
					return fmt.Errorf("%s : le champ « %s » est requis", where, key)
				}
			}
		}
		keys := make([]string, 0, len(obj))
		for key := range obj {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			sub, known := props[key].(map[string]any)
			if !known {
				switch extra := schema["additionalProperties"].(type) {
				case bool:
					if !extra {
						return fmt.Errorf("%s : champ inconnu « %s »", where, key)
					}
					continue
				case map[string]any:
					sub = extra
				default:
					continue
				}
			}
			if obj[key] == nil {
				continue
			}
			if err := checkSchema(sub, obj[key], strings.TrimPrefix(path+"."+key, ".")); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s : une liste est attendue", where)
		}
		sub, _ := schema["items"].(map[string]any)
		for i, item := range items {
			if err := checkSchema(sub, item, fmt.Sprintf("%s[%d]", where, i)); err != nil {
				return err
			}
		}
	case "string":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s : un texte est attendu", where)
		}
		if values, ok := schema["enum"].([]string); ok {
			for _, v := range values {
				if v == s {
					return nil
				}
			}
			return fmt.Errorf("%s : « %s » n'est pas permis (%s)", where, s, strings.Join(values, ", "))
		}
	case "integer", "number":
		n, ok := value.(float64)
		if !ok {
			return fmt.Errorf("%s : un nombre est attendu", where)
		}
		if schema["type"] == "integer" && n != math.Trunc(n) {
			return fmt.Errorf("%s : un nombre entier est attendu", where)
		}
		if min, ok := schema["minimum"].(int); ok && n < float64(min) {
			return fmt.Errorf("%s : %g est sous le minimum %d", where, n, min)
		}
		if max, ok := schema["maximum"].(int); ok && n > float64(max) {
			return fmt.Errorf("%s : %g dépasse le maximum %d", where, n, max)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s : vrai ou faux est attendu", where)
		}
	}
	return nil
}

// Resources: what an assistant reads before writing.

const guideURI = "myinfra://guide/packages"

func mcpResourceList() []map[string]any {
	return []map[string]any{{
		"uri":         guideURI,
		"name":        "guide-paquets",
		"title":       "Écrire un paquet MyInfra",
		"description": "Le format des paquets, chaque champ, les valeurs remplies par MyInfra et un exemple complet.",
		"mimeType":    "text/markdown",
	}}
}

func mcpResourceTemplates() []map[string]any {
	return []map[string]any{{
		"uriTemplate": "myinfra://catalogue/{id}",
		"name":        "article-catalogue",
		"title":       "Paquet d'un article du catalogue",
		"description": "Le paquet YAML d'une application du catalogue, à lire comme exemple ou à copier.",
		"mimeType":    "application/yaml",
	}}
}

func (s *mcpSession) readResource(uri string) ([]map[string]any, error) {
	if uri == guideURI {
		return []map[string]any{{"uri": uri, "mimeType": "text/markdown", "text": pack.Guide}}, nil
	}
	if id, ok := strings.CutPrefix(uri, "myinfra://catalogue/"); ok {
		p, found := market.PackageOf(id)
		if !found {
			return nil, fmt.Errorf("article inconnu : %s", id)
		}
		yaml, err := pack.YAML(p)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{"uri": uri, "mimeType": "application/yaml", "text": string(yaml)}}, nil
	}
	return nil, fmt.Errorf("ressource inconnue : %s", uri)
}
