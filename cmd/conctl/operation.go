package main

import (
	"context"
	"fmt"
	"os"

	gcdatastore "cloud.google.com/go/datastore"
	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/infra/config"
	"github.com/concrnt/concrnt/internal/infra/database"
	dsrepo "github.com/concrnt/concrnt/internal/infra/repository/datastore"
	"github.com/concrnt/concrnt/internal/infra/repository/postgres"
	"github.com/concrnt/concrnt/internal/usecase"
)

type operationContext struct {
	Config          config.Config
	GlobalConfig    domain.Config
	Repository      string
	DB              *gorm.DB
	DatastoreClient *gcdatastore.Client
	Client          *client.Client
}

func loadConcrntConfig() (config.Config, error) {
	configPath := os.Getenv("CONCRNT_CONFIG")
	if configPath == "" {
		configPath = "/etc/concrnt/config"
	}

	conf, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, fmt.Errorf("failed to load config: %w", err)
	}

	return conf, nil
}

func newOperationContext() (*operationContext, error) {
	conf, err := loadConcrntConfig()
	if err != nil {
		return nil, err
	}

	globalConfig := conf.DomainConfig()

	cl := client.New(globalConfig.FQDN)
	if conf.Backends.GatewayAddr != "" {
		cl.AddHostRemapping(globalConfig.FQDN, conf.Backends.GatewayAddr)
	}

	repository := conf.Backends.Repository
	if repository == "" {
		repository = "postgres"
	}

	op := &operationContext{
		Config:       conf,
		GlobalConfig: globalConfig,
		Repository:   repository,
		Client:       cl,
	}

	switch repository {
	case "postgres":
		db, err := database.NewPostgres(conf.Backends.PostgresDsn)
		if err != nil {
			return nil, fmt.Errorf("failed to connect database: %w", err)
		}
		op.DB = db
	case "datastore":
		if conf.Backends.DatastoreProjectID == "" {
			return nil, fmt.Errorf("backends.datastoreProjectID is required when backends.repository is datastore")
		}
		ds, err := dsrepo.NewClient(context.Background(), conf.Backends.DatastoreProjectID)
		if err != nil {
			return nil, fmt.Errorf("failed to connect datastore: %w", err)
		}
		op.DatastoreClient = ds
	default:
		return nil, fmt.Errorf("unsupported repository backend: %s", repository)
	}

	return op, nil
}

func (o *operationContext) Close() error {
	if o == nil {
		return nil
	}
	if o.DatastoreClient != nil {
		return o.DatastoreClient.Close()
	}
	if o.DB != nil {
		sqlDB, err := o.DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

func (o *operationContext) NewEntityRepository() (usecase.EntityRepository, error) {
	switch o.Repository {
	case "postgres":
		if o.DB == nil {
			return nil, fmt.Errorf("postgres database is not initialized")
		}
		return postgres.NewEntityRepository(o.DB, o.Client, o.GlobalConfig), nil
	case "datastore":
		if o.DatastoreClient == nil {
			return nil, fmt.Errorf("datastore client is not initialized")
		}
		return dsrepo.NewEntityRepository(o.DatastoreClient, o.Config.Backends.DatastoreNamespace, o.Client, o.GlobalConfig), nil
	default:
		return nil, fmt.Errorf("unsupported repository backend: %s", o.Repository)
	}
}

func withOperationContext(run func(cmd *cobra.Command, args []string, op *operationContext) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		op, err := newOperationContext()
		if err != nil {
			return err
		}
		defer op.Close()

		return run(cmd, args, op)
	}
}

var operationCmd = &cobra.Command{
	Use:     "operation",
	Aliases: []string{"op"},
	Short:   "Execute local server operations",
}

func init() {
	rootCmd.AddCommand(operationCmd)
}
