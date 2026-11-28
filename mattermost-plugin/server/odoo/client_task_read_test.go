package odoo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTaskRetriesTemporaryUnavailableWithFreshSignature(t *testing.T) {
	requests := make(chan string, 3)
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/usl/mattermost/task/read", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Empty(t, Verify([]string{key}, r.Header.Get(HeaderTimestamp), r.Header.Get(HeaderSignature), r.Method, r.URL.Path, body, time.Now()))
		var fields struct {
			RequestID string  `json:"rid"`
			User      string  `json:"user"`
			ChannelID string  `json:"channel_id"`
			Task      TaskRef `json:"task"`
		}
		assert.NoError(t, json.Unmarshal(body, &fields))
		assert.Equal(t, "subject-1", fields.User)
		assert.Equal(t, "channel-1", fields.ChannelID)
		assert.Equal(t, TaskRef{ID: 5}, fields.Task)
		requests <- fields.RequestID
		if count.Add(1) == 1 {
			http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"visible":true,"task":{"id":5,"name":"Review notes"}}`)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Key: key}
	result, err := client.ReadTask(t.Context(), "subject-1", "channel-1", TaskRef{ID: 5})

	require.NoError(t, err)
	require.True(t, result.Visible)
	require.NotNil(t, result.Task)
	assert.Equal(t, "Review notes", result.Task.Name)
	require.Equal(t, int32(2), count.Load())
	first, second := <-requests, <-requests
	assert.NotEmpty(t, first)
	assert.NotEmpty(t, second)
	assert.NotEqual(t, first, second)
}

func TestReadTaskDoesNotRetryNonTransientResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{"policy refusal", http.StatusForbidden, `{"error":"forbidden"}`, "forbidden"},
		{"configuration missing", http.StatusServiceUnavailable, `{"error":"not_configured"}`, "not_configured"},
		{"unexpected server error", http.StatusBadGateway, `{}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				count.Add(1)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()

			client := &Client{BaseURL: server.URL, Key: key}
			_, err := client.ReadTask(t.Context(), "subject-1", "channel-1", TaskRef{ID: 5})

			var protocol *ProtocolError
			require.ErrorAs(t, err, &protocol)
			assert.Equal(t, test.status, protocol.Status)
			assert.Equal(t, test.code, protocol.Code)
			assert.Equal(t, int32(1), count.Load())
		})
	}
}

func TestTaskReadRetryKeepsTheExistingRequestBudget(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	t.Run("caller deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		defer cancel()
		client := &Client{BaseURL: server.URL, Key: key}
		_, err := client.ReadTask(ctx, "subject-1", "channel-1", TaskRef{ID: 5})
		var protocol *ProtocolError
		require.ErrorAs(t, err, &protocol)
		assert.Equal(t, http.StatusServiceUnavailable, protocol.Status)
		assert.Equal(t, int32(1), count.Swap(0))
	})

	t.Run("http client timeout", func(t *testing.T) {
		client := &Client{BaseURL: server.URL, Key: key, HTTP: &http.Client{Timeout: 150 * time.Millisecond}}
		_, err := client.ReadTask(t.Context(), "subject-1", "channel-1", TaskRef{ID: 5})
		var protocol *ProtocolError
		require.ErrorAs(t, err, &protocol)
		assert.Equal(t, http.StatusServiceUnavailable, protocol.Status)
		assert.Equal(t, int32(1), count.Load())
	})
}

func TestTaskWatchDoesNotRetryServiceUnavailable(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/usl/mattermost/task/watch", r.URL.Path)
		count.Add(1)
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Key: key}
	_, err := client.Watch(t.Context(), "subject-1", "add", "channel-1", "post-1", 5)
	var protocol *ProtocolError
	require.ErrorAs(t, err, &protocol)
	assert.Equal(t, http.StatusServiceUnavailable, protocol.Status)
	assert.Equal(t, int32(1), count.Load())
}

func TestReadTaskStopsOnCancellation(t *testing.T) {
	firstRequest := make(chan struct{}, 1)
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		select {
		case firstRequest <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := &Client{BaseURL: server.URL, Key: key}
	done := make(chan error, 1)
	go func() {
		_, err := client.ReadTask(ctx, "subject-1", "channel-1", TaskRef{ID: 5})
		done <- err
	}()

	select {
	case <-firstRequest:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("synthetic Odoo did not receive the initial read")
	}
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("task read did not return after cancellation")
	}
	assert.Equal(t, int32(1), count.Load())
}

func TestReadTaskStopsAfterTwoRetries(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, Key: key}
	_, err := client.ReadTask(t.Context(), "subject-1", "channel-1", TaskRef{ID: 5})
	var protocol *ProtocolError
	require.ErrorAs(t, err, &protocol)
	assert.Equal(t, http.StatusServiceUnavailable, protocol.Status)
	assert.Equal(t, int32(3), count.Load())
}
