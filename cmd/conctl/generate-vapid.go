package main

import (
	"fmt"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/spf13/cobra"
)

var generateVapidKeysCmd = &cobra.Command{
	Use:   "vapid",
	Short: "Generate a new VAPID key pair",
	Run: func(cmd *cobra.Command, args []string) {
		vapidPrivateKey, vapidPublicKey, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Println("VAPID private key:", vapidPrivateKey)
		fmt.Println("VAPID public key:", vapidPublicKey)
	},
}

func init() {
	generateCmd.AddCommand(generateVapidKeysCmd)
}
