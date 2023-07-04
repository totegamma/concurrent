package util

import (
	"github.com/go-yaml/yaml"
	"log"
	"os"
)

// Config is Concurrent base configuration
type Config struct {
	Server     Server     `yaml:"server"`
	Concurrent Concurrent `yaml:"concurrent"`
	NodeInfo   NodeInfo   `yaml:"nodeinfo"`
	Profile    Profile    `yaml:"profile"`
}

type Server struct {
	Dsn           string `yaml:"dsn"`
	RedisAddr     string `yaml:"redisAddr"`
	EnableTrace   bool   `yaml:"enableTrace"`
	TraceEndpoint string `yaml:"traceEndpoint"`
	LogPath       string `yaml:"logPath"`
}

type Concurrent struct {
	FQDN   string   `yaml:"fqdn"`
	CCAddr string   `yaml:"ccaddr"`
	Pubkey string   `yaml:"publickey"`
	Prvkey string   `yaml:"privatekey"`
	Admins []string `yaml:"admins"`
}

type Profile struct {
	Nickname    string `yaml:"nickname" json:"nickname"`
	Description string `yaml:"description" json:"description"`
	Logo        string `yaml:"logo" json:"logo"`
	WordMark    string `yaml:"wordmark" json:"wordmark"`
	Rules       string `yaml:"rules" json:"rules"`
	TosURL      string `yaml:"tosURL" json:"tosURL"`
	Version     string `yaml:"version" json:"version"`
	Hash        string `yaml:"hash" json:"hash"`
}

// NodeInfo is Activitypub NodeInfo
type NodeInfo struct {
	OpenRegistrations bool `yaml:"openRegistrations"`
	Metadata          struct {
		NodeName        string `yaml:"nodeName"`
		NodeDescription string `yaml:"nodeDescription"`
		Maintainer      struct {
			Name  string `yaml:"name"`
			Email string `yaml:"email"`
		} `yaml:"maintainer"`
		ThemeColor string `yaml:"themeColor"`
	} `yaml:"metadata"`
}

// Load loads concurrent config from given path
func (c *Config) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal("failed to open configuration file:", err)
		return err
	}
	defer f.Close()

	err = yaml.NewDecoder(f).Decode(&c)
	if err != nil {
		log.Fatal("failed to load configuration file:", err)
		return err
	}

	return nil
}
