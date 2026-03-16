package acosmi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSConfig WebSocket 长连接配置
type WSConfig struct {
	// OnEvent 收到服务端事件回调
	OnEvent func(WSEvent)
	// OnConnect 连接建立回调
	OnConnect func()
	// OnDisconnect 断线回调
	OnDisconnect func(error)
	// Topics 自动订阅的主题
	Topics []string
	// ReconnectMin 最小重连间隔 (默认 2s)
	ReconnectMin time.Duration
	// ReconnectMax 最大重连间隔 (默认 60s)
	ReconnectMax time.Duration
	// AutoReconnect 是否自动重连 (默认 true)
	AutoReconnect *bool
}

// WSEvent 服务端推送事件
type WSEvent struct {
	Type      string          `json:"type"`
	Topic     string          `json:"topic,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	ConnID    string          `json:"connId,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Message   string          `json:"message,omitempty"`
}

type wsState struct {
	conn      *websocket.Conn
	cfg       WSConfig
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	mu        sync.Mutex
	connected bool
}

// Connect 建立 WebSocket 长连接
// 阻塞直到首次连接成功或 ctx 取消
func (c *Client) Connect(ctx context.Context, cfg WSConfig) error {
	// 默认值
	if cfg.ReconnectMin == 0 {
		cfg.ReconnectMin = 2 * time.Second
	}
	if cfg.ReconnectMax == 0 {
		cfg.ReconnectMax = 60 * time.Second
	}
	if cfg.AutoReconnect == nil {
		t := true
		cfg.AutoReconnect = &t
	}

	wsCtx, wsCancel := context.WithCancel(ctx)

	ws := &wsState{
		cfg:    cfg,
		ctx:    wsCtx,
		cancel: wsCancel,
		done:   make(chan struct{}),
	}

	// 首次连接
	if err := c.wsConnect(ws); err != nil {
		wsCancel()
		return fmt.Errorf("websocket connect: %w", err)
	}

	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()

	// 后台读循环 + 自动重连
	go c.wsLoop(ws)

	return nil
}

// Disconnect 优雅断开 WebSocket 连接
func (c *Client) Disconnect() error {
	c.mu.Lock()
	ws := c.ws
	c.ws = nil
	c.mu.Unlock()

	if ws == nil {
		return nil
	}

	ws.cancel()

	ws.mu.Lock()
	conn := ws.conn
	ws.connected = false
	ws.mu.Unlock()

	if conn != nil {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(3*time.Second),
		)
		_ = conn.Close()
	}

	// 等待读循环退出
	select {
	case <-ws.done:
	case <-time.After(5 * time.Second):
	}

	return nil
}

// IsConnected WebSocket 是否已连接
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	if ws == nil {
		return false
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.connected
}

// ---------- 内部实现 ----------

func (c *Client) wsConnect(ws *wsState) error {
	token, err := c.ensureToken(ws.ctx)
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}

	wsURL := c.wsURL()
	header := http.Header{"Authorization": []string{"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.DialContext(ws.ctx, wsURL, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	// 读取 welcome 消息
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read welcome: %w", err)
	}
	conn.SetReadDeadline(time.Time{}) // 清除 deadline

	var welcome WSEvent
	if err := json.Unmarshal(msg, &welcome); err != nil {
		conn.Close()
		return fmt.Errorf("parse welcome: %w", err)
	}

	if welcome.Type != "welcome" {
		conn.Close()
		return fmt.Errorf("unexpected first message: %s", welcome.Type)
	}

	ws.mu.Lock()
	ws.conn = conn
	ws.connected = true
	ws.mu.Unlock()

	// 自动订阅主题
	if len(ws.cfg.Topics) > 0 {
		sub := struct {
			Type   string   `json:"type"`
			Topics []string `json:"topics"`
		}{
			Type:   "subscribe",
			Topics: ws.cfg.Topics,
		}
		data, err := json.Marshal(sub)
		if err != nil {
			conn.Close()
			return fmt.Errorf("marshal subscribe: %w", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			conn.Close()
			return fmt.Errorf("send subscribe: %w", err)
		}
	}

	if ws.cfg.OnConnect != nil {
		ws.cfg.OnConnect()
	}

	log.Printf("[acosmi-sdk] websocket connected, connId=%s", welcome.ConnID)
	return nil
}

func (c *Client) wsLoop(ws *wsState) {
	defer close(ws.done)

	for {
		// 读循环
		c.wsReadLoop(ws)

		// 检查是否该退出
		select {
		case <-ws.ctx.Done():
			return
		default:
		}

		// 关闭旧连接，防止 FD 泄漏
		ws.mu.Lock()
		if ws.conn != nil {
			ws.conn.Close()
			ws.conn = nil
		}
		ws.connected = false
		ws.mu.Unlock()

		if !*ws.cfg.AutoReconnect {
			return
		}

		// 自动重连 (指数退避)
		delay := ws.cfg.ReconnectMin
		for {
			select {
			case <-ws.ctx.Done():
				return
			case <-time.After(delay):
			}

			log.Printf("[acosmi-sdk] websocket reconnecting (delay=%s)...", delay)
			if err := c.wsConnect(ws); err != nil {
				log.Printf("[acosmi-sdk] websocket reconnect failed: %v", err)
				// 指数退避
				delay = delay * 2
				if delay > ws.cfg.ReconnectMax {
					delay = ws.cfg.ReconnectMax
				}
				continue
			}
			// 重连成功
			break
		}
	}
}

func (c *Client) wsReadLoop(ws *wsState) {
	ws.mu.Lock()
	conn := ws.conn
	ws.mu.Unlock()
	if conn == nil {
		return
	}

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ws.cfg.OnDisconnect != nil {
				ws.cfg.OnDisconnect(err)
			}
			return
		}

		var event WSEvent
		if err := json.Unmarshal(msg, &event); err != nil {
			continue
		}

		if ws.cfg.OnEvent != nil {
			ws.cfg.OnEvent(event)
		}
	}
}

func (c *Client) wsURL() string {
	base := c.apiURL("/ws")
	base = strings.Replace(base, "http://", "ws://", 1)
	base = strings.Replace(base, "https://", "wss://", 1)
	return base
}
