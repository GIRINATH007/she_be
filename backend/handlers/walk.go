package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
	"github.com/sheguard/backend/services"
)

// InviteWalk creates a walk session and notifies the selected contacts.
func InviteWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()

	var input struct {
		ContactIDs []string `json:"contactIds"`
	}
	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if len(input.ContactIDs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one contact is required"})
	}

	normalized := make([]string, 0, len(input.ContactIDs))
	seen := make(map[string]bool)
	for _, id := range input.ContactIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one valid contact is required"})
	}

	myContacts, _ := querySupabaseDocuments(cfg, "contacts", "ownerId", userID)
	allowed := make(map[string]bool)
	for _, contact := range myContacts {
		contactUserID, _ := contact["contactUserId"].(string)
		if contactUserID != "" {
			allowed[contactUserID] = true
		}
	}
	// Also check reverse direction (contacts who added me)
	reverseContacts, _ := querySupabaseDocuments(cfg, "contacts", "contactUserId", userID)
	for _, contact := range reverseContacts {
		ownerID, _ := contact["ownerId"].(string)
		if ownerID != "" {
			allowed[ownerID] = true
		}
	}
	for _, id := range normalized {
		if !allowed[id] {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "you can only invite your own contacts"})
		}
	}

	existing, _ := querySupabaseDocuments(cfg, "walk_sessions", "requesterId", userID)
	for _, doc := range existing {
		status, _ := doc["status"].(string)
		if status == "pending" || status == "active" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "you already have an active walk session"})
		}
	}

	requesterProfile, _ := queryProfileByUserID(cfg, userID)
	requesterName := "Someone"
	if requesterProfile != nil {
		if name, ok := requesterProfile["name"].(string); ok {
			requesterName = name
		}
	}

	walkData := map[string]interface{}{
		"requesterId": userID,
		"accepterId":  "",
		"invitedIds":  normalized,
		"status":      "pending",
		"startedAt":   time.Now().UTC().Format(time.RFC3339),
		"endedAt":     "",
	}

	walkDoc, err := createSupabaseDocument(cfg, "walk_sessions", walkData)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create walk session"})
	}

	walkID := valueAsString(walkDoc["$id"])

	notifiedCount := 0
	for _, contactUserID := range normalized {
		notifData := services.FCMData{
			"type":          "walk_invite",
			"walkId":        walkID,
			"requesterId":   userID,
			"requesterName": requesterName,
		}

		go services.SendPushToUser(contactUserID, services.FCMNotification{
			Title: "?? Walk With Me",
			Body:  fmt.Sprintf("%s wants you to walk with them!", requesterName),
		}, notifData)

		go BroadcastToUser(contactUserID, map[string]interface{}{
			"type":          "walk_invite",
			"walkId":        walkID,
			"requesterId":   userID,
			"requesterName": requesterName,
		})

		notifiedCount++
	}

	log.Printf("Walk: invite by %s (%s), invited %d contacts, walkId=%s", userID, requesterName, notifiedCount, walkID)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"walkId":   walkID,
		"status":   "pending",
		"notified": notifiedCount,
	})
}

