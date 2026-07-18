package main

import (
	"github.com/spf13/cobra"
)

var lookupCmd = &cobra.Command{
	Use:   "lookup",
	Short: "Lookup remote concrnt resources",
}

func init() {
	rootCmd.AddCommand(lookupCmd)
}
