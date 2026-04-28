package main

import (
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use: "migrate",
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
