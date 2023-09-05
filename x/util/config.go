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
	Profile    Profile    `yaml:"profile"`
}

type Server struct {
	Dsn            string `yaml:"dsn"`
	RedisAddr      string `yaml:"redisAddr"`
	EnableTrace    bool   `yaml:"enableTrace"`
	TraceEndpoint  string `yaml:"traceEndpoint"`
	LogPath        string `yaml:"logPath"`
	CaptchaSitekey string `yaml:"captchaSitekey"`
	CaptchaSecret  string `yaml:"captchaSecret"`
}

type Concurrent struct {
	FQDN         string `yaml:"fqdn"`
	CCID         string `yaml:"ccid"`
	Pubkey       string `yaml:"publickey"`
	Prvkey       string `yaml:"privatekey"`
	Registration string `yaml:"registration"` // open, invite, close
}

type Profile struct {
	Nickname        string `yaml:"nickname" json:"nickname"`
	Description     string `yaml:"description" json:"description"`
	Logo            string `yaml:"logo" json:"logo"`
	WordMark        string `yaml:"wordmark" json:"wordmark"`
	ThemeColor      string `yaml:"themeColor" json:"themeColor"`
	Rules           string `yaml:"rules" json:"rules"`
	TosURL          string `yaml:"tosURL" json:"tosURL"`
	MaintainerName  string `yaml:"maintainerName" json:"maintainerName"`
	MaintainerEmail string `yaml:"maintainerEmail" json:"maintainerEmail"`

	// internal generated
	Registration string `yaml:"registration" json:"registration"`
	Version      string `yaml:"version" json:"version"`
	Hash         string `yaml:"hash" json:"hash"`
	SiteKey      string `yaml:"captchaSiteKey" json:"captchaSiteKey"`
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
