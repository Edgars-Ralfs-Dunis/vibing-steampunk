package adt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// RWF 13: a function module that dumps in the APC session takes the socket
// down at once, but a pending call kept waiting its full timeout (125 s for
// CallRFC, past Claude Code's 120 s), so the dump read as a hang.
func TestPendingRequestFailsWhenSocketCloses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"id":"welcome","success":true,"data":{"session":"t"}}`))
		_, _, _ = conn.ReadMessage()
		conn.Close()
	}))
	defer srv.Close()

	c := NewBaseWebSocketClient(srv.URL, "001", "u", "p", false)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		resp, err := c.SendRawRequest(context.Background(), "rfc_1", map[string]any{"id": "rfc_1"}, 125*time.Second)
		if err == nil && resp.Success {
			t.Error("a request on a closed socket reported success")
		}
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the pending request was still waiting 5 s after the socket closed")
	}
}
