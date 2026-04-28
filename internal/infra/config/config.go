package config

import (
	"os"

	"github.com/go-yaml/yaml"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/impl/interop"
	"github.com/totegamma/concrnt-playground/internal/domain"
)

type Config struct {
	NodeInfo NodeInfo          `yaml:"nodeInfo"`
	Server   Server            `yaml:"server"`
	Profile  map[string]any    `yaml:"profile"`
	Services []interop.Service `yaml:"services"`
}

type NodeInfo struct {
	FQDN         string `yaml:"fqdn"`
	PrivateKey   string `yaml:"privatekey"`
	Registration string `yaml:"registration"` // open, invite, close
	SiteKey      string `yaml:"sitekey"`
	Layer        string `yaml:"layer"`
	Dimension    string `yaml:"dimension"` // backward compatibility
	Debug        bool   `yaml:"debug"`
}

type Server struct {
	PostgresDsn     string `yaml:"postgresDsn"`
	GatewayAddr     string `yaml:"gatewayAddr"`
	RedisAddr       string `yaml:"redisAddr"`
	RedisDB         int    `yaml:"redisDB"`
	MemcachedAddr   string `yaml:"memcachedAddr"`
	EnableTrace     bool   `yaml:"enableTrace"`
	TraceEndpoint   string `yaml:"traceEndpoint"`
	RepositoryPath  string `yaml:"repositoryPath"`
	CaptchaSitekey  string `yaml:"captchaSitekey"`
	CaptchaSecret   string `yaml:"captchaSecret"`
	VapidPublicKey  string `yaml:"vapidPublicKey"`
	VapidPrivateKey string `yaml:"vapidPrivateKey"`
}

func Load(path string) (Config, error) {

	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}

	var config Config
	err = yaml.NewDecoder(file).Decode(&config)
	if err != nil {
		return Config{}, err
	}

	return config, nil
}

func (c Config) GlobalConfig() domain.Config {

	csid, err := concrnt.PrivKeyToAddr(c.NodeInfo.PrivateKey, "ccs")
	if err != nil {
		panic(err)
	}

	return domain.Config{
		FQDN:         c.NodeInfo.FQDN,
		PrivateKey:   c.NodeInfo.PrivateKey,
		Registration: c.NodeInfo.Registration,
		SiteKey:      c.NodeInfo.SiteKey,
		Layer:        c.NodeInfo.Layer,
		Dimension:    c.NodeInfo.Dimension,
		CSID:         csid,
		Meta:         c.Profile,
		Debug:        c.NodeInfo.Debug,
	}
}