// AcceptWalk accepts a walk invitation and starts the walk session.
func AcceptWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()
	walkID := c.Param("id")

	if walkID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "walk ID is required"})
	}

	walkDoc, err := getSupabaseDocument(cfg, "walk_sessions", walkID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "walk session not found"})
	}

	status, _ := walkDoc["status"].(string)
	if status != "pending" {
		return c.JSON(http.StatusConflict, map[string]string{"error": "walk can only be accepted while pending"})
	}

	invitedIDs := extractStringArray(walkDoc, "invitedIds")
	if !containsString(invitedIDs, userID) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "you are not invited to this walk"})
	}

	existingAccepterID, _ := walkDoc["accepterId"].(string)
	if strings.TrimSpace(existingAccepterID) != "" {
		return c.JSON(http.StatusConflict, map[string]string{"error": "walk has already been accepted"})
	}

	_, err = updateSupabaseDocument(cfg, "walk_sessions", walkID, map[string]interface{}{
		"accepterId": userID,
		"status":     "active",
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to accept walk"})
	}

	accepterProfile, _ := queryProfileByUserID(cfg, userID)
	accepterName := "Someone"
	if accepterProfile != nil {
		if name, ok := accepterProfile["name"].(string); ok {
			accepterName = name
		}
	}

	requesterID, _ := walkDoc["requesterId"].(string)

	go services.SendPushToUser(requesterID, services.FCMNotification{
		Title: "? Walk Accepted!",
		Body:  fmt.Sprintf("%s is now walking with you!", accepterName),
	}, services.FCMData{
		"type":         "walk_accepted",
		"walkId":       walkID,
		"accepterId":   userID,
		"accepterName": accepterName,
	})

	go BroadcastToUser(requesterID, map[string]interface{}{
		"type":         "walk_accepted",
		"walkId":       walkID,
		"accepterId":   userID,
		"accepterName": accepterName,
	})

	log.Printf("Walk: accepted by %s (%s), walkId=%s", userID, accepterName, walkID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "walk accepted",
		"walkId":  walkID,
	})
}

// RejectWalk rejects a walk invitation.
func RejectWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()
	walkID := c.Param("id")

	if walkID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "walk ID is required"})
	}

	walkDoc, err := getSupabaseDocument(cfg, "walk_sessions", walkID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "walk session not found"})
	}

	status, _ := walkDoc["status"].(string)
	if status != "pending" {
		return c.JSON(http.StatusConflict, map[string]string{"error": "walk can only be rejected while pending"})
	}

	invitedIDs := extractStringArray(walkDoc, "invitedIds")
	if !containsString(invitedIDs, userID) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "you are not invited to this walk"})
	}

	requesterID, _ := walkDoc["requesterId"].(string)

	rejectorProfile, _ := queryProfileByUserID(cfg, userID)
	rejectorName := "Someone"
	if rejectorProfile != nil {
		if name, ok := rejectorProfile["name"].(string); ok {
			rejectorName = name
		}
	}

	remainingIDs := make([]string, 0)
	for _, id := range invitedIDs {
		if id != userID {
			remainingIDs = append(remainingIDs, id)
		}
	}

	newStatus := "pending"
	if len(remainingIDs) == 0 {
		newStatus = "cancelled"
	}

	_, err = updateSupabaseDocument(cfg, "walk_sessions", walkID, map[string]interface{}{
		"invitedIds": remainingIDs,
		"status":     newStatus,
		"endedAt": func() string {
			if newStatus == "cancelled" {
				return time.Now().UTC().Format(time.RFC3339)
			}
			return ""
		}(),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to reject walk"})
	}

	go BroadcastToUser(requesterID, map[string]interface{}{
		"type":         "walk_rejected",
		"walkId":       walkID,
		"rejectorId":   userID,
		"rejectorName": rejectorName,
		"newStatus":    newStatus,
	})

	log.Printf("Walk: rejected by %s (%s), walkId=%s, remaining=%d", userID, rejectorName, walkID, len(remainingIDs))

	return c.JSON(http.StatusOK, map[string]string{
		"message": "walk rejected",
		"walkId":  walkID,
	})
}

