package domain

type Config struct {
	FQDN         string         `yaml:"fqdn"`
	PrivateKey   string         `yaml:"privatekey"`
	Registration string         `yaml:"registration"` // open, invite, close
	Layer        string         `yaml:"layer"`
	CSID         string         `yaml:"csid"`
	Meta         map[string]any `yaml:"meta,omitempty"`
	Debug        bool           `yaml:"debug,omitempty"`
}
