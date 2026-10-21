package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
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
	api.On("GetSession", "session-author").Return(&model.Session{Id: "session-author", UserId: "author"}, nil)

	post := &model.Post{UserId: "author", ChannelId: "channel", Message: "see http://" + host + "/odoo/tasks/1 and http://" + host + "/odoo/tasks/2"}
	updated, rejection := p.MessageWillBePosted(&plugin.Context{SessionId: "session-author"}, post)
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
	api.On("GetSession", "session-peanut").Return(&model.Session{Id: "session-peanut", UserId: "peanut"}, nil)

	post := &model.Post{UserId: "peanut", ChannelId: "channel", Message: "http://" + host + "/odoo/tasks/1"}
	updated, _ := p.MessageWillBePosted(&plugin.Context{SessionId: "session-peanut"}, post)
	assert.Empty(t, updated.Attachments())
}

// forgedCard is a post with card values that a client sent.
func forgedCard(post *model.Post) *model.Post {
	model.ParseMessageAttachment(post, []*model.MessageAttachment{{Title: "Other integration"}, {Title: "Forged", Footer: cardFooter}})
	post.AddProp(propTaskIDs, []any{float64(9)})
	return post
}

func TestPostWithoutTheAuthorSessionGetsNoCards(t *testing.T) {
	server := fakeOdoo(t, map[int]string{1: "First task"}, nil)
	defer server.Close()
	p, api := newPlugin(t, server.URL)
	host := strings.TrimPrefix(server.URL, "http://")
	api.On("GetSession", "session-admin").Return(&model.Session{Id: "session-admin", UserId: "admin"}, nil)

	for name, c := range map[string]*plugin.Context{
		"a plugin, no session":    nil,
		"another user's session":  {SessionId: "session-admin"},
		"an empty plugin context": {},
	} {
		post := forgedCard(&model.Post{UserId: "author", ChannelId: "channel", Message: "http://" + host + "/odoo/tasks/1"})
		updated, _ := p.MessageWillBePosted(c, post)
		require.Len(t, updated.Attachments(), 1, name)
		assert.Equal(t, "Other integration", updated.Attachments()[0].Title, name)
		assert.Nil(t, updated.GetProp(propTaskIDs), name)
	}
}

func TestEditByAnotherUserKeepsTheOldCards(t *testing.T) {
	server := fakeOdoo(t, map[int]string{1: "First task", 2: "Second task"}, nil)
	defer server.Close()
	p, api := newPlugin(t, server.URL)
	host := strings.TrimPrefix(server.URL, "http://")
	api.On("GetSession", "session-admin").Return(&model.Session{Id: "session-admin", UserId: "admin"}, nil)

	oldPost := &model.Post{UserId: "author", ChannelId: "channel", Message: "http://" + host + "/odoo/tasks/1"}
	model.ParseMessageAttachment(oldPost, []*model.MessageAttachment{{Title: "First task", Footer: cardFooter}})
	oldPost.AddProp(propTaskIDs, []any{float64(1)})
	newPost := forgedCard(&model.Post{UserId: "author", ChannelId: "channel", Message: "http://" + host + "/odoo/tasks/2"})

	updated, _ := p.MessageWillBeUpdated(&plugin.Context{SessionId: "session-admin"}, newPost, oldPost)
	var titles []string
	for _, attachment := range updated.Attachments() {
		titles = append(titles, attachment.Title)
	}
	assert.Equal(t, []string{"Other integration", "First task"}, titles)
	assert.Equal(t, []int{1}, taskIDsOf(updated))
}

func TestEditByTheAuthorRecomputesTheCards(t *testing.T) {
	server := fakeOdoo(t, map[int]string{1: "First task", 2: "Second task"}, nil)
	defer server.Close()
	p, api := newPlugin(t, server.URL)
	host := strings.TrimPrefix(server.URL, "http://")
	api.On("GetUser", "author").Return(pocketIDUser("author"), nil)
	api.On("GetSession", "session-author").Return(&model.Session{Id: "session-author", UserId: "author"}, nil)

	oldPost := &model.Post{UserId: "author", ChannelId: "channel", Message: "http://" + host + "/odoo/tasks/1"}
	newPost := forgedCard(&model.Post{UserId: "author", ChannelId: "channel", Message: "http://" + host + "/odoo/tasks/2"})
	updated, _ := p.MessageWillBeUpdated(&plugin.Context{SessionId: "session-author"}, newPost, oldPost)
	var titles []string
	for _, attachment := range updated.Attachments() {
		titles = append(titles, attachment.Title)
	}
	assert.Equal(t, []string{"Other integration", "Second task"}, titles)
	assert.Equal(t, []int{2}, taskIDsOf(updated))
}

func TestUpdateWithoutASessionStaysAsItIs(t *testing.T) {
	p, _ := newPlugin(t, "https://odoo.test")
	newPost := forgedCard(&model.Post{UserId: "author", Message: "same"})
	updated, _ := p.MessageWillBeUpdated(nil, newPost, &model.Post{UserId: "author", Message: "same"})
	assert.Same(t, newPost, updated)
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

func TestTaskChangedRefusesAQueryAndACopy(t *testing.T) {
	p, _ := newPlugin(t, "https://odoo.test")
	payload := map[string]any{"v": 1, "task_id": 1, "posts": []any{}}

	request := signedTaskChanged(t, payload, webhookKey)
	request.URL.RawQuery = "debug=1"
	recorder := httptest.NewRecorder()
	p.handleTaskChanged(recorder, request)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	request = signedTaskChanged(t, payload, webhookKey)
	copied := request.Clone(request.Context())
	body, _ := io.ReadAll(request.Body)
	request.Body = io.NopCloser(bytes.NewReader(body))
	copied.Body = io.NopCloser(bytes.NewReader(body))
	first := httptest.NewRecorder()
	p.handleTaskChanged(first, request)
	assert.Equal(t, http.StatusOK, first.Code, first.Body.String())
	second := httptest.NewRecorder()
	p.handleTaskChanged(second, copied)
	assert.Equal(t, http.StatusUnauthorized, second.Code)
	assert.Contains(t, second.Body.String(), "replayed")
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

func TestEnvironmentWinsOverSettings(t *testing.T) {
	t.Setenv(envPluginKey, strings.Repeat("e", 40))
	t.Setenv(envWebhookKey, strings.Repeat("f", 40))
	t.Setenv(envOdooURL, "https://odoo.env.test")
	config := &configuration{OdooURL: "https://odoo.setting.test", PluginKey: "short", WebhookKey: webhookKey}
	assert.True(t, config.ready())
	assert.Equal(t, "odoo.env.test", config.odooHost())
	assert.Equal(t, []string{strings.Repeat("f", 40)}, config.webhookKeys())
}

// The build puts plugin.json into a JavaScript template string for the
// webapp. A backslash there changes the JSON, and the webapp then fails to
// load.
func TestManifestHasNoBackslash(t *testing.T) {
	raw, err := os.ReadFile("../plugin.json")
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `\`)
}