// CompleteWalk marks a walk session as completed.
func CompleteWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()
	walkID := c.Param("id")

	if walkID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "walk ID is required"})
	}

	walkDoc, err := getSupabaseDocument(cfg, "walk_sessions", walkID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "walk session not found"})
	}

	requesterID, _ := walkDoc["requesterId"].(string)
	accepterID, _ := walkDoc["accepterId"].(string)
	status, _ := walkDoc["status"].(string)
	if status != "active" {
		return c.JSON(http.StatusConflict, map[string]string{"error": "walk is not active"})
	}
	if userID != requesterID && userID != accepterID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "you are not part of this walk"})
	}

	_, err = updateSupabaseDocument(cfg, "walk_sessions", walkID, map[string]interface{}{
		"status":  "completed",
		"endedAt": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to complete walk"})
	}

	completerProfile, _ := queryProfileByUserID(cfg, userID)
	completerName := "Someone"
	if completerProfile != nil {
		if name, ok := completerProfile["name"].(string); ok {
			completerName = name
		}
	}

	otherUserID := requesterID
	if userID == requesterID {
		otherUserID = accepterID
	}

	if otherUserID != "" {
		go services.SendPushToUser(otherUserID, services.FCMNotification{
			Title: "?? Walk Completed",
			Body:  fmt.Sprintf("%s has arrived safely!", completerName),
		}, services.FCMData{
			"type":   "walk_completed",
			"walkId": walkID,
		})

		go BroadcastToUser(otherUserID, map[string]interface{}{
			"type":          "walk_completed",
			"walkId":        walkID,
			"completedBy":   userID,
			"completerName": completerName,
		})
	}

	log.Printf("Walk: completed by %s, walkId=%s", userID, walkID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "walk completed",
		"walkId":  walkID,
	})
}

// CancelWalk cancels a pending or active walk session.
func CancelWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()
	walkID := c.Param("id")

	if walkID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "walk ID is required"})
	}

	walkDoc, err := getSupabaseDocument(cfg, "walk_sessions", walkID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "walk session not found"})
	}

	requesterID, _ := walkDoc["requesterId"].(string)
	accepterID, _ := walkDoc["accepterId"].(string)
	status, _ := walkDoc["status"].(string)
	if status != "pending" && status != "active" {
		return c.JSON(http.StatusConflict, map[string]string{"error": "walk cannot be cancelled in current state"})
	}
	if userID != requesterID && userID != accepterID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "you are not allowed to cancel this walk"})
	}

	_, err = updateSupabaseDocument(cfg, "walk_sessions", walkID, map[string]interface{}{
		"status":  "cancelled",
		"endedAt": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to cancel walk"})
	}

	cancellerProfile, _ := queryProfileByUserID(cfg, userID)
	cancellerName := "Someone"
	if cancellerProfile != nil {
		if name, ok := cancellerProfile["name"].(string); ok {
			cancellerName = name
		}
	}

	otherUserID := requesterID
	if userID == requesterID {
		otherUserID = accepterID
	}

	if otherUserID != "" {
		go BroadcastToUser(otherUserID, map[string]interface{}{
			"type":          "walk_cancelled",
			"walkId":        walkID,
			"cancelledBy":   userID,
			"cancellerName": cancellerName,
		})

		go services.SendPushToUser(otherUserID, services.FCMNotification{
			Title: "? Walk Cancelled",
			Body:  fmt.Sprintf("%s cancelled the walk session.", cancellerName),
		}, services.FCMData{
			"type":   "walk_cancelled",
			"walkId": walkID,
		})
	}

	invitedIDs := extractStringArray(walkDoc, "invitedIds")
	for _, invitedID := range invitedIDs {
		if invitedID != userID {
			go BroadcastToUser(invitedID, map[string]interface{}{
				"type":          "walk_cancelled",
				"walkId":        walkID,
				"cancelledBy":   userID,
				"cancellerName": cancellerName,
			})
		}
	}

	log.Printf("Walk: cancelled by %s, walkId=%s", userID, walkID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "walk cancelled",
		"walkId":  walkID,
	})
}

