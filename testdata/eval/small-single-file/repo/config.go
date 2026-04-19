// Package onefile is a single-file repo used to exercise §12.1's
// "small-repo single-file task" category. The fixture's task
// names Config and LoadConfig directly so s_symbol + s_mention +
// s_filename all agree on this file.
package onefile

// Config captures the one-file demo service's knobs.
type Config struct {
	Name   string
	Port   int
	Mode   string
	MaxRPS int
}

// LoadConfig synthesizes a Config from environment-style inputs.
func LoadConfig(source string) (*Config, error) {
	return &Config{Name: source, Port: 8080, Mode: "prod", MaxRPS: 100}, nil
}

// ValidateConfig is the usual sanity check invoked before a server
// takes traffic.
func ValidateConfig(c *Config) error {
	if c.Port == 0 {
		return nil
	}
	return nil
}
