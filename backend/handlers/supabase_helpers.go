package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sheguard/backend/config"
)

var appToDBFieldMap = map[string]map[string]string{
	"profiles": {
		"$id":         "id",
		"userId":      "user_id",
		"name":        "name",
		"phone":       "phone",
		"bloodGroup":  "blood_group",
		"allergies":   "allergies",
		"medications": "medications",
		"pinHash":     "pin_hash",
		"fcmToken":    "fcm_token",
		"avatarUrl":   "avatar_url",
	},
	"contacts": {
		"$id":           "id",
		"ownerId":       "owner_id",
		"contactUserId": "contact_user_id",
		"type":          "type",
		"status":        "status",
	},
	"sos_events": {
		"$id":          "id",
		"triggeredBy":  "triggered_by",
		"type":         "type",
		"status":       "status",
		"agoraChannel": "agora_channel",
		"startedAt":    "started_at",
		"endedAt":      "ended_at",
	},
	"walk_sessions": {
		"$id":         "id",
		"requesterId": "requester_id",
		"accepterId":  "accepter_id",
		"invitedIds":  "invited_ids",
		"status":      "status",
		"startedAt":   "started_at",
		"endedAt":     "ended_at",
	},
	"user_locations": {
		"$id":       "user_id",
		"userId":    "user_id",
		"lat":       "lat",
		"lng":       "lng",
		"accuracy":  "accuracy",
		"name":      "name",
		"updatedAt": "updated_at",
	},
}

func fieldToDB(table, field string) string {
	if m, ok := appToDBFieldMap[table]; ok {
		if mapped, ok := m[field]; ok {
			return mapped
		}
	}
	return field
}

func fieldToApp(table, field string) string {
	if m, ok := appToDBFieldMap[table]; ok {
		for appField, dbField := range m {
			if dbField == field {
				return appField
			}
		}
	}
	return field
}

func toDBData(table string, data map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(data))
	for k, v := range data {
		dbField := fieldToDB(table, k)
		out[dbField] = v
	}
	return out
}

func toAppData(table string, row map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(row)+1)
	for k, v := range row {
		appField := fieldToApp(table, k)
		out[appField] = v
	}
	if id, ok := row["id"]; ok {
		out["$id"] = id
	}
	return out
}

func supabaseRestBase(cfg *config.Config) string {
	return strings.TrimRight(cfg.SupabaseURL, "/") + "/rest/v1"
}

func doSupabaseServiceRequest(cfg *config.Config, method, path string, query url.Values, payload map[string]interface{}) (*http.Response, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing config")
	}
	if cfg.SupabaseURL == "" || cfg.SupabaseServiceRoleKey == "" {
		return nil, fmt.Errorf("supabase service role config missing")
	}

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}

	u := supabaseRestBase(cfg) + "/" + path
	if query != nil && len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("apikey", cfg.SupabaseServiceRoleKey)
	req.Header.Set("Authorization", "Bearer "+cfg.SupabaseServiceRoleKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=representation")

	return http.DefaultClient.Do(req)
}

func decodeRows(resp *http.Response) ([]map[string]interface{}, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return []map[string]interface{}{}, nil
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return []map[string]interface{}{obj}, nil
	}

	return nil, fmt.Errorf("failed to parse Supabase response: %s", string(raw))
}

func querySupabaseDocuments(cfg *config.Config, table, field, value string) ([]map[string]interface{}, error) {
	params := url.Values{}
	params.Set(fieldToDB(table, field), "eq."+value)
	params.Set("select", "*")
	params.Set("limit", "100")

	resp, err := doSupabaseServiceRequest(cfg, "GET", table, params, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Supabase error (%d): %s", resp.StatusCode, string(body))
	}

	rows, err := decodeRows(resp)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAppData(table, row))
	}
	return out, nil
}

func querySupabaseDocumentByField(cfg *config.Config, table, field, value string) (map[string]interface{}, error) {
	docs, err := querySupabaseDocuments(cfg, table, field, value)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("not found")
	}
	return docs[0], nil
}

func queryProfileByUserID(cfg *config.Config, userID string) (map[string]interface{}, error) {
	return querySupabaseDocumentByField(cfg, "profiles", "userId", userID)
}

func createSupabaseDocument(cfg *config.Config, table string, data map[string]interface{}) (map[string]interface{}, error) {
	resp, err := doSupabaseServiceRequest(cfg, "POST", table, nil, toDBData(table, data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Supabase error (%d): %s", resp.StatusCode, string(body))
	}

	rows, err := decodeRows(resp)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty create response")
	}
	return toAppData(table, rows[0]), nil
}

// upsertSupabaseDocument performs an INSERT with ON CONFLICT UPDATE (merge-duplicates).
func upsertSupabaseDocument(cfg *config.Config, table string, data map[string]interface{}) error {
	if cfg == nil || cfg.SupabaseURL == "" || cfg.SupabaseServiceRoleKey == "" {
		return fmt.Errorf("supabase config missing")
	}

	dbData := toDBData(table, data)
	raw, err := json.Marshal(dbData)
	if err != nil {
		return err
	}

	u := supabaseRestBase(cfg) + "/" + table
	req, err := http.NewRequest("POST", u, bytes.NewReader(raw))
	if err != nil {
		return err
	}

	req.Header.Set("apikey", cfg.SupabaseServiceRoleKey)
	req.Header.Set("Authorization", "Bearer "+cfg.SupabaseServiceRoleKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "resolution=merge-duplicates")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Supabase upsert error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func updateSupabaseDocument(cfg *config.Config, table, docID string, data map[string]interface{}) (map[string]interface{}, error) {
	params := url.Values{}
	params.Set("id", "eq."+docID)

	resp, err := doSupabaseServiceRequest(cfg, "PATCH", table, params, toDBData(table, data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Supabase error (%d): %s", resp.StatusCode, string(body))
	}

	rows, err := decodeRows(resp)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("document not found")
	}
	return toAppData(table, rows[0]), nil
}

func deleteSupabaseDocument(cfg *config.Config, table, docID string) error {
	params := url.Values{}
	params.Set("id", "eq."+docID)

	resp, err := doSupabaseServiceRequest(cfg, "DELETE", table, params, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Supabase error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func getSupabaseDocument(cfg *config.Config, table, docID string) (map[string]interface{}, error) {
	params := url.Values{}
	params.Set("id", "eq."+docID)
	params.Set("select", "*")
	params.Set("limit", "1")

	resp, err := doSupabaseServiceRequest(cfg, "GET", table, params, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Supabase error (%d): %s", resp.StatusCode, string(body))
	}

	rows, err := decodeRows(resp)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("not found")
	}
	return toAppData(table, rows[0]), nil
}