// GetActiveWalk returns the user's current active or pending walk session.
func GetActiveWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()

	docs, _ := querySupabaseDocuments(cfg, "walk_sessions", "requesterId", userID)
	if doc := pickLatestWalkByStatus(docs, "active", "pending"); doc != nil {
		doc["role"] = "requester"
		if walkID := valueAsString(doc["$id"]); walkID != "" {
			doc["walkId"] = walkID
		}
		return c.JSON(http.StatusOK, doc)
	}

	docs, _ = querySupabaseDocuments(cfg, "walk_sessions", "accepterId", userID)
	if doc := pickLatestWalkByStatus(docs, "active"); doc != nil {
		doc["role"] = "accepter"
		if walkID := valueAsString(doc["$id"]); walkID != "" {
			doc["walkId"] = walkID
		}
		return c.JSON(http.StatusOK, doc)
	}

	inviteDocs, _ := queryPendingWalkInvitesForUser(cfg, userID)
	if invite := pickLatestInvite(inviteDocs); invite != nil {
		invite["role"] = "invitee"
		if walkID := valueAsString(invite["$id"]); walkID != "" {
			invite["walkId"] = walkID
		}
		requesterID := valueAsString(invite["requesterId"])
		if requesterID != "" {
			if requesterProfile, _ := queryProfileByUserID(cfg, requesterID); requesterProfile != nil {
				if requesterName, ok := requesterProfile["name"].(string); ok && strings.TrimSpace(requesterName) != "" {
					invite["requesterName"] = requesterName
				}
			}
		}
		return c.JSON(http.StatusOK, invite)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"active": false,
	})
}

func queryPendingWalkInvitesForUser(cfg *config.Config, userID string) ([]map[string]interface{}, error) {
	params := url.Values{}
	params.Set("status", "eq.pending")
	params.Set("invited_ids", fmt.Sprintf("cs.{%s}", userID))
	params.Set("select", "*")
	params.Set("limit", "50")

	resp, err := doSupabaseServiceRequest(cfg, http.MethodGet, "walk_sessions", params, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Supabase error (%d) while fetching pending invites", resp.StatusCode)
	}

	rows, err := decodeRows(resp)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		result = append(result, toAppData("walk_sessions", row))
	}
	return result, nil
}

func pickLatestWalkByStatus(docs []map[string]interface{}, statuses ...string) map[string]interface{} {
	if len(docs) == 0 || len(statuses) == 0 {
		return nil
	}

	for _, desiredStatus := range statuses {
		var best map[string]interface{}
		var bestTime time.Time
		for _, doc := range docs {
			status := valueAsString(doc["status"])
			if status != desiredStatus {
				continue
			}
			startedAt := parseRFC3339(valueAsString(doc["startedAt"]))
			if best == nil || startedAt.After(bestTime) {
				best = doc
				bestTime = startedAt
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

func pickLatestInvite(docs []map[string]interface{}) map[string]interface{} {
	var best map[string]interface{}
	var bestTime time.Time

	for _, doc := range docs {
		if valueAsString(doc["status"]) != "pending" {
			continue
		}
		if strings.TrimSpace(valueAsString(doc["accepterId"])) != "" {
			continue
		}
		startedAt := parseRFC3339(valueAsString(doc["startedAt"]))
		if best == nil || startedAt.After(bestTime) {
			best = doc
			bestTime = startedAt
		}
	}
	return best
}

func parseRFC3339(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func valueAsString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	case float32:
		if v == float32(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int16:
		return fmt.Sprintf("%d", v)
	case int8:
		return fmt.Sprintf("%d", v)
	case uint:
		return fmt.Sprintf("%d", v)
	case uint64:
		return fmt.Sprintf("%d", v)
	case uint32:
		return fmt.Sprintf("%d", v)
	case uint16:
		return fmt.Sprintf("%d", v)
	case uint8:
		return fmt.Sprintf("%d", v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func extractStringArray(doc map[string]interface{}, key string) []string {
	result := make([]string, 0)
	if arr, ok := doc[key].([]interface{}); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
	}
	return result
}

func containsString(arr []string, target string) bool {
	for _, item := range arr {
		if item == target {
			return true
		}
	}
	return false
}
