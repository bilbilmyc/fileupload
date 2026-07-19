package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func waitForWSClientCount(t *testing.T, hub *WSHub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("ClientCount = %d, want %d", hub.ClientCount(), want)
}

func readWSEvent(t *testing.T, conn *websocket.Conn) WSEvent {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline error = %v", err)
	}
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage error = %v", err)
	}
	var event WSEvent
	if err := json.Unmarshal(message, &event); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	return event
}

func TestWSHub_NewHub(t *testing.T) {
	hub := NewWSHub()
	if hub == nil {
		t.Fatal("hub is nil")
	}
	if hub.ClientCount() != 0 {
		t.Errorf("ClientCount = %d, want 0", hub.ClientCount())
	}
}

func TestWSHub_Close(t *testing.T) {
	hub := NewWSHub()
	hub.Close()
	if hub.ClientCount() != 0 {
		t.Errorf("ClientCount after close = %d, want 0", hub.ClientCount())
	}
}

func TestWSHub_ConnectAndDisconnect(t *testing.T) {
	hub := NewWSHub()

	// Start test HTTP server with WebSocket handler
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	// Connect
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial error = %v", err)
	}

	// Wait for connection to register.
	waitForWSClientCount(t, hub, 1)

	// Close client connection
	conn.Close()

	// readPump should remove on disconnect.
	waitForWSClientCount(t, hub, 0)
}

func TestWSHub_Broadcast(t *testing.T) {
	hub := NewWSHub()

	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial error = %v", err)
	}
	defer conn.Close()

	waitForWSClientCount(t, hub, 1)
	hub.Broadcast(WSEvent{Type: "test", Payload: "data"})

	event := readWSEvent(t, conn)
	if event.Type != "test" {
		t.Errorf("type = %s, want test", event.Type)
	}
}

func TestWSHub_BroadcastTypes(t *testing.T) {
	tests := []struct {
		name      string
		broadcast func(hub *WSHub)
		wantType  string
	}{
		{"upload_progress", func(h *WSHub) { h.BroadcastUploadProgress("t1", "f.txt", 50, "1MB/s", "uploading") }, "upload_progress"},
		{"upload_complete", func(h *WSHub) { h.BroadcastUploadComplete("t1", "f.txt", "fid-1") }, "upload_complete"},
		{"upload_error", func(h *WSHub) { h.BroadcastUploadError("t1", "f.txt", "disk full") }, "upload_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := NewWSHub()

			srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
			defer srv.Close()

			url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
			conn, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err != nil {
				t.Fatalf("dial error = %v", err)
			}
			defer conn.Close()

			waitForWSClientCount(t, hub, 1)
			tt.broadcast(hub)

			event := readWSEvent(t, conn)
			if event.Type != tt.wantType {
				t.Errorf("type = %s, want %s", event.Type, tt.wantType)
			}
		})
	}
}

func TestWSHub_BroadcastToMultiple(t *testing.T) {
	hub := NewWSHub()

	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	// Connect two clients
	conn1, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("conn1 dial error = %v", err)
	}
	defer conn1.Close()

	conn2, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("conn2 dial error = %v", err)
	}
	defer conn2.Close()

	waitForWSClientCount(t, hub, 2)

	// Broadcast to both
	hub.Broadcast(WSEvent{Type: "multi"})

	for i, conn := range []*websocket.Conn{conn1, conn2} {
		event := readWSEvent(t, conn)
		if event.Type != "multi" {
			t.Errorf("conn%d type = %s", i+1, event.Type)
		}
	}
}

func TestWSHub_ClientCount(t *testing.T) {
	hub := NewWSHub()

	if n := hub.ClientCount(); n != 0 {
		t.Errorf("initial count = %d, want 0", n)
	}

	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial error = %v", err)
	}
	waitForWSClientCount(t, hub, 1)

	conn.Close()
	waitForWSClientCount(t, hub, 0)
	hub.Close()
	if n := hub.ClientCount(); n != 0 {
		t.Errorf("after close count = %d, want 0", n)
	}
}

// TestWSHub_BroadcastAfterClose verifies no panic on broadcast after close
func TestWSHub_BroadcastAfterClose(t *testing.T) {
	hub := NewWSHub()

	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial error = %v", err)
	}
	waitForWSClientCount(t, hub, 1)

	hub.Close()

	// Should not panic
	hub.Broadcast(WSEvent{Type: "after_close"})
	_ = conn
}
