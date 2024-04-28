// Package agent runs some scheduled tasks
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"

	"github.com/totegamma/concurrent/x/store"
	"github.com/totegamma/concurrent/x/util"
)

var tracer = otel.Tracer("agent")

type Agent interface {
	Boot()
}

type agent struct {
	rdb    *redis.Client
	store  store.Service
	config util.Config
}

// NewAgent creates a new agent
func NewAgent(rdb *redis.Client, store store.Service, config util.Config) Agent {
	return &agent{
		rdb,
		store,
		config,
	}
}

// Boot starts agent
func (a *agent) Boot() {
	slog.Info("agent start!")
	ticker60 := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker60.C:
				ctx, span := tracer.Start(context.Background(), "Agent.Boot.FlushLog")
				a.FlushLog(ctx)
				span.End()
				break
			}
		}
	}()
}

func (a *agent) FlushLog(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "Agent.FlushLog")
	defer span.End()

	slog.Info("flush log Start")

	allPath := filepath.Join(a.config.Server.RepositoryPath, "/all")

	err := os.MkdirAll(allPath, 0755)
	if err != nil {
		slog.Error("failed to create repository directory:", err)
		panic(err)
	}

	timestamp := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("%s.log", timestamp)

	alllogpath := filepath.Join(allPath, filename)
	storage, err := os.OpenFile(alllogpath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		slog.Error("failed to open repository log file:", err)
		panic(err)
	}
	defer storage.Close()

	// find last log entry
	stats, err := storage.Stat()
	if err != nil {
		slog.Error("failed to stat repository log file:", err)
		panic(err)
	}

	var lastLine string
	var seeker int64 = stats.Size()

	for {
		fmt.Println("Seeker: ", seeker)
		from := seeker - 1024
		to := seeker

		if from < 0 {
			from = 0
		}

		if from == 0 && to == 0 {
			break
		}

		buf := make([]byte, to-from)
		_, err := storage.ReadAt(buf, from)
		if err != nil {
			slog.Error("failed to read repository log file:", err)
			panic(err)
		}

		// remove trailing newline
		if buf[len(buf)-1] == '\n' {
			buf = buf[:len(buf)-1]
		}

		lines := strings.Split(string(buf), "\n")
		fmt.Println("lines: ", len(lines))
		if len(lines) > 1 {
			fmt.Println("Last line: ", lines[len(lines)-1])
			lastLine = lines[len(lines)-1] + lastLine
			break
		}

		lastLine = string(buf) + lastLine

		seeker = from
	}

	split := strings.Split(lastLine, " ")
	lastID := split[0]
	if len(split) < 2 {
		slog.Error("no last log entry found")
		lastID = "0-0"
	}

	entries, err := a.store.Since(ctx, lastID)

	var log string
	for _, entry := range entries {
		log += fmt.Sprintf("%s %s %s\n", entry.ID, entry.Owner, entry.Content)
	}

	storage.WriteString(log)

	// flush to each user log

	userlogPath := filepath.Join(a.config.Server.RepositoryPath, "/user")
	err = os.MkdirAll(userlogPath, 0755)
	if err != nil {
		slog.Error("failed to create repository directory:", err)
		panic(err)
	}

	bucket := make(map[string]string)
	for _, entry := range entries {
		if _, ok := bucket[entry.Owner]; !ok {
			bucket[entry.Owner] = ""
		}
		bucket[entry.Owner] += fmt.Sprintf("%s %s %s\n", entry.ID, entry.Owner, entry.Content)
	}

	for owner, log := range bucket {
		filename := fmt.Sprintf("%s.log", owner)
		logpath := filepath.Join(userlogPath, filename)
		userstore, err := os.OpenFile(logpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			slog.Error("failed to open repository log file:", err)
			continue
		}
		defer userstore.Close()

		userstore.WriteString(log)
	}

	slog.Info(fmt.Sprintf("%d entries flushed", len(entries)))
}
