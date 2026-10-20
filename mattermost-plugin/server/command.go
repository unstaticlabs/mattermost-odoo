package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/unstaticlabs/mattermost-odoo/mattermost-plugin/server/odoo"
)

const commandHelp = "**/odoo link <project>**: show the task cards of an Odoo project in this channel, whatever the project's visibility. You must be a member of the channel and be allowed to edit the project in Odoo.\n" +
	"**/odoo unlink <project>**: remove that link. You must be a member of the channel or be allowed to edit the project.\n" +
	"**/odoo links**: list the Odoo projects linked to this channel.\n" +
	"`<project>` is the project's name or its id."

func odooCommand() *model.Command {
	autocomplete := model.NewAutocompleteData("odoo", "[command]", "Link Odoo projects to this channel")
	link := model.NewAutocompleteData("link", "<project>", "Show an Odoo project's task cards in this channel")
	link.AddTextArgument("Project name or id", "<project>", "")
	unlink := model.NewAutocompleteData("unlink", "<project>", "Remove an Odoo project link from this channel")
	unlink.AddTextArgument("Project name or id", "<project>", "")
	autocomplete.AddCommand(link)
	autocomplete.AddCommand(unlink)
	autocomplete.AddCommand(model.NewAutocompleteData("links", "", "List the Odoo projects linked to this channel"))
	autocomplete.AddCommand(model.NewAutocompleteData("help", "", "Show the help"))
	return &model.Command{
		Trigger:          "odoo",
		DisplayName:      "Odoo",
		Description:      "Link Odoo projects to this channel",
		AutoComplete:     true,
		AutoCompleteDesc: "Link Odoo projects to this channel",
		AutoCompleteHint: "[link|unlink|links|help]",
		AutocompleteData: autocomplete,
	}
}

func ephemeral(text string) *model.CommandResponse {
	return &model.CommandResponse{ResponseType: model.CommandResponseTypeEphemeral, Text: text}
}

// ExecuteCommand runs /odoo.
func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	fields := strings.Fields(args.Command)
	action := ""
	if len(fields) > 1 {
		action = fields[1]
	}
	project := ""
	if len(fields) > 2 {
		project = strings.Join(fields[2:], " ")
	}

	switch action {
	case "link", "unlink":
		if project == "" {
			return ephemeral(fmt.Sprintf("Give a project: `/odoo %s <project name or id>`.", action)), nil
		}
		return ephemeral(p.changeLink(args, action, project)), nil
	case "links":
		return ephemeral(p.listLinks(args)), nil
	default:
		return ephemeral(commandHelp), nil
	}
}

// odooReady gives the client and the user's Pocket ID user id, or a message
// that says why the command cannot run.
func (p *Plugin) odooReady(userID string) (*odoo.Client, string, string) {
	client := p.odooClient()
	if client == nil {
		return nil, "", "The Odoo plugin is not configured yet. Ask a system admin."
	}
	subject := p.pocketIDSubject(userID)
	if subject == "" {
		return nil, "", "Your Mattermost account does not log in with Pocket ID, so Odoo cannot know who you are."
	}
	return client, subject, ""
}

func commandFailure(err error) string {
	var protocolError *odoo.ProtocolError
	if errors.As(err, &protocolError) && protocolError.Code == "not_configured" {
		return "Odoo has no key for this plugin yet. Ask a system admin."
	}
	return "Odoo did not answer. Try again later."
}

