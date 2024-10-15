package main

import (
	"github.com/go-yaml/yaml"
	"log"
	"os"

	"github.com/totegamma/concurrent/core"
)

type GatewayConfig struct {
	Services []Service `json:"services"`
}

type ServiceInfo struct {
	Path string `json:"path"`
}

type Service struct {
	Name          string                  `yaml:"name"`
	Host          string                  `yaml:"host"`
	Port          int                     `yaml:"port"`
	Path          string                  `yaml:"path"`
	PreservePath  bool                    `yaml:"preservePath"`
	InjectCors    bool                    `yaml:"injectCors"`
	RateLimitConf core.RateLimitConfigMap `yaml:"rateLimit"`
}

// Load loads concurrent config from given path
func (c *GatewayConfig) Load(path string) error {
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
