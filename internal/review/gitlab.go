package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PostNote пишет комментарий к merge request.
// authHeader — JOB-TOKEN для CI_JOB_TOKEN или PRIVATE-TOKEN для проектного токена.
func PostNote(ctx context.Context, apiURL, projectID, iid, token, authHeader, body string) error {
	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	projectID = strings.TrimSpace(projectID)
	iid = strings.TrimSpace(iid)
	token = strings.TrimSpace(token)
	if apiURL == "" || projectID == "" || iid == "" || token == "" {
		return fmt.Errorf("gitlab note: api, project, iid and token are required")
	}
	if authHeader == "" {
		authHeader = "JOB-TOKEN"
	}
	endpoint := apiURL + "/projects/" + url.PathEscape(projectID) + "/merge_requests/" + url.PathEscape(iid) + "/notes"
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authHeader, token)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("gitlab note: %s: %s", resp.Status, msg)
	}
	return nil
}