func (p *Plugin) changeLink(args *model.CommandArgs, action, project string) string {
	client, subject, problem := p.odooReady(args.UserId)
	if problem != "" {
		return problem
	}
	channel, appErr := p.API.GetChannel(args.ChannelId)
	if appErr != nil {
		return "This channel could not be read."
	}
	_, memberErr := p.API.GetChannelMember(args.ChannelId, args.UserId)
	info := odoo.Channel{ID: channel.Id, Name: channel.Name, DisplayName: channel.DisplayName}
	if team, teamErr := p.API.GetTeam(channel.TeamId); teamErr == nil {
		info.TeamName = team.Name
	}
	op := "add"
	if action == "unlink" {
		op = "remove"
	}
	var query any = project
	if id, ok := positiveID(project); ok {
		query = id
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.Link(ctx, subject, op, info, memberErr == nil, query)
	if err != nil {
		p.API.LogWarn("Odoo link failed", "err", err.Error())
		return commandFailure(err)
	}
	switch result.Error {
	case "":
	case "no_user":
		return "Odoo has no user for your Pocket ID account."
	case "project_not_found":
		return fmt.Sprintf("No Odoo project that you can see matches %q.", project)
	case "project_ambiguous":
		names := make([]string, 0, len(result.Candidates))
		for _, candidate := range result.Candidates {
			names = append(names, fmt.Sprintf("- %s (id %d)", candidate.Name, candidate.ID))
		}
		return fmt.Sprintf("Several Odoo projects match %q. Use the name or the id:\n%s", project, strings.Join(names, "\n"))
	case "forbidden":
		if op == "add" {
			return "To link a project, you must be a member of this channel and be allowed to edit the project in Odoo."
		}
		return "To unlink a project, you must be a member of this channel or be allowed to edit the project in Odoo."
	default:
		return "Odoo refused: " + result.Error
	}

	name := project
	if result.Project != nil {
		name = result.Project.Name
	}
	if !result.Changed {
		if op == "add" {
			return fmt.Sprintf("The Odoo project **%s** was already linked to this channel.", name)
		}
		return fmt.Sprintf("The Odoo project **%s** was not linked to this channel.", name)
	}
	p.announce(args, op, name)
	if op == "add" {
		return fmt.Sprintf("Linked the Odoo project **%s** to this channel.", name)
	}
	return fmt.Sprintf("Unlinked the Odoo project **%s** from this channel.", name)
}

// announce tells the channel about a change, because a link decides what
// its members can see.
func (p *Plugin) announce(args *model.CommandArgs, op, projectName string) {
	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil || p.botID == "" {
		return
	}
	text := fmt.Sprintf("@%s linked the Odoo project **%s** to this channel. Its task links now show cards here.", user.Username, projectName)
	if op == "remove" {
		text = fmt.Sprintf("@%s unlinked the Odoo project **%s** from this channel.", user.Username, projectName)
	}
	if _, appErr := p.API.CreatePost(&model.Post{UserId: p.botID, ChannelId: args.ChannelId, Message: text}); appErr != nil {
		p.API.LogWarn("Odoo link announcement failed", "err", appErr.Error())
	}
}

func (p *Plugin) listLinks(args *model.CommandArgs) string {
	client, subject, problem := p.odooReady(args.UserId)
	if problem != "" {
		return problem
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.Links(ctx, subject, args.ChannelId)
	if err != nil {
		p.API.LogWarn("Odoo links failed", "err", err.Error())
		return commandFailure(err)
	}
	if result.Error == "no_user" {
		return "Odoo has no user for your Pocket ID account."
	}
	if len(result.Projects) == 0 && result.Hidden == 0 {
		return "No Odoo project is linked to this channel."
	}
	lines := []string{"Odoo projects linked to this channel:"}
	for _, project := range result.Projects {
		line := fmt.Sprintf("- **%s** (id %d)", project.Name, project.ID)
		if project.LinkedBy != nil {
			line += ", linked by " + *project.LinkedBy
		}
		lines = append(lines, line)
	}
	if result.Hidden > 0 {
		lines = append(lines, fmt.Sprintf("- and %d project(s) that you cannot see in Odoo", result.Hidden))
	}
	return strings.Join(lines, "\n")
}

func positiveID(text string) (int, bool) {
	var id int
	if _, err := fmt.Sscanf(text, "%d", &id); err != nil || id <= 0 || fmt.Sprint(id) != text {
		return 0, false
	}
	return id, true
}
