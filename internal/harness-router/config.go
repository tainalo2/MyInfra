package harness

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ContextProfile is the scoped capability profile for a functional context.
type ContextProfile struct {
	Name         string   `yaml:"name" json:"name"`
	SystemPrompt string   `yaml:"system_prompt" json:"system_prompt"`
	AllowedTools []string `yaml:"allowed_tools" json:"allowed_tools"`
	MaxTurns     int      `yaml:"max_turns" json:"max_turns"`
	MaxTokens    int      `yaml:"max_tokens" json:"max_tokens"`
}

// PageToContextMapping maps a page name to its corresponding context profile.
type PageToContextMapping struct {
	Page    string `yaml:"page" json:"page"`
	Context string `yaml:"context" json:"context"`
}

// Config maps functional context name -> profile and page -> context.
// Loaded from YAML/JSON config.
type Config struct {
	Contexts map[string]ContextProfile `yaml:"contexts" json:"contexts"`
	Pages    []PageToContextMapping    `yaml:"pages" json:"pages"`
}

// LoadConfig reads a YAML config file.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Contexts == nil {
		c.Contexts = map[string]ContextProfile{}
	}
	if c.Pages == nil {
		c.Pages = []PageToContextMapping{}
	}
	return &c, nil
}

// ParseConfig parses YAML bytes.
func ParseConfig(data []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.Contexts == nil {
		c.Contexts = map[string]ContextProfile{}
	}
	if c.Pages == nil {
		c.Pages = []PageToContextMapping{}
	}
	return &c, nil
}

// Get returns the profile for a context, or error if unknown.
func (c *Config) Get(name string) (ContextProfile, error) {
	p, ok := c.Contexts[name]
	if !ok {
		return ContextProfile{}, fmt.Errorf("unknown context: %s", name)
	}
	if p.Name == "" {
		p.Name = name
	}
	return p, nil
}

// GetContextForPage returns the context profile name for a given page.
// Falls back to "general" if the page is not found or the mapping is empty.
func (c *Config) GetContextForPage(page string) string {
	for _, mapping := range c.Pages {
		if mapping.Page == page {
			return mapping.Context
		}
	}
	return "general"
}

// AllowedSet returns a set of allowed tools for quick lookup.
func (p ContextProfile) AllowedSet() map[string]bool {
	s := map[string]bool{}
	for _, t := range p.AllowedTools {
		s[t] = true
	}
	return s
}
