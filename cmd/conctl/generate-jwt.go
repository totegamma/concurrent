package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/concrnt/concrnt/internal/infra/config"
	"github.com/concrnt/concrnt/jwt"
	"github.com/spf13/cobra"
)

var generateJwtCmd = &cobra.Command{
	Use:   "jwt",
	Short: "Generate a Serve-signed JWT",
	Run: func(cmd *cobra.Command, args []string) {

		configPath := os.Getenv("CONCRNT_CONFIG")
		if configPath == "" {
			configPath = "/etc/concrnt/config"
		}

		conf, err := config.Load(configPath)
		if err != nil {
			panic("failed to load config: " + err.Error())
		}

		globalConfig := conf.GlobalConfig()

		claims := jwt.Claims{
			Issuer:         globalConfig.CSID,
			Subject:        "system",
			Audience:       globalConfig.CSID,
			ExpirationTime: strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10), // Token valid for 24 hours
			IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		}

		token, err := jwt.Create(claims, conf.NodeInfo.PrivateKey)
		if err != nil {
			panic("failed to create JWT: " + err.Error())
		}

		fmt.Println(token)
	},
}

func init() {
	generateCmd.AddCommand(generateJwtCmd)
}
