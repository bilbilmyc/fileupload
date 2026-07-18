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

	// Wait for connection to register
	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Errorf("ClientCount after connect = %d, want 1", hub.ClientCount())
	}

	// Close client connection
	conn.Close()
	time.Sleep(50 * time.Millisecond)

	// readPump should remove on disconnect
	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()
	if count != 0 {
		t.Errorf("clients after disconnect = %d, want 0", count)
	}
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

	// Broadcast
	hub.Broadcast(WSEvent{Type: "test", Payload: "data"})

	// Read message
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read error = %v", err)
	}

	var event WSEvent
	if err := json.Unmarshal(msg, &event); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
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

			tt.broadcast(hub)

			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read error = %v", err)
			}
			var event WSEvent
			json.Unmarshal(msg, &event)
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

	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 2 {
		t.Errorf("ClientCount = %d, want 2", hub.ClientCount())
	}

	// Broadcast to both
	hub.Broadcast(WSEvent{Type: "multi"})

	for i, conn := range []*websocket.Conn{conn1, conn2} {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("conn%d read error = %v", i+1, err)
		}
		var event WSEvent
		json.Unmarshal(msg, &event)
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

	conn, _, _ := websocket.DefaultDialer.Dial(url, nil)
	time.Sleep(50 * time.Millisecond)

	if n := hub.ClientCount(); n != 1 {
		t.Errorf("after connect count = %d, want 1", n)
	}

	conn.Close()
	time.Sleep(50 * time.Millisecond)

	// readPump cleans up, but we can check via Close()
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
	conn, _, _ := websocket.DefaultDialer.Dial(url, nil)
	time.Sleep(50 * time.Millisecond)

	hub.Close()

	// Should not panic
	hub.Broadcast(WSEvent{Type: "after_close"})
	_ = conn
}
