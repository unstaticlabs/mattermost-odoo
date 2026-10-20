package odoo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// APIVersion is the version of docs/api.md that this plugin speaks.
const APIVersion = 1

// Client calls the Odoo module usl_mattermost.
type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
	Now     func() time.Time
}

// ProtocolError is a 4xx or 5xx answer from Odoo (docs/api.md, Errors).
type ProtocolError struct {
	Status  int
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("odoo answered %d %s: %s", e.Status, e.Code, e.Message)
}

// Task is the card data that task/read returns.
type Task struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	URL          string  `json:"url"`
	Project      *Named  `json:"project"`
	Stage        *Stage  `json:"stage"`
	State        string  `json:"state"`
	Assignees    []Named `json:"assignees"`
	DateDeadline *string `json:"date_deadline"`
	Tags         []Tag   `json:"tags"`
	Priority     string  `json:"priority"`
	Color        int     `json:"color"`
	WriteDate    *string `json:"write_date"`
}

// Named is a record id with its name.
type Named struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Stage is a task stage.
type Stage struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Folded bool   `json:"folded"`
}

// Tag is a task tag.
type Tag struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color int    `json:"color"`
}

// ReadResult is the answer of task/read.
type ReadResult struct {
	OK      bool   `json:"ok"`
	Visible bool   `json:"visible"`
	Reason  string `json:"reason"`
	Task    *Task  `json:"task"`
}

// WatchResult is the answer of task/watch.
type WatchResult struct {
	OK       bool   `json:"ok"`
	Watching bool   `json:"watching"`
	Reason   string `json:"reason"`
}

// Channel describes the channel of a link.
type Channel struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	TeamName    string `json:"team_name,omitempty"`
}

// LinkResult is the answer of link.
type LinkResult struct {
	OK         bool    `json:"ok"`
	Changed    bool    `json:"changed"`
	Project    *Named  `json:"project"`
	Error      string  `json:"error"`
	Candidates []Named `json:"candidates"`
}

// LinkedProject is one project of the links answer.
type LinkedProject struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	LinkedBy *string `json:"linked_by"`
	LinkedAt *string `json:"linked_at"`
}

// LinksResult is the answer of links.
type LinksResult struct {
	OK       bool            `json:"ok"`
	Projects []LinkedProject `json:"projects"`
	Hidden   int             `json:"hidden"`
	Error    string          `json:"error"`
}

// ReadTask calls task/read as the Pocket ID user `subject`.
func (c *Client) ReadTask(ctx context.Context, subject, channelID string, task TaskRef) (*ReadResult, error) {
	var result ReadResult
	err := c.post(ctx, "/usl/mattermost/task/read", map[string]any{
		"user": subject, "channel_id": channelID, "task": task,
	}, &result)
	return &result, err
}

// Watch calls task/watch. op is "add" or "remove".
func (c *Client) Watch(ctx context.Context, subject, op, channelID, postID string, taskID int) (*WatchResult, error) {
	var result WatchResult
	err := c.post(ctx, "/usl/mattermost/task/watch", map[string]any{
		"user": subject, "op": op, "channel_id": channelID, "post_id": postID, "task_id": taskID,
	}, &result)
	return &result, err
}

// Link calls link. project is a project id or a name.
func (c *Client) Link(ctx context.Context, subject, op string, channel Channel, channelMember bool, project any) (*LinkResult, error) {
	var result LinkResult
	err := c.post(ctx, "/usl/mattermost/link", map[string]any{
		"user": subject, "op": op, "channel": channel, "channel_member": channelMember, "project": project,
	}, &result)
	return &result, err
}

// Links calls links.
func (c *Client) Links(ctx context.Context, subject, channelID string) (*LinksResult, error) {
	var result LinksResult
	err := c.post(ctx, "/usl/mattermost/links", map[string]any{"user": subject, "channel_id": channelID}, &result)
	return &result, err
}

func (c *Client) post(ctx context.Context, path string, fields map[string]any, result any) error {
	fields["v"] = APIVersion
	// A random request id makes every signature unique, so Odoo can refuse
	// a copied request without refusing two equal ones.
	rid := make([]byte, 12)
	if _, err := rand.Read(rid); err != nil {
		return err
	}
	fields["rid"] = hex.EncodeToString(rid)
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, body, result)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, result any) error {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	timestamp := now().Unix()
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set(HeaderTimestamp, fmt.Sprint(timestamp))
	request.Header.Set(HeaderSignature, Sign(c.Key, timestamp, method, path, body))

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		protocolError := &ProtocolError{Status: response.StatusCode}
		_ = json.Unmarshal(answer, protocolError)
		return protocolError
	}
	return json.Unmarshal(answer, result)
}

// HealthResult is the answer of health.
type HealthResult struct {
	OK                bool    `json:"ok"`
	Module            string  `json:"module"`
	Version           *string `json:"version"`
	WebhookConfigured bool    `json:"webhook_configured"`
}

// Health calls health, a signed GET with no body.
func (c *Client) Health(ctx context.Context) (*HealthResult, error) {
	var result HealthResult
	err := c.do(ctx, http.MethodGet, "/usl/mattermost/health", nil, &result)
	return &result, err
}
