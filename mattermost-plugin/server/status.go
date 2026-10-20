package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/unstaticlabs/mattermost-odoo/mattermost-plugin/server/odoo"
)

// settingStatus says where a setting comes from. It never holds the value.
type settingStatus struct {
	Key    string `json:"key"`
	Source string `json:"source"` // "environment", "settings", or "empty"
	Valid  bool   `json:"valid"`
}

type odooStatus struct {
	Reachable         bool    `json:"reachable"`
	Version           *string `json:"version,omitempty"`
	WebhookConfigured bool    `json:"webhook_configured"`
	Error             string  `json:"error,omitempty"`
}

type pluginStatus struct {
	Settings []settingStatus `json:"settings"`
	Odoo     *odooStatus     `json:"odoo"`
}

func source(env, setting string) string {
	switch {
	case strings.TrimSpace(os.Getenv(env)) != "":
		return "environment"
	case strings.TrimSpace(setting) != "":
		return "settings"
	default:
		return "empty"
	}
}

func (p *Plugin) status(ctx context.Context) pluginStatus {
	config := p.getConfiguration()
	keyValid := func(value string) bool { return len(value) >= minKeyLength }
	status := pluginStatus{Settings: []settingStatus{
		{Key: "OdooURL", Source: source(envOdooURL, config.OdooURL), Valid: config.odooHost() != ""},
		{Key: "PluginKey", Source: source(envPluginKey, config.PluginKey), Valid: keyValid(config.pluginKey())},
		{Key: "WebhookKey", Source: source(envWebhookKey, config.WebhookKey), Valid: keyValid(fromEnv(envWebhookKey, config.WebhookKey))},
		{Key: "PreviousWebhookKey", Source: source(envPreviousWebhookKey, config.PreviousWebhookKey), Valid: keyValid(fromEnv(envPreviousWebhookKey, config.PreviousWebhookKey))},
	}}

	client := p.odooClient()
	if client == nil {
		status.Odoo = &odooStatus{Error: "The Odoo URL or the plugin signing key is missing."}
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	health, err := client.Health(ctx)
	var protocolError *odoo.ProtocolError
	switch {
	case errors.As(err, &protocolError) && protocolError.Status == http.StatusNotFound:
		status.Odoo = &odooStatus{Error: "Odoo answers 404: the usl_mattermost module is not installed."}
	case err != nil:
		status.Odoo = &odooStatus{Error: err.Error()}
	default:
		status.Odoo = &odooStatus{Reachable: true, Version: health.Version, WebhookConfigured: health.WebhookConfigured}
	}
	return status
}

// handleStatus serves the status block of the plugin settings page. Only a
// system admin may read it.
func (p *Plugin) handleStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	if userID == "" || !p.API.HasPermissionTo(userID, model.PermissionManageSystem) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(p.status(r.Context()))
}
