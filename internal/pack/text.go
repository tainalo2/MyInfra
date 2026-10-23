package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxSize is the largest package text MyInfra reads.
const MaxSize = 1 << 20

var (
	yamlLine  = regexp.MustCompile(`line (\d+)`)
	pathStep  = regexp.MustCompile(`([^.\[\]]+)|\[(\d+)\]`)
	jsonField = regexp.MustCompile(`unknown field "([^"]+)"`)
)

// Parse reads a package written in YAML or JSON (JSON is YAML). It returns
// the package as far as it could be read, and what is wrong in it, each
// problem at its line.
func Parse(data []byte) (Package, []Problem) {
	p, problems, _ := Read(data)
	return p, problems
}

// Read is Parse that also tells whether the whole text was read: the
// problems are then only those Check finds, and the package holds it all.
func Read(data []byte) (Package, []Problem, bool) {
	var p Package
	if len(data) > MaxSize {
		return p, []Problem{{Message: "Un paquet fait au plus 1 Mio."}}, false
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return p, []Problem{{Line: lineOf(err), Message: "Le texte est illisible : " + plainYAMLError(err)}}, false
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return p, []Problem{{Line: 1, Message: "Un paquet est une suite de champs, en commençant par type: " + Type + "."}}, false
	}
	var generic any
	if err := root.Content[0].Decode(&generic); err != nil {
		return p, []Problem{{Line: lineOf(err), Message: "Le texte est illisible : " + plainYAMLError(err)}}, false
	}
	raw, err := json.Marshal(generic)
	if err != nil {
		return p, []Problem{{Line: 1, Message: "Les noms des champs sont du texte."}}, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, []Problem{decodeProblem(err, root.Content[0])}, false
	}
	problems := Check(p)
	for i := range problems {
		problems[i].Line = Locate(root.Content[0], problems[i].Path)
	}
	return p, problems, true
}

// decodeProblem turns a JSON decoding error into a problem a person reads.
func decodeProblem(err error, doc *yaml.Node) Problem {
	var typed *json.UnmarshalTypeError
	if errors.As(err, &typed) {
		path := fieldPath(typed.Field)
		want := map[string]string{"int": "un nombre", "string": "du texte", "bool": "true ou false"}[typed.Type.Kind().String()]
		if want == "" {
			want = "une autre forme"
		}
		line := Locate(doc, path)
		if !strings.Contains(path, "[") {
			line = findKey(doc, lastStep(path))
		}
		return Problem{Path: path, Line: line, Message: fmt.Sprintf("Ce champ attend %s.", want)}
	}
	if m := jsonField.FindStringSubmatch(err.Error()); m != nil {
		return Problem{Path: m[1], Line: findKey(doc, m[1]), Message: fmt.Sprintf("Le champ %s n'existe pas dans un paquet.", m[1])}
	}
	return Problem{Line: 1, Message: "Le paquet est illisible."}
}

// fieldPath writes "services.1.port" as "services[1].port". Older Go leaves
// the indexes out: the path stays as it came.
func fieldPath(field string) string {
	var b strings.Builder
	for i, step := range strings.Split(field, ".") {
		if _, err := strconv.Atoi(step); err == nil {
			b.WriteString("[" + step + "]")
			continue
		}
		if i > 0 {
			b.WriteString(".")
		}
		b.WriteString(step)
	}
	return b.String()
}

func lastStep(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

func lineOf(err error) int {
	if m := yamlLine.FindStringSubmatch(err.Error()); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 1
}

func plainYAMLError(err error) string {
	return strings.TrimPrefix(strings.TrimPrefix(err.Error(), "yaml: "), "unmarshal errors:\n  ")
}

// Locate finds the line of a path such as "services[1].env.KEY" in a
// mapping; the line of the nearest part found when the rest is missing.
func Locate(doc *yaml.Node, path string) int {
	node := doc
	line := doc.Line
	for _, step := range pathStep.FindAllStringSubmatch(path, -1) {
		var next *yaml.Node
		switch {
		case step[1] != "" && node.Kind == yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				if node.Content[i].Value == step[1] {
					next = node.Content[i+1]
					line = node.Content[i].Line
				}
			}
		case step[2] != "" && node.Kind == yaml.SequenceNode:
			n, _ := strconv.Atoi(step[2])
			if n < len(node.Content) {
				next = node.Content[n]
				line = next.Line
			}
		}
		if next == nil {
			return line
		}
		node = next
	}
	return line
}

// findKey is the line of the first key of that name, anywhere.
func findKey(node *yaml.Node, key string) int {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				return node.Content[i].Line
			}
		}
	}
	for _, child := range node.Content {
		if line := findKey(child, key); line > 0 {
			return line
		}
	}
	return 0
}

// YAML writes a package the way a person would: fields in their order, text
// on several lines as a block.
func YAML(p Package) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	restyle(&root)
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// JSON writes a package as indented JSON.
func JSON(p Package) ([]byte, error) {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// restyle turns the flow style JSON came with into block style.
func restyle(node *yaml.Node) {
	switch node.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		node.Style = 0
	case yaml.ScalarNode:
		node.Style = 0
		if node.Tag == "!!str" && strings.Contains(node.Value, "\n") {
			node.Style = yaml.LiteralStyle
		}
	}
	for _, child := range node.Content {
		restyle(child)
	}
}
