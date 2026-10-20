package odoo

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// MaxTaskLinks is the most task cards one post gets.
const MaxTaskLinks = 3

// TaskRef names a task in an Odoo link: by id, or by an action and a record
// id for an /odoo/action-<action>/<id> link.
type TaskRef struct {
	ID     int    `json:"id"`
	Action string `json:"action,omitempty"`
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>()\[\]"'` + "`" + `]+`)

// The /odoo/<segment>/<id> segments that open a task.
var taskSegments = map[string]bool{
	"tasks":        true,
	"my-tasks":     true,
	"all-tasks":    true,
	"project.task": true,
}

// FindTaskLinks returns the tasks linked in a message, on the Odoo host only,
// in order, without repeats, and at most MaxTaskLinks.
func FindTaskLinks(message, odooHost string) []TaskRef {
	var refs []TaskRef
	seen := map[TaskRef]bool{}
	for _, raw := range urlPattern.FindAllString(message, -1) {
		raw = strings.TrimRight(raw, ".,;:!?*_~")
		ref, ok := ParseTaskLink(raw, odooHost)
		if !ok || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
		if len(refs) == MaxTaskLinks {
			break
		}
	}
	return refs
}

// ParseTaskLink reads one link in a form of docs/api.md.
func ParseTaskLink(raw, odooHost string) (TaskRef, bool) {
	link, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(link.Hostname(), odooHost) {
		return TaskRef{}, false
	}
	path := strings.TrimSuffix(link.Path, "/")
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")

	switch {
	case len(segments) >= 3 && segments[0] == "odoo":
		id, ok := positiveInt(segments[len(segments)-1])
		if !ok {
			return TaskRef{}, false
		}
		previous := segments[len(segments)-2]
		if taskSegments[previous] {
			return TaskRef{ID: id}, true
		}
		if action, found := strings.CutPrefix(previous, "action-"); found && action != "" {
			return TaskRef{ID: id, Action: action}, true
		}
	case len(segments) == 3 && segments[0] == "my" && segments[1] == "tasks":
		if id, ok := positiveInt(segments[2]); ok {
			return TaskRef{ID: id}, true
		}
	case path == "/mail/view":
		return fromQuery(link.Query())
	case path == "/web":
		if fragment, err := url.ParseQuery(link.Fragment); err == nil {
			if fragment.Get("res_id") == "" && fragment.Get("id") != "" {
				fragment.Set("res_id", fragment.Get("id"))
			}
			return fromQuery(fragment)
		}
	}
	return TaskRef{}, false
}

func fromQuery(values url.Values) (TaskRef, bool) {
	if values.Get("model") != "project.task" {
		return TaskRef{}, false
	}
	if id, ok := positiveInt(values.Get("res_id")); ok {
		return TaskRef{ID: id}, true
	}
	return TaskRef{}, false
}

func positiveInt(text string) (int, bool) {
	value, err := strconv.Atoi(text)
	return value, err == nil && value > 0
}
