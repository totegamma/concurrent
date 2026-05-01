package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"

	"github.com/go-yaml/yaml"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
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

func DeepMerge(dst, src any) error {
	dstPtr := reflect.ValueOf(dst)
	srcPtr := reflect.ValueOf(src)

	if dstPtr.Kind() != reflect.Pointer || srcPtr.Kind() != reflect.Pointer {
		return fmt.Errorf("both arguments must be pointers")
	}

	dstElem := dstPtr.Elem()
	srcElem := srcPtr.Elem()
	if dstElem.Kind() != reflect.Struct || srcElem.Kind() != reflect.Struct {
		return fmt.Errorf("both arguments must be pointers to structs")
	}

	dstType := dstElem.Type()
	for i := range dstElem.NumField() {
		dstField := dstElem.Field(i)
		srcField := srcElem.Field(i)
		structField := dstType.Field(i)

		if !dstField.CanSet() {
			continue
		}

		if isZeroValue(srcField) {
			continue
		}

		switch dstField.Kind() {
		case reflect.Struct:
			if err := DeepMerge(dstField.Addr().Interface(), srcField.Addr().Interface()); err != nil {
				return fmt.Errorf("error merging field %s: %w", structField.Name, err)
			}
		default:
			dstField.Set(srcField)
		}
	}

	return nil
}

func isZeroValue(v reflect.Value) bool {
	return reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface())
}

func Load(path string) (Config, error) {

	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}

	var config Config
	if info.IsDir() {
		files, err := os.ReadDir(path)
		if err != nil {
			slog.Error("Failed to read config directory", "path", path, "error", err)
			return Config{}, err
		}

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			entryPath := filepath.Join(path, file.Name())
			slog.Debug("Loading config from file", "path", entryPath)

			f, err := os.Open(entryPath)
			if err != nil {
				slog.Warn("Failed to open config file, skipping", "path", entryPath, "error", err)
				continue
			}
			defer f.Close()

			var tmp Config
			err = yaml.NewDecoder(f).Decode(&tmp)
			if err != nil {
				slog.Warn("Failed to decode config file, skipping", "path", entryPath, "error", err)
				continue
			}

			err = DeepMerge(&config, &tmp)
			if err != nil {
				slog.Warn("Failed to merge config file, skipping", "path", entryPath, "error", err)
				continue
			}
		}
	} else {
		file, err := os.Open(path)
		if err != nil {
			return Config{}, err
		}

		slog.Debug("Loading config from file", "path", path)

		err = yaml.NewDecoder(file).Decode(&config)
		if err != nil {
			return Config{}, err
		}

		return config, nil
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
