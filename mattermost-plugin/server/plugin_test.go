package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/unstaticlabs/mattermost-odoo/mattermost-plugin/server/odoo"
)

var (
	pluginKey  = strings.Repeat("p", 40)
	webhookKey = strings.Repeat("w", 40)
)

// fakeOdoo answers task/read with the tasks in `tasks`, as visible, and with
// `reasons` for the others.
func fakeOdoo(t *testing.T, tasks map[int]string, reasons map[int]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.Equal(t, "", odoo.Verify([]string{pluginKey}, r.Header.Get(odoo.HeaderTimestamp), r.Header.Get(odoo.HeaderSignature), r.Method, r.URL.Path, body, time.Now()))
		var request struct {
			Task odoo.TaskRef `json:"task"`
		}
		require.NoError(t, json.Unmarshal(body, &request))
		if name, ok := tasks[request.Task.ID]; ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"v": 1, "ok": true, "visible": true, "task": map[string]any{
				"id": request.Task.ID, "name": name, "url": "https://odoo.test/odoo/project.task/1",
				"project": map[string]any{"id": 2, "name": "Project"}, "stage": map[string]any{"id": 3, "name": "Doing", "folded": false},
				"state": "01_in_progress", "assignees": []any{map[string]any{"id": 8, "name": "Roger"}},
				"date_deadline": "2026-10-20T16:00:00Z", "tags": []any{map[string]any{"id": 1, "name": "infra", "color": 4}},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"v": 1, "ok": true, "visible": false, "reason": reasons[request.Task.ID]})
	}))
}

func newPlugin(t *testing.T, odooURL string) (*Plugin, *plugintest.API) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.setConfiguration(&configuration{OdooURL: odooURL, PluginKey: pluginKey, WebhookKey: webhookKey})
	p.router = p.initRouter()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	t.Cleanup(func() { api.AssertExpectations(t) })
	return p, api
}

func pocketIDUser(id string) *model.User {
	subject := "subject-" + id
	return &model.User{Id: id, Username: id, AuthService: model.ServiceOpenid, AuthData: &subject}
}

func TestMessageWillBePostedAddsCards(t *testing.T) {
	server := fakeOdoo(t, map[int]string{1: "First task"}, map[int]string{2: "not_shared"})
	defer server.Close()
	p, api := newPlugin(t, server.URL)
	host := strings.TrimPrefix(server.URL, "http://")
	api.On("GetUser", "author").Return(pocketIDUser("author"), nil)

	post := &model.Post{UserId: "author", ChannelId: "channel", Message: "see http://" + host + "/odoo/tasks/1 and http://" + host + "/odoo/tasks/2"}
	updated, rejection := p.MessageWillBePosted(nil, post)
	require.Equal(t, "", rejection)
	attachments := updated.Attachments()
	require.Len(t, attachments, 1)
	assert.Equal(t, "First task", attachments[0].Title)
	assert.Equal(t, cardFooter, attachments[0].Footer)
	assert.Equal(t, []int{1}, taskIDsOf(updated))
	assert.Empty(t, post.Attachments(), "the original post must not change")
}

func TestNoCardsWithoutPocketID(t *testing.T) {
	server := fakeOdoo(t, map[int]string{1: "First task"}, nil)
	defer server.Close()
	p, api := newPlugin(t, server.URL)
	host := strings.TrimPrefix(server.URL, "http://")
	api.On("GetUser", "peanut").Return(&model.User{Id: "peanut", AuthService: ""}, nil)

	post := &model.Post{UserId: "peanut", ChannelId: "channel", Message: "http://" + host + "/odoo/tasks/1"}
	updated, _ := p.MessageWillBePosted(nil, post)
	assert.Empty(t, updated.Attachments())
}

func TestApplyCardsKeepsOtherAttachments(t *testing.T) {
	post := &model.Post{}
	model.ParseMessageAttachment(post, []*model.MessageAttachment{{Title: "Other integration"}, {Title: "Old card", Footer: cardFooter}})
	updated := applyCards(post, []*model.MessageAttachment{{Title: "New card", Footer: cardFooter}}, []int{4})
	var titles []string
	for _, attachment := range updated.Attachments() {
		titles = append(titles, attachment.Title)
	}
	assert.Equal(t, []string{"Other integration", "New card"}, titles)

	cleared := applyCards(updated, nil, nil)
	require.Len(t, cleared.Attachments(), 1)
	assert.Nil(t, cleared.GetProp(propTaskIDs))
}

func TestTaskIDsSurviveJSON(t *testing.T) {
	post := &model.Post{}
	post.AddProp(propTaskIDs, []int{3, 5})
	raw, err := json.Marshal(post)
	require.NoError(t, err)
	var decoded model.Post
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, []int{3, 5}, taskIDsOf(&decoded))
}

func signedTaskChanged(t *testing.T, payload map[string]any, keyToUse string) *http.Request {
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	timestamp := time.Now().Unix()
	path := "/plugins/" + manifest.Id + "/odoo/task-changed"
	request := httptest.NewRequest(http.MethodPost, "/odoo/task-changed", bytes.NewReader(body))
	request.Header.Set(odoo.HeaderTimestamp, itoa(timestamp))
	request.Header.Set(odoo.HeaderSignature, odoo.Sign(keyToUse, timestamp, http.MethodPost, path, body))
	return request
}

func itoa(value int64) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func TestTaskChangedRefusesAWrongKey(t *testing.T) {
	p, _ := newPlugin(t, "https://odoo.test")
	recorder := httptest.NewRecorder()
	p.handleTaskChanged(recorder, signedTaskChanged(t, map[string]any{"v": 1, "task_id": 1, "posts": []any{}}, strings.Repeat("x", 40)))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestTaskChangedRefreshesTheCard(t *testing.T) {
	server := fakeOdoo(t, nil, map[int]string{1: "archived"})
	defer server.Close()
	p, api := newPlugin(t, server.URL)
	host := strings.TrimPrefix(server.URL, "http://")
	post := &model.Post{Id: "post", UserId: "author", ChannelId: "channel", Message: "http://" + host + "/odoo/tasks/1"}
	model.ParseMessageAttachment(post, []*model.MessageAttachment{{Title: "Old name", Footer: cardFooter}})
	post.AddProp(propTaskIDs, []any{float64(1)})
	api.On("GetPost", "post").Return(post, nil)
	api.On("GetUser", "author").Return(pocketIDUser("author"), nil)
	api.On("UpdatePost", mock.MatchedBy(func(updated *model.Post) bool {
		attachments := updated.Attachments()
		return len(attachments) == 1 && attachments[0].Title == "This Odoo task is no longer available"
	})).Return(post, nil)

	recorder := httptest.NewRecorder()
	p.handleTaskChanged(recorder, signedTaskChanged(t, map[string]any{
		"v": 1, "task_id": 1, "posts": []any{map[string]any{"post_id": "post", "channel_id": "channel"}},
	}, webhookKey))
	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
}

func TestPositiveID(t *testing.T) {
	id, ok := positiveID("21")
	assert.True(t, ok)
	assert.Equal(t, 21, id)
	for _, text := range []string{"0", "-1", "21a", "Kinkverse", " 21"} {
		_, ok := positiveID(text)
		assert.False(t, ok, text)
	}
}
