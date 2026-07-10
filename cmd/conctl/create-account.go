package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/infra/repository/postgres"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/concrnt/concrnt/policy"
)

var (
	createAccountAlias   string
	createAccountInfo    string
	createAccountInviter string
)

type nopSignal struct{}

func (nopSignal) Publish(context.Context, string, concrnt.Event) error {
	return nil
}

type nopPolicy struct{}

func (nopPolicy) Eval(context.Context, policy.RequestContext, []concrnt.Policy, string, string) error {
	return nil
}

// nopDelivery discards delivery jobs: account creation via conctl is a
// local, one-shot operation with no federation distribution expected.
type nopDelivery struct{}

func (nopDelivery) Enqueue(context.Context, domain.DeliveryJob) error {
	return nil
}

var createAccountCmd = &cobra.Command{
	Use:   "create-account",
	Short: "Create a new local account and commit its entity document",
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		info, err := normalizeCreateAccountInfo(createAccountInfo)
		if err != nil {
			return err
		}

		alias := normalizeCreateAccountAlias(createAccountAlias)
		inviter, err := normalizeCreateAccountInviter(createAccountInviter)
		if err != nil {
			return err
		}

		identity, err := generateIdentityMaterial()
		if err != nil {
			return err
		}

		req, err := buildCreateAccountRequest(identity, op.GlobalConfig.FQDN, alias, info, inviter, time.Now().UTC())
		if err != nil {
			return err
		}

		residenceRepo := postgres.NewResidenceRepository(op.DB, op.Client, op.GlobalConfig)
		recordRepo := postgres.NewRecordRepository(op.DB)
		recordUC := usecase.NewRecordUsecase(recordRepo, residenceRepo, &op.GlobalConfig, op.Client, nopSignal{}, nopPolicy{}, nopDelivery{}, nil)
		residenceUC := usecase.NewResidenceUsecase(residenceRepo, recordUC, &op.GlobalConfig)

		if err := residenceUC.Register(cmd.Context(), "", req); err != nil {
			return fmt.Errorf("failed to create account: %w", err)
		}

		fmt.Println("domain:\t\t", op.GlobalConfig.FQDN)
		if alias != nil {
			fmt.Println("alias:\t\t", *alias)
		}
		if inviter != nil {
			fmt.Println("inviter:\t", *inviter)
		}
		fmt.Println("ccid:\t\t", identity.CCID)
		fmt.Println("mnemonic:\t", identity.Mnemonic)
		fmt.Println("privatekey:\t", identity.PrivateKey)
		fmt.Println("publickey:\t", identity.PublicKey)

		return nil
	}),
}

func init() {
	operationCmd.AddCommand(createAccountCmd)

	createAccountCmd.Flags().StringVar(&createAccountAlias, "alias", "", "Optional alias to bind to the account")
	createAccountCmd.Flags().StringVar(&createAccountInfo, "info", "null", "JSON value to store in entity meta.info")
	createAccountCmd.Flags().StringVar(&createAccountInviter, "inviter", "", "Optional inviter CCID to store in entity meta")
}
