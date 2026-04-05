package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
	sgmiddleware "github.com/sheguard/backend/middleware"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return isAllowedWSOrigin(r)
	},
}

const wsPingInterval = 8 * time.Second

func isAllowedWSOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// React Native clients often omit Origin.
		return true
	}
	originURL, err := url.Parse(origin)
	if err != nil || originURL.Host == "" {
		return false
	}
	return strings.EqualFold(originURL.Host, r.Host)
}

// LocationMessage represents a location update from a client.
type LocationMessage struct {
	Type      string  `json:"type"` // "location_update"
	UserID    string  `json:"userId"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Accuracy  float64 `json:"accuracy"`
	Timestamp int64   `json:"timestamp"`
	Name      string  `json:"name,omitempty"`
}

// ConnInfo holds WebSocket connection and user metadata.
type ConnInfo struct {
	Conn   *websocket.Conn
	UserID string
	Name   string
	mu     sync.Mutex
}

// Pool manages all active WebSocket connections.
type Pool struct {
	sync.RWMutex
	connections map[string]*ConnInfo
}

var pool = &Pool{
	connections: make(map[string]*ConnInfo),
}

// SendJSON sends a JSON message to a connection safely.
func (ci *ConnInfo) SendJSON(v interface{}) error {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	return ci.Conn.WriteJSON(v)
}

// LocationWebSocket handles real-time location sharing via WebSocket.
func LocationWebSocket(c echo.Context) error {
	authHeader := c.Request().Header.Get(echo.HeaderAuthorization)
	token := ""
	if strings.TrimSpace(authHeader) != "" {
		extracted, err := sgmiddleware.ExtractBearerToken(authHeader)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid Authorization header"})
		}
		token = extracted
	}
	if token == "" {
		token = strings.TrimSpace(c.QueryParam("token"))
	}
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing auth token"})
	}

	user, err := sgmiddleware.VerifySupabaseAccessToken(token)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid auth token"})
	}

	userID := user.ID
	userName := c.QueryParam("name")
	if userID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "user not found in token"})
	}

	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	// Set read deadline and pong handler for keepalive
	ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	connInfo := &ConnInfo{Conn: ws, UserID: userID, Name: userName}

	// Register and replace stale connections for the same user.
	var staleConn *ConnInfo
	pool.Lock()
	if existing, ok := pool.connections[userID]; ok && existing != nil {
		staleConn = existing
	}
	pool.connections[userID] = connInfo
	total := len(pool.connections)
	pool.Unlock()
	if staleConn != nil {
		_ = staleConn.Conn.Close()
	}
	log.Printf("WS: user %s (%s) connected. Total: %d", userID, userName, total)

	defer func() {
		pool.Lock()
		if current, ok := pool.connections[userID]; ok && current == connInfo {
			delete(pool.connections, userID)
		}
		total := len(pool.connections)
		pool.Unlock()
		log.Printf("WS: user %s disconnected. Total: %d", userID, total)
	}()

	// Keep the upstream connection active; some proxies close idle WS quickly.
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			connInfo.mu.Lock()
			_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				connInfo.mu.Unlock()
				_ = ws.Close()
				return
			}
			connInfo.mu.Unlock()
		}
	}()

	// Send initial online contacts
	sendOnlineContacts(connInfo)

	// Read loop
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WS error for %s: %v", userID, err)
			}
			break
		}

		var locMsg LocationMessage
		if err := json.Unmarshal(msg, &locMsg); err != nil {
			log.Printf("WS: invalid message from %s: %v", userID, err)
			continue
		}

		// Ignore heartbeat/control frames from clients.
		if locMsg.Type != "" && locMsg.Type != "location_update" {
			continue
		}

		locMsg.UserID = userID
		locMsg.Name = userName

		// Persist to database (async, non-blocking)
		go PersistLocationAsync(config.GetSupabaseConfig(), userID, userName, locMsg.Lat, locMsg.Lng, locMsg.Accuracy)

		// Broadcast to all contacts of this user
		broadcastToContacts(userID, locMsg)
	}

	return nil
}

// broadcastToContacts sends a location update to all contacts of the given user.
func broadcastToContacts(userID string, msg LocationMessage) {
	cfg := config.GetSupabaseConfig()

	// Collect all contactUserIDs from both directions of the relationship
	recipients := make(map[string]bool)

	// Direction 1: user owns the contact row
	contacts, _ := querySupabaseDocuments(cfg, "contacts", "ownerId", userID)
	for _, c := range contacts {
		if id, _ := c["contactUserId"].(string); id != "" {
			recipients[id] = true
		}
	}

	// Direction 2: user is someone else's contact
	reverseContacts, _ := querySupabaseDocuments(cfg, "contacts", "contactUserId", userID)
	for _, c := range reverseContacts {
		if id, _ := c["ownerId"].(string); id != "" {
			recipients[id] = true
		}
	}

	pool.RLock()
	defer pool.RUnlock()

	for recipientID := range recipients {
		if conn, ok := pool.connections[recipientID]; ok {
			if err := conn.SendJSON(msg); err != nil {
				log.Printf("WS: failed to send to %s: %v", recipientID, err)
			}
		}
	}
}

// sendOnlineContacts sends a list of currently online contacts to the user.
func sendOnlineContacts(ci *ConnInfo) {
	cfg := config.GetSupabaseConfig()

	// Collect contact user IDs from both directions
	peerIDs := make(map[string]bool)

	contacts, _ := querySupabaseDocuments(cfg, "contacts", "ownerId", ci.UserID)
	for _, c := range contacts {
		if id, _ := c["contactUserId"].(string); id != "" {
			peerIDs[id] = true
		}
	}

	reverseContacts, _ := querySupabaseDocuments(cfg, "contacts", "contactUserId", ci.UserID)
	for _, c := range reverseContacts {
		if id, _ := c["ownerId"].(string); id != "" {
			peerIDs[id] = true
		}
	}

	pool.RLock()
	defer pool.RUnlock()

	onlineList := make([]map[string]string, 0)
	for peerID := range peerIDs {
		if conn, ok := pool.connections[peerID]; ok {
			onlineList = append(onlineList, map[string]string{
				"userId": peerID,
				"name":   conn.Name,
			})
		}
	}

	ci.SendJSON(map[string]interface{}{
		"type":     "online_contacts",
		"contacts": onlineList,
	})
}

// BroadcastToUser sends a JSON message to a specific user if connected.
func BroadcastToUser(userID string, message interface{}) error {
	pool.RLock()
	conn, ok := pool.connections[userID]
	pool.RUnlock()

	if !ok {
		return fmt.Errorf("user %s not connected", userID)
	}

	return conn.SendJSON(message)
}

// GetOnlineUserIDs returns all currently connected user IDs.
func GetOnlineUserIDs() []string {
	pool.RLock()
	defer pool.RUnlock()

	ids := make([]string, 0, len(pool.connections))
	for id := range pool.connections {
		ids = append(ids, id)
	}
	return ids
}
