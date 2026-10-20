package odoo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var key = strings.Repeat("k", 40)

// The same value as signing.signature("k" * 40, 1000, "POST",
// "/usl/mattermost/task/read", b'{"v":1}') in the Odoo module, so both sides
// sign the same bytes.
const pythonVector = "v1=702f2000326141b97f65bffcadd2ac582ed7c71c5dd4ff4b6e0e233b294a20e8"

func TestSignMatchesTheOdooModule(t *testing.T) {
	assert.Equal(t, pythonVector, Sign(key, 1000, "post", "/usl/mattermost/task/read", []byte(`{"v":1}`)))
}

func TestVerify(t *testing.T) {
	signature := Sign(key, 1000, "POST", "/p", []byte("{}"))
	at := func(seconds int64) time.Time { return time.Unix(seconds, 0) }

	assert.Equal(t, "", Verify([]string{key}, "1000", signature, "POST", "/p", []byte("{}"), at(1100)))
	assert.Equal(t, "", Verify([]string{"", strings.Repeat("o", 40), key}, "1000", signature, "POST", "/p", []byte("{}"), at(1000)))
	assert.Equal(t, "bad_signature", Verify([]string{key}, "1000", signature, "POST", "/p", []byte("{ }"), at(1000)))
	assert.Equal(t, "bad_signature", Verify([]string{key}, "1000", signature, "POST", "/q", []byte("{}"), at(1000)))
	assert.Equal(t, "bad_signature", Verify([]string{key}, "1000", "", "POST", "/p", []byte("{}"), at(1000)))
	assert.Equal(t, "stale_timestamp", Verify([]string{key}, "1000", signature, "POST", "/p", []byte("{}"), at(1301)))
	assert.Equal(t, "stale_timestamp", Verify([]string{key}, "1000", signature, "POST", "/p", []byte("{}"), at(699)))
	assert.Equal(t, "stale_timestamp", Verify([]string{key}, "soon", signature, "POST", "/p", []byte("{}"), at(1000)))
}

func TestParseTaskLink(t *testing.T) {
	host := "odoo.unstaticlabs.com"
	cases := map[string]TaskRef{
		"https://odoo.unstaticlabs.com/odoo/project/18/tasks/3278":                 {ID: 3278},
		"https://odoo.unstaticlabs.com/odoo/project.task/3278":                     {ID: 3278},
		"https://odoo.unstaticlabs.com/odoo/my-tasks/3278?debug=1":                 {ID: 3278},
		"https://ODOO.unstaticlabs.com/odoo/all-tasks/3278/":                       {ID: 3278},
		"https://odoo.unstaticlabs.com/odoo/tasks/3278":                            {ID: 3278},
		"https://odoo.unstaticlabs.com/odoo/action-123/3278":                       {ID: 3278, Action: "123"},
		"https://odoo.unstaticlabs.com/odoo/action-project.foo/3278":               {ID: 3278, Action: "project.foo"},
		"https://odoo.unstaticlabs.com/my/tasks/3278":                              {ID: 3278},
		"https://odoo.unstaticlabs.com/mail/view?model=project.task&res_id=9":      {ID: 9},
		"https://odoo.unstaticlabs.com/web#id=7&model=project.task&view_type=form": {ID: 7},
	}
	for link, want := range cases {
		got, ok := ParseTaskLink(link, host)
		assert.True(t, ok, link)
		assert.Equal(t, want, got, link)
	}
	for _, link := range []string{
		"https://example.com/odoo/project/18/tasks/3278",
		"https://odoo.unstaticlabs.com/odoo/project/18",
		"https://odoo.unstaticlabs.com/odoo/project/18/tasks/new",
		"https://odoo.unstaticlabs.com/odoo/res.users/8",
		"https://odoo.unstaticlabs.com/mail/view?model=res.partner&res_id=9",
		"https://odoo.unstaticlabs.com/web#id=7&model=sale.order",
		"https://odoo.unstaticlabs.com/my/tasks/0",
	} {
		_, ok := ParseTaskLink(link, host)
		assert.False(t, ok, link)
	}
}

func TestFindTaskLinks(t *testing.T) {
	message := "See [the task](https://odoo.unstaticlabs.com/odoo/project/18/tasks/1), " +
		"https://odoo.unstaticlabs.com/odoo/project.task/1. and https://odoo.unstaticlabs.com/odoo/tasks/2! " +
		"https://example.com/odoo/tasks/9 https://odoo.unstaticlabs.com/odoo/tasks/3 https://odoo.unstaticlabs.com/odoo/tasks/4"
	assert.Equal(t, []TaskRef{{ID: 1}, {ID: 2}, {ID: 3}}, FindTaskLinks(message, "odoo.unstaticlabs.com"))
	assert.Empty(t, FindTaskLinks("no links here", "odoo.unstaticlabs.com"))
}

func TestClientSignsAndReadsAnswers(t *testing.T) {
	var seen map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		code := Verify([]string{key}, r.Header.Get(HeaderTimestamp), r.Header.Get(HeaderSignature), r.Method, r.URL.Path, body, time.Now())
		if code != "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"v":1,"ok":false,"error":"` + code + `","message":"no"}`))
			return
		}
		require.NoError(t, json.Unmarshal(body, &seen))
		_, _ = w.Write([]byte(`{"v":1,"ok":true,"visible":true,"task":{"id":5,"name":"Fix it","url":"u","project":{"id":2,"name":"P"},"date_deadline":null}}`))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL + "/", Key: key}
	result, err := client.ReadTask(context.Background(), "subject-1", "channel-1", TaskRef{ID: 5})
	require.NoError(t, err)
	assert.True(t, result.Visible)
	assert.Equal(t, "Fix it", result.Task.Name)
	assert.Nil(t, result.Task.DateDeadline)
	assert.Equal(t, map[string]any{"v": float64(1), "user": "subject-1", "channel_id": "channel-1", "task": map[string]any{"id": float64(5)}}, seen)

	wrong := &Client{BaseURL: server.URL, Key: strings.Repeat("x", 40)}
	_, err = wrong.ReadTask(context.Background(), "subject-1", "channel-1", TaskRef{ID: 5})
	var protocolError *ProtocolError
	require.ErrorAs(t, err, &protocolError)
	assert.Equal(t, http.StatusUnauthorized, protocolError.Status)
	assert.Equal(t, "bad_signature", protocolError.Code)
}
