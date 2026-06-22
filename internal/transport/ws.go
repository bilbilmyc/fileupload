package transport

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ===== WebSocket Hub =====

// WSHub WebSocket 连接管理器，支持广播推送。
type WSHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]bool
}

// wsClient 单个 WebSocket 连接
type wsClient struct {
	conn *websocket.Conn
	hub  *WSHub
	send chan []byte
}

// WSEvent WebSocket 事件消息
type WSEvent struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源（开发环境）
	},
}

// NewWSHub 创建 WebSocket hub
func NewWSHub() *WSHub {
	return &WSHub{
		clients: make(map[*wsClient]bool),
	}
}

// HandleWebSocket 处理 WebSocket 升级请求
func (h *WSHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] 升级失败: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		hub:  h,
		send: make(chan []byte, 256),
	}

	h.mu.Lock()
	h.clients[client] = true
	h.mu.Unlock()

	go client.writePump()
	go client.readPump()
}

// Broadcast 向所有连接的客户端广播事件
func (h *WSHub) Broadcast(event WSEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[ws] 序列化事件失败: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.send <- data:
		default:
			// 发送缓冲区满，跳过该客户端
			log.Printf("[ws] 客户端发送缓冲区满，跳过")
		}
	}
}

// BroadcastUploadProgress 广播上传进度
func (h *WSHub) BroadcastUploadProgress(taskID string, fileName string, progress int, speed string, status string) {
	h.Broadcast(WSEvent{
		Type: "upload_progress",
		Payload: map[string]any{
			"task_id":   taskID,
			"file_name": fileName,
			"progress":  progress,
			"speed":     speed,
			"status":    status,
		},
	})
}

// BroadcastUploadComplete 广播上传完成
func (h *WSHub) BroadcastUploadComplete(taskID string, fileName string, fileID string) {
	h.Broadcast(WSEvent{
		Type: "upload_complete",
		Payload: map[string]any{
			"task_id":   taskID,
			"file_name": fileName,
			"file_id":   fileID,
		},
	})
}

// BroadcastUploadError 广播上传错误
func (h *WSHub) BroadcastUploadError(taskID string, fileName string, errorMsg string) {
	h.Broadcast(WSEvent{
		Type: "upload_error",
		Payload: map[string]any{
			"task_id":   taskID,
			"file_name": fileName,
			"error":     errorMsg,
		},
	})
}

// Close 关闭所有连接
func (h *WSHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		close(client.send)
		client.conn.Close()
	}
	h.clients = make(map[*wsClient]bool)
}

// ClientCount 返回当前连接的客户端数
func (h *WSHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ===== Client read/write pumps =====

// writePump 从 send 通道读取消息并写入 WebSocket 连接
func (c *wsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second) // ping 间隔
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[ws] 写入失败: %v", err)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump 从 WebSocket 连接读取消息（用于处理客户端发来的消息）
func (c *wsClient) readPump() {
	defer func() {
		c.hub.mu.Lock()
		delete(c.hub.clients, c)
		c.hub.mu.Unlock()
		c.conn.Close()
	}()

	c.conn.SetReadLimit(4096)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
