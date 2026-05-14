package rest

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/service"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestHandleRealtimeDisconnectAfterListenDoesNotPanic(t *testing.T) {
	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	e := echo.New()
	h := &Handler{signal: service.NewSignalService(rdb)}
	handlerDone := make(chan struct{})
	e.GET("/realtime", func(c echo.Context) error {
		defer close(handlerDone)
		return h.handleRealtime(c)
	})

	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/realtime"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	require.NoError(t, conn.WriteJSON(concrnt.RealtimeRequest{
		Type:     "listen",
		Prefixes: []string{"cckv://remote.example/concrnt.world/timeline"},
	}))
	require.NoError(t, conn.Close())

	require.Eventually(t, func() bool {
		select {
		case <-handlerDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		return len(h.signal.GetCurrentSubscriptions()) == 0
	}, time.Second, 10*time.Millisecond)
}
