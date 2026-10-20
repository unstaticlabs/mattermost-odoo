package main

import (
	"context"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/unstaticlabs/mattermost-odoo/mattermost-plugin/server/odoo"
)

const (
	// propTaskIDs lists the tasks that have a card on a post, as numbers.
	propTaskIDs = "usl_odoo_task_ids"
	cardFooter  = "Odoo"
	// readTimeout bounds the Odoo calls made while a message is posted.
	readTimeout = 3 * time.Second
)

var stateColors = map[string]string{
	"01_in_progress":       "#2389d7",
	"02_changes_requested": "#e8a33d",
	"03_approved":          "#3db887",
	"04_waiting_normal":    "#a0a0a0",
	"1_done":               "#3db887",
	"1_canceled":           "#a0a0a0",
}

// taskAttachment is the card of one task.
func taskAttachment(task *odoo.Task) *model.MessageAttachment {
	attachment := &model.MessageAttachment{
		Title:     task.Name,
		TitleLink: task.URL,
		Fallback:  task.Name,
		Color:     stateColors[task.State],
		Footer:    cardFooter,
	}
	add := func(title, value string, short bool) {
		if value != "" {
			attachment.Fields = append(attachment.Fields, &model.MessageAttachmentField{Title: title, Value: value, Short: model.SlackCompatibleBool(short)})
		}
	}
	if task.Project != nil {
		add("Project", task.Project.Name, true)
	}
	if task.Stage != nil {
		add("Stage", task.Stage.Name, true)
	}
	names := make([]string, 0, len(task.Assignees))
	for _, assignee := range task.Assignees {
		names = append(names, assignee.Name)
	}
	add("Assignees", strings.Join(names, ", "), true)
	if task.DateDeadline != nil {
		add("Deadline", formatDeadline(*task.DateDeadline), true)
	}
	tags := make([]string, 0, len(task.Tags))
	for _, tag := range task.Tags {
		tags = append(tags, tag.Name)
	}
	add("Tags", strings.Join(tags, ", "), false)
	return attachment
}

// unavailableAttachment replaces the card of a task that was deleted or
// archived after the post.
func unavailableAttachment() *model.MessageAttachment {
	return &model.MessageAttachment{
		Title:    "This Odoo task is no longer available",
		Fallback: "This Odoo task is no longer available",
		Color:    "#a0a0a0",
		Footer:   cardFooter,
	}
}

func formatDeadline(value string) string {
	deadline, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return deadline.UTC().Format("Mon 2 Jan 2006 15:04 UTC")
}

// cardsFor computes the cards of a post as its author sees them. previous
// lists the tasks that already had a card: such a task that is now deleted
// or archived keeps a card that says so. ok is false when Odoo could not be
// reached, so the caller can keep the cards that the post has.
func (p *Plugin) cardsFor(ctx context.Context, post *model.Post, previous []int) (attachments []*model.MessageAttachment, taskIDs []int, ok bool) {
	client := p.odooClient()
	if client == nil {
		return nil, nil, false
	}
	refs := odoo.FindTaskLinks(post.Message, p.getConfiguration().odooHost())
	if len(refs) == 0 {
		return nil, nil, true
	}
	subject := p.pocketIDSubject(post.UserId)
	if subject == "" {
		return nil, nil, true
	}
	shown := map[int]bool{}
	for _, id := range previous {
		shown[id] = true
	}
	for _, ref := range refs {
		result, err := client.ReadTask(ctx, subject, post.ChannelId, ref)
		if err != nil {
			p.API.LogWarn("Odoo task read failed", "task", ref.ID, "err", err.Error())
			return nil, nil, false
		}
		switch {
		case result.Visible && result.Task != nil:
			attachments = append(attachments, taskAttachment(result.Task))
			taskIDs = append(taskIDs, result.Task.ID)
		case shown[ref.ID] && (result.Reason == "archived" || result.Reason == "not_found"):
			attachments = append(attachments, unavailableAttachment())
			taskIDs = append(taskIDs, ref.ID)
		}
	}
	return attachments, taskIDs, true
}

// applyCards puts the Odoo cards on a copy of the post, and keeps the
// attachments that are not Odoo cards.
func applyCards(post *model.Post, cards []*model.MessageAttachment, taskIDs []int) *model.Post {
	updated := post.Clone()
	var kept []*model.MessageAttachment
	for _, attachment := range post.Attachments() {
		if attachment.Footer != cardFooter {
			kept = append(kept, attachment)
		}
	}
	all := append(kept, cards...)
	if len(all) == 0 {
		updated.DelProp(model.PostPropsAttachments)
	} else {
		model.ParseMessageAttachment(updated, all)
	}
	if len(taskIDs) == 0 {
		updated.DelProp(propTaskIDs)
	} else {
		updated.AddProp(propTaskIDs, taskIDs)
	}
	return updated
}

// withCardsOf returns a copy of post with the cards of source: the Odoo card
// attachments and propTaskIDs. With a nil source, the copy has no cards.
// A client can send these values, so the hooks always replace them with the
// values that this plugin wrote.
func withCardsOf(post, source *model.Post) *model.Post {
	var cards []*model.MessageAttachment
	var taskIDs []int
	if source != nil {
		for _, attachment := range source.Attachments() {
			if attachment.Footer == cardFooter {
				cards = append(cards, attachment)
			}
		}
		taskIDs = taskIDsOf(source)
	}
	if source == nil && post.GetProp(propTaskIDs) == nil && !hasCard(post) {
		return post
	}
	return applyCards(post, cards, taskIDs)
}

func hasCard(post *model.Post) bool {
	for _, attachment := range post.Attachments() {
		if attachment.Footer == cardFooter {
			return true
		}
	}
	return false
}

// taskIDsOf reads propTaskIDs. After a database round trip the numbers are
// float64 values in a []any.
func taskIDsOf(post *model.Post) []int {
	if post == nil {
		return nil
	}
	var ids []int
	switch values := post.GetProp(propTaskIDs).(type) {
	case []int:
		ids = append(ids, values...)
	case []any:
		for _, value := range values {
			if number, ok := value.(float64); ok && number > 0 {
				ids = append(ids, int(number))
			}
		}
	}
	return ids
}

// handlesPost is false for posts that never get cards. A post with
// attachments, cards included, has the type slack_attachment.
func handlesPost(post *model.Post) bool {
	return (post.Type == "" || post.Type == model.PostTypeMessageAttachment) && post.GetProp(model.PostPropsFromWebhook) == nil && post.GetProp(model.PostPropsFromBot) == nil
}
