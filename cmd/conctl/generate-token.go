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

func generateToken(subject string, validFor time.Duration) string {
	configPath := os.Getenv("CONCRNT_CONFIG")
	if configPath == "" {
		configPath = "/etc/concrnt/config"
	}

	conf, err := config.Load(configPath)
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	domainConfig := conf.DomainConfig()

	claims := jwt.Claims{
		Issuer:         domainConfig.CSID,
		Subject:        subject,
		Audience:       domainConfig.CSID,
		ExpirationTime: strconv.FormatInt(time.Now().Add(validFor).Unix(), 10),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
	}

	token, err := jwt.Create(claims, conf.Concrnt.PrivateKey)
	if err != nil {
		panic("failed to create JWT: " + err.Error())
	}

	return token
}

var generateJwtCmd = &cobra.Command{
	Use:   "token",
	Short: "Generate a Serve-signed JWT",
	Run: func(cmd *cobra.Command, args []string) {

		subject, err := cmd.Flags().GetString("subject")
		if err != nil {
			panic("failed to get subject flag: " + err.Error())
		}

		validForStr, _ := cmd.Flags().GetString("valid-for")
		validFor, err := time.ParseDuration(validForStr)
		if err != nil {
			panic("invalid duration format for valid-for: " + err.Error())
		}

		token := generateToken(subject, validFor)
		fmt.Println(token)
	},
}

func init() {
	generateCmd.AddCommand(generateJwtCmd)

	generateJwtCmd.Flags().StringP("subject", "s", "system", "Subject (sub) claim for the JWT")
	generateJwtCmd.Flags().StringP("valid-for", "v", "5m", "Duration for which the JWT is valid (e.g., 24h, 30m)")
}
