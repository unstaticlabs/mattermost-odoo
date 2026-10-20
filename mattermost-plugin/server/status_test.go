package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unstaticlabs/mattermost-odoo/mattermost-plugin/server/odoo"
)

func TestStatusShowsSourcesAndNeverKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "", odoo.Verify([]string{pluginKey}, r.Header.Get(odoo.HeaderTimestamp), r.Header.Get(odoo.HeaderSignature), r.Method, r.URL.Path, body, time.Now()))
		_, _ = w.Write([]byte(`{"v":1,"ok":true,"module":"usl_mattermost","version":"saas~19.3.0.1.0","webhook_configured":true}`))
	}))
	defer server.Close()

	t.Setenv(envWebhookKey, strings.Repeat("e", 40))
	p, api := newPlugin(t, server.URL)
	api.On("HasPermissionTo", "admin", model.PermissionManageSystem).Return(true)
	api.On("HasPermissionTo", "user", model.PermissionManageSystem).Return(false)

	get := func(userID string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		request.Header.Set("Mattermost-User-ID", userID)
		recorder := httptest.NewRecorder()
		p.handleStatus(recorder, request)
		return recorder
	}

	assert.Equal(t, http.StatusForbidden, get("user").Code)
	assert.Equal(t, http.StatusForbidden, get("").Code)

	answer := get("admin")
	require.Equal(t, http.StatusOK, answer.Code)
	assert.NotContains(t, answer.Body.String(), pluginKey)
	assert.NotContains(t, answer.Body.String(), strings.Repeat("e", 40))
	var status pluginStatus
	require.NoError(t, json.Unmarshal(answer.Body.Bytes(), &status))
	assert.Equal(t, []settingStatus{
		{Key: "OdooURL", Source: "settings", Valid: true},
		{Key: "PluginKey", Source: "settings", Valid: true},
		{Key: "WebhookKey", Source: "environment", Valid: true},
		{Key: "PreviousWebhookKey", Source: "empty", Valid: false},
	}, status.Settings)
	require.NotNil(t, status.Odoo)
	assert.True(t, status.Odoo.Reachable)
	assert.True(t, status.Odoo.WebhookConfigured)
}

func TestStatusExplainsAMissingModule(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	p, _ := newPlugin(t, server.URL)

	status := p.status(t.Context())
	assert.False(t, status.Odoo.Reachable)
	assert.Contains(t, status.Odoo.Error, "not installed")
}
