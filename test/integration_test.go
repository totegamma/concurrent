package test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent"
	"github.com/totegamma/concurrent/client/mock"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/socket/mock"
	"github.com/totegamma/concurrent/x/store"
	"github.com/totegamma/concurrent/x/util"
)

var (
	db  *gorm.DB
	rdb *redis.Client
	mc  *memcache.Client
)

const (
	RootKey  = "con1mu9xruulec4y6hd0d369sdf325l94z4770m33d"
	RootPriv = "3fcfac6c211b743975de2d7b3f622c12694b8125daf4013562c5a1aefa3253a5"
)

func TestMain(m *testing.M) {

	var cleanup_db func()
	db, cleanup_db = testutil.CreateDB()
	defer cleanup_db()

	var cleanup_rdb func()
	rdb, cleanup_rdb = testutil.CreateRDB()
	defer cleanup_rdb()

	var cleanup_mc func()
	mc, cleanup_mc = testutil.CreateMC()
	defer cleanup_mc()

	m.Run()
}

func TestCreateEntity(t *testing.T) {

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mock_client.NewMockClient(ctrl)
	mockManager := mock_socket.NewMockManager(ctrl)

	config := util.Config{
		Concurrent: util.Concurrent{
			FQDN:         "example.com",
			Registration: "open",
		},
	}

	storeService := concurrent.SetupStoreService(db, rdb, mc, mockClient, mockManager, config)
	storeHandler := store.NewHandler(storeService)

	authService := concurrent.SetupAuthService(db, rdb, mc, mockClient, config)

	e := echo.New()
	e.Use(authService.IdentifyIdentity)
	e.POST("/commit", storeHandler.Commit)

	affiliationDocument := core.EntityAffiliation{
		DocumentBase: core.DocumentBase[any]{
			Signer:   RootKey,
			Type:     "affiliation",
			SignedAt: time.Now(),
		},
		Domain: "example.com",
	}

	document, _ := json.Marshal(affiliationDocument)
	signatureBytes, _ := util.SignBytes(document, RootPriv)
	signature := hex.EncodeToString(signatureBytes)

	commit := core.Commit{
		Document:  string(document),
		Signature: string(signature),
	}

	commitJSON, _ := json.Marshal(commit)

	fmt.Println(string(commitJSON))

	req := httptest.NewRequest(http.MethodPost, "/commit", strings.NewReader(string(commitJSON)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String())
	assert.Equal(t, http.StatusOK, rec.Code)
}
