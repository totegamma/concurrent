package main

import (
	"github.com/concrnt/concrnt/core"
)

type Config struct {
	Server  Server           `yaml:"server"`
	Concrnt core.ConfigInput `yaml:"concrnt"`
	Profile Profile          `yaml:"profile"`
}

type Server struct {
	Dsn             string `yaml:"dsn"`
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
	VapidKey     string    `yaml:"vapidKey" json:"vapidKey"`
}
