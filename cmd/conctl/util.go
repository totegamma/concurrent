package main

import (
	"github.com/spf13/cobra"
)

var utilCmd = &cobra.Command{
	Use:   "util",
	Short: "Local file utilities (no server or database access)",
}

func init() {
	rootCmd.AddCommand(utilCmd)
}
