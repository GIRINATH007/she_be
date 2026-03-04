package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// LocationMessage represents a location update from a client.
type LocationMessage struct {
	Type      string  `json:"type"`      // "location_update"
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
	userID := c.QueryParam("userId")
	userName := c.QueryParam("name")
	if userID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "userId query param required"})
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

	// Register
	pool.Lock()
	pool.connections[userID] = connInfo
	pool.Unlock()
	log.Printf("WS: user %s (%s) connected. Total: %d", userID, userName, len(pool.connections))

	defer func() {
		pool.Lock()
		delete(pool.connections, userID)
		pool.Unlock()
		log.Printf("WS: user %s disconnected. Total: %d", userID, len(pool.connections))
	}()

	// Start ping ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			connInfo.mu.Lock()
			if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				connInfo.mu.Unlock()
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

		locMsg.UserID = userID
		locMsg.Name = userName

		// Broadcast to all contacts of this user
		broadcastToContacts(userID, locMsg)
	}

	return nil
}

// broadcastToContacts sends a location update to all contacts of the given user.
func broadcastToContacts(userID string, msg LocationMessage) {
	cfg := config.GetAppWriteConfig()

	// Get contacts for this user
	contacts, err := queryAppWriteDocuments(cfg, "contacts", "ownerId", userID)
	if err != nil {
		return
	}

	pool.RLock()
	defer pool.RUnlock()

	for _, contact := range contacts {
		contactUserID, _ := contact["contactUserId"].(string)
		if conn, ok := pool.connections[contactUserID]; ok {
			if err := conn.SendJSON(msg); err != nil {
				log.Printf("WS: failed to send to %s: %v", contactUserID, err)
			}
		}
	}
}

// sendOnlineContacts sends a list of currently online contacts to the user.
func sendOnlineContacts(ci *ConnInfo) {
	cfg := config.GetAppWriteConfig()
	contacts, err := queryAppWriteDocuments(cfg, "contacts", "ownerId", ci.UserID)
	if err != nil {
		return
	}

	pool.RLock()
	defer pool.RUnlock()

	onlineList := make([]map[string]string, 0)
	for _, contact := range contacts {
		contactUserID, _ := contact["contactUserId"].(string)
		if conn, ok := pool.connections[contactUserID]; ok {
			onlineList = append(onlineList, map[string]string{
				"userId": contactUserID,
				"name":   conn.Name,
			})
		}
	}

	ci.SendJSON(map[string]interface{}{
		"type":    "online_contacts",
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
