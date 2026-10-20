package main

import (
	"context"
	"slices"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

// MessageWillBePosted adds the cards before the post is saved, so a new post
// is not shown as edited. Only a post that its author sends in their own
// session gets cards: a post that a plugin or an admin makes for a user
// must not read Odoo with that user's rights.
func (p *Plugin) MessageWillBePosted(c *plugin.Context, post *model.Post) (*model.Post, string) {
	if !handlesPost(post) {
		return post, ""
	}
	post = withCardsOf(post, nil)
	if p.sessionUserID(c) != post.UserId {
		return post, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cards, taskIDs, ok := p.cardsFor(ctx, post, nil)
	if !ok || len(taskIDs) == 0 {
		return post, ""
	}
	return applyCards(post, cards, taskIDs), ""
}

// MessageWillBeUpdated recomputes the cards when the author edits the text.
// A person who edits a post never sets its cards: the cards of the old post
// stay, and only an edit by the author in their own session recomputes them.
// An update without a session comes from the server or a plugin, for
// example the card refresh of this plugin, and stays as it is.
func (p *Plugin) MessageWillBeUpdated(c *plugin.Context, newPost, oldPost *model.Post) (*model.Post, string) {
	editor := p.sessionUserID(c)
	if editor == "" || !handlesPost(newPost) {
		return newPost, ""
	}
	restored := withCardsOf(newPost, oldPost)
	if newPost.Message == oldPost.Message || editor != oldPost.UserId {
		return restored, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cards, taskIDs, ok := p.cardsFor(ctx, restored, taskIDsOf(oldPost))
	if !ok {
		return restored, ""
	}
	return applyCards(restored, cards, taskIDs), ""
}

// sessionUserID is the user of the session that made the change, or "" when
// no user session made it.
func (p *Plugin) sessionUserID(c *plugin.Context) string {
	if c == nil || c.SessionId == "" {
		return ""
	}
	session, appErr := p.API.GetSession(c.SessionId)
	if appErr != nil || session == nil {
		return ""
	}
	return session.UserId
}

// MessageHasBeenPosted asks Odoo to report changes to the tasks on the post.
func (p *Plugin) MessageHasBeenPosted(_ *plugin.Context, post *model.Post) {
	p.syncWatches(post, nil, taskIDsOf(post))
}

// MessageHasBeenUpdated follows the tasks added to or removed from the post.
func (p *Plugin) MessageHasBeenUpdated(_ *plugin.Context, newPost, oldPost *model.Post) {
	p.syncWatches(newPost, taskIDsOf(oldPost), taskIDsOf(newPost))
}

// MessageHasBeenDeleted stops the Odoo reports for the post.
func (p *Plugin) MessageHasBeenDeleted(_ *plugin.Context, post *model.Post) {
	p.syncWatches(post, taskIDsOf(post), nil)
}

func (p *Plugin) syncWatches(post *model.Post, before, after []int) {
	if len(before) == 0 && len(after) == 0 {
		return
	}
	client := p.odooClient()
	if client == nil {
		return
	}
	subject := p.pocketIDSubject(post.UserId)
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout*2)
	defer cancel()
	for _, id := range before {
		if !slices.Contains(after, id) {
			if _, err := client.Watch(ctx, subject, "remove", post.ChannelId, post.Id, id); err != nil {
				p.API.LogWarn("Odoo watch remove failed", "task", id, "err", err.Error())
			}
		}
	}
	if subject == "" {
		return
	}
	for _, id := range after {
		if !slices.Contains(before, id) {
			if _, err := client.Watch(ctx, subject, "add", post.ChannelId, post.Id, id); err != nil {
				p.API.LogWarn("Odoo watch add failed", "task", id, "err", err.Error())
			}
		}
	}
}
