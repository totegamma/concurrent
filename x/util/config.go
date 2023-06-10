package util

import (
    "os"
    "log"
    "github.com/go-yaml/yaml"
)

// Config is Concurrent base configuration
type Config struct {
    Server Server `yaml:"server"`
    Concurrent Concurrent `yaml:"concurrent"`
    NodeInfo NodeInfo `yaml:"nodeinfo"`
}

type Server struct {
    Dsn string `yaml:"dsn"`
    RedisAddr string `yaml:"redisAddr"`
}

type Concurrent struct {
    FQDN string `yaml:"fqdn"`
    CCAddr string `yaml:"ccaddr"`
    Pubkey string `yaml:"publickey"`
    Prvkey string `yaml:"privatekey"`
    Admins []string `yaml:"admins"`
}

// NodeInfo is Activitypub NodeInfo
type NodeInfo struct {
    OpenRegistrations bool `yaml:"openRegistrations"`
    Software struct {
        Name string `yaml:"name"`
        Version string `yaml:"version"`
    } `yaml:"software"`
    Metadata struct {
        Name string `yaml:"name"`
        NodeName string `yaml:"nodeName"`
        NodeDescription string `yaml:"nodeDescription"`
        Description string `yaml:"description"`
        Maintainer struct {
            Name string `yaml:"name"`
            Email string `yaml:"email"`
        } `yaml:"maintainer"`
        ThemeColor string `yaml:"themeColor"`
    } `yaml:"metadata"`
}

// Load loads concurrent config from given path
func (c *Config) Load (path string) error {
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


