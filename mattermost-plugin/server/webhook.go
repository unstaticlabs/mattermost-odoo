package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/unstaticlabs/mattermost-odoo/mattermost-plugin/server/odoo"
)

type taskChanged struct {
	V      int `json:"v"`
	TaskID int `json:"task_id"`
	Posts  []struct {
		PostID    string `json:"post_id"`
		ChannelID string `json:"channel_id"`
	} `json:"posts"`
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	payload["v"] = odoo.APIVersion
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": code})
}

// handleTaskChanged is Odoo's "task changed" call (docs/api.md). It reads the
// task again for each post, as the post's author, and updates the cards.
func (p *Plugin) handleTaskChanged(w http.ResponseWriter, r *http.Request) {
	keys := p.getConfiguration().webhookKeys()
	if len(keys) == 0 {
		writeError(w, http.StatusServiceUnavailable, "not_configured")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large")
		return
	}
	// Odoo signs the full path; the plugin sees it without the prefix.
	path := "/plugins/" + manifest.Id + r.URL.Path
	if code := odoo.Verify(keys, r.Header.Get(odoo.HeaderTimestamp), r.Header.Get(odoo.HeaderSignature), r.Method, path, body, time.Now()); code != "" {
		writeError(w, http.StatusUnauthorized, code)
		return
	}
	var change taskChanged
	if err := json.Unmarshal(body, &change); err != nil || change.TaskID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if change.V != odoo.APIVersion {
		writeError(w, http.StatusBadRequest, "unsupported_version")
		return
	}
	for _, changed := range change.Posts {
		p.refreshPost(changed.PostID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Plugin) refreshPost(postID string) {
	post, appErr := p.API.GetPost(postID)
	if appErr != nil || post.DeleteAt != 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout*3)
	defer cancel()
	cards, taskIDs, ok := p.cardsFor(ctx, post, taskIDsOf(post))
	if !ok {
		return
	}
	updated := applyCards(post, cards, taskIDs)
	if reflect.DeepEqual(updated.GetProps(), post.GetProps()) {
		return
	}
	if _, appErr := p.API.UpdatePost(updated); appErr != nil {
		p.API.LogWarn("Odoo card refresh failed", "post", postID, "err", appErr.Error())
	}
}
