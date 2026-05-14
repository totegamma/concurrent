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
	e.GET("/realtime", h.handleRealtime)

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

	// Give the handler and SignalService goroutines time to observe the
	// disconnect and subscription acknowledgement. Before the fix, this path
	// could panic with "send on closed channel".
	time.Sleep(200 * time.Millisecond)
}
