package main

import (
	"github.com/go-yaml/yaml"
	"github.com/totegamma/concurrent/core"
	"log"
	"os"
)

type Config struct {
	Server  Server           `yaml:"server"`
	Concrnt core.ConfigInput `yaml:"concrnt"`
	Profile Profile          `yaml:"profile"`
}

type Server struct {
	Dsn            string `yaml:"dsn"`
	RedisAddr      string `yaml:"redisAddr"`
	RedisDB        int    `yaml:"redisDB"`
	MemcachedAddr  string `yaml:"memcachedAddr"`
	EnableTrace    bool   `yaml:"enableTrace"`
	TraceEndpoint  string `yaml:"traceEndpoint"`
	RepositoryPath string `yaml:"repositoryPath"`
	CaptchaSitekey string `yaml:"captchaSitekey"`
	CaptchaSecret  string `yaml:"captchaSecret"`
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
	MaintainerName  string `yaml:"maintainerName" json:"maintainerName"`
	MaintainerEmail string `yaml:"maintainerEmail" json:"maintainerEmail"`

	// internal generated
	Registration string    `yaml:"registration" json:"registration"`
	Version      string    `yaml:"version" json:"version"`
	BuildInfo    BuildInfo `yaml:"buildInfo" json:"buildInfo"`
	SiteKey      string    `yaml:"captchaSiteKey" json:"captchaSiteKey"`
}

// Load loads config from given path
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
