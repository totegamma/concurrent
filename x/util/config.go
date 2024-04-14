package util

import (
	"encoding/hex"
	"log"
	"os"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/go-yaml/yaml"
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
	MemcachedAddr  string `yaml:"memcachedAddr"`
	EnableTrace    bool   `yaml:"enableTrace"`
	TraceEndpoint  string `yaml:"traceEndpoint"`
	LogPath        string `yaml:"logPath"`
	CaptchaSitekey string `yaml:"captchaSitekey"`
	CaptchaSecret  string `yaml:"captchaSecret"`
}

type Concurrent struct {
	FQDN         string `yaml:"fqdn"`
	PrivateKey   string `yaml:"privatekey"`
	Registration string `yaml:"registration"` // open, invite, close
	Dimension    string `yaml:"dimension"`

	// internal generated
	CCID      string `yaml:"ccid"`
	PublicKey string `yaml:"publickey"`
}

type BuildInfo struct {
	BuildTime    string `yaml:"BuildTime" json:"BuildTime"`
	BuildMachine string `yaml:"BuildMachine" json:"BuildMachine"`
	GoVersion    string `yaml:"GoVersion" json:"GoVersion"`
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
	Registration string    `yaml:"registration" json:"registration"`
	Version      string    `yaml:"version" json:"version"`
	BuildInfo    BuildInfo `yaml:"buildInfo" json:"buildInfo"`
	SiteKey      string    `yaml:"captchaSiteKey" json:"captchaSiteKey"`
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

	privKeyBytes, err := hex.DecodeString(c.Concurrent.PrivateKey)
	if err != nil {
		log.Fatal("failed to decode private key:", err)
		return err
	}

	privKey := secp256k1.PrivKey{
		Key: privKeyBytes,
	}

	pubkey := privKey.PubKey()

	addr, err := PubkeyBytesToAddr(pubkey.Bytes(), "ccd")
	if err != nil {
		log.Fatal("failed to convert pubkey to address:", err)
		return err
	}

	c.Concurrent.CCID = addr

	return nil
}
