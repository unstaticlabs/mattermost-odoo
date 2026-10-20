package main

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"

	"github.com/unstaticlabs/mattermost-odoo/mattermost-plugin/server/odoo"
)

// Plugin shows Odoo task links as cards. See the README of this repository.
type Plugin struct {
	plugin.MattermostPlugin

	client *pluginapi.Client
	router *mux.Router
	botID  string
	seen   signatureCache

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration
}

// OnActivate is invoked when the plugin is activated. If an error is returned, the plugin will be deactivated.
func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)
	p.router = p.initRouter()

	botID, err := p.client.Bot.EnsureBot(&model.Bot{
		Username:    "odoo",
		DisplayName: "Odoo",
		Description: "Posts the changes to Odoo project links in channels.",
	})
	if err != nil {
		return errors.Wrap(err, "failed to ensure the Odoo bot")
	}
	p.botID = botID

	if err := p.API.RegisterCommand(odooCommand()); err != nil {
		return errors.Wrap(err, "failed to register the /odoo command")
	}
	return nil
}

// odooClient returns a client for the current settings, or nil when the
// settings are not complete.
func (p *Plugin) odooClient() *odoo.Client {
	config := p.getConfiguration()
	if !config.ready() {
		return nil
	}
	return &odoo.Client{
		BaseURL: config.odooURL(),
		Key:     config.pluginKey(),
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

// pocketIDSubject is the Pocket ID user id of a Mattermost user, or "" when
// the user does not log in with Pocket ID.
func (p *Plugin) pocketIDSubject(userID string) string {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil || user.IsBot || user.AuthService != model.ServiceOpenid || user.AuthData == nil {
		return ""
	}
	return *user.AuthData
}
