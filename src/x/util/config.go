package util

import (
    "os"
    "log"
    "github.com/go-yaml/yaml"
)

// Config is Concurrent base configuration
type Config struct {
    FQDN string `yaml:"fqdn"`
    CCAddr string `yaml:"ccaddr"`
    Pubkey string `yaml:"publickey"`
    Prvkey string `yaml:"privatekey"`
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


