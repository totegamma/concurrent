package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
)

const lookupFallbackResolver = "ariake.concrnt.net"

type worldProfile struct {
	Username string `json:"username"`
}

var lookupWorldCmd = &cobra.Command{
	Use:   "world <ccid>",
	Short: "Lookup a user's home server and concrnt.world profile username",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ccid := args[0]
		if !concrnt.IsCCID(ccid) {
			return fmt.Errorf("invalid ccid: %s", ccid)
		}

		resolver := lookupFallbackResolver
		var cl *client.Client
		if conf, err := loadConcrntConfig(); err == nil {
			globalConfig := conf.DomainConfig()
			resolver = globalConfig.FQDN
			cl = client.New(resolver)
			if conf.Backends.GatewayAddr != "" {
				cl.AddHostRemapping(globalConfig.FQDN, conf.Backends.GatewayAddr)
			}
		} else {
			cl = client.New(resolver)
		}

		ctx := cmd.Context()

		domain, err := cl.ResolveResourceHost(ctx, "cckv://"+ccid)
		if err != nil {
			return fmt.Errorf("failed to resolve home server via %s: %w", resolver, err)
		}

		fmt.Println("Domain:  ", domain)

		var doc concrnt.Document[worldProfile]
		uri := "cckv://" + ccid + "/concrnt.world/profiles/main"
		err = cl.GetRecord(ctx, uri, &client.Options{Resolver: domain}, &doc)
		if err != nil {
			verifyFailed := errors.Is(err, concrnt.ErrNoneProofNotAllowed) ||
				errors.Is(err, concrnt.ErrSignatureVerificationFailed) ||
				errors.Is(err, concrnt.ErrUnsupportedProofType)
			if !verifyFailed {
				return fmt.Errorf("failed to get profile %s: %w", uri, err)
			}
			// Profiles migrated from v1 carry a "none" proof and cannot be
			// verified; fall back to an unverified fetch so lookups still work
			// for those accounts.
			retryErr := cl.GetRecord(ctx, uri, &client.Options{Resolver: domain, SkipVerify: true}, &doc)
			if retryErr != nil {
				return fmt.Errorf("failed to get profile %s: %w", uri, err)
			}
			fmt.Fprintln(os.Stderr, "warning: profile signature could not be verified:", err)
		}

		if doc.Value.Username == "" {
			fmt.Println("Username: (not set)")
		} else {
			fmt.Println("Username:", doc.Value.Username)
		}

		return nil
	},
}

func init() {
	lookupCmd.AddCommand(lookupWorldCmd)
}
