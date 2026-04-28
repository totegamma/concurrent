package database

import (
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/XSAM/otelsql"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"

	"github.com/totegamma/concrnt-playground/internal/infra/database/models"
)

func NewPostgres(dsn string) (*gorm.DB, error) {

	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             300 * time.Millisecond, // Slow SQL threshold
			LogLevel:                  logger.Warn,            // Log level
			IgnoreRecordNotFoundError: true,                   // Ignore ErrRecordNotFound error for logger
			Colorful:                  true,                   // Enable color
		},
	)

	sqlDB, err := otelsql.Open("pgx", dsn,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			OmitConnResetSession: false,
			OmitConnPrepare:      false,
			OmitConnQuery:        false,
			OmitRows:             false,
			OmitConnectorConnect: false,
		}),
	)
	if err != nil {
		return nil, err
	}

	// DBStatsをメトリクスとして記録（コネクションプール待ちを可視化）
	if _, err = otelsql.RegisterDBStatsMetrics(sqlDB,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
	); err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(0)

	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB}),
		&gorm.Config{
			TranslateError: true,
			Logger:         gormLogger,
		},
	)
	if err != nil {
		return nil, err
	}

	if err = db.Use(tracing.NewPlugin(
		tracing.WithDBSystem("postgresql"),
	)); err != nil {
		slog.Error("failed to enable tracing plugin for postgres", slog.String("error", err.Error()))
	}

	return db, nil
}

func MigratePostgres(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.CommitLog{},
		&models.CommitOwner{},
		&models.Record{},
		&models.RecordKey{},
		&models.Association{},
		&models.Ack{},
		&models.Server{},
		&models.Entity{},
		&models.EntityMeta{},
		&models.Subscription{},
		&models.AbuseReport{},
	)
}
