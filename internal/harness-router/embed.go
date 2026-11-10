package harness

import _ "embed"

//go:embed contexts.yaml
var defaultContexts []byte

// DefaultConfig is the set of functional contexts the executable carries.
func DefaultConfig() *Config {
	cfg, err := ParseConfig(defaultContexts)
	if err != nil {
		// The file is part of the build: a bad one fails at the first test.
		panic("harness: contexts.yaml: " + err.Error())
	}
	return cfg
}
