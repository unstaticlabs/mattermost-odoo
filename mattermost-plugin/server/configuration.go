package main

import (
	"net/url"
	"os"
	"reflect"
	"strings"

	"github.com/pkg/errors"
)

// configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration, as well as values computed from the configuration. Any public fields will be
// deserialized from the Mattermost server configuration in OnConfigurationChange.
//
// As plugins are inherently concurrent (hooks being called asynchronously), and the plugin
// configuration can change at any time, access to the configuration must be synchronized. The
// strategy used in this plugin is to guard a pointer to the configuration, and clone the entire
// struct whenever it changes. You may replace this with whatever strategy you choose.
//
// If you add non-reference types to your configuration struct, be sure to rewrite Clone as a deep
// copy appropriate for your types.
type configuration struct {
	// OdooURL is the Odoo base URL. Task links on its host get cards, and
	// the plugin calls the usl_mattermost API there.
	OdooURL string
	// PluginKey signs the plugin's requests to Odoo (docs/api.md).
	PluginKey string
	// WebhookKey checks Odoo's "task changed" requests.
	WebhookKey string
	// PreviousWebhookKey is accepted too, during a rotation of WebhookKey.
	PreviousWebhookKey string
}

// Environment variables win over the settings. The Mattermost server passes
// its environment to the plugin process, so the gitops compose can set the
// keys from Komodo variables that rotator writes.
const (
	envOdooURL            = "USL_ODOO_URL"
	envPluginKey          = "USL_ODOO_PLUGIN_KEY"
	envWebhookKey         = "USL_ODOO_WEBHOOK_KEY"
	envPreviousWebhookKey = "USL_ODOO_PREVIOUS_WEBHOOK_KEY"
)

func fromEnv(name, setting string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return strings.TrimSpace(setting)
}

func (c *configuration) odooURL() string   { return fromEnv(envOdooURL, c.OdooURL) }
func (c *configuration) pluginKey() string { return fromEnv(envPluginKey, c.PluginKey) }

// odooHost is the host of the Odoo URL, or "" when it is not a URL.
func (c *configuration) odooHost() string {
	link, err := url.Parse(c.odooURL())
	if err != nil || link.Scheme == "" {
		return ""
	}
	return link.Hostname()
}

// ready is true when the plugin can call Odoo.
func (c *configuration) ready() bool {
	return c.odooHost() != "" && len(c.pluginKey()) >= minKeyLength
}

// webhookKeys are the keys accepted for Odoo's requests.
func (c *configuration) webhookKeys() []string {
	var keys []string
	for _, key := range []string{fromEnv(envWebhookKey, c.WebhookKey), fromEnv(envPreviousWebhookKey, c.PreviousWebhookKey)} {
		if len(key) >= minKeyLength {
			keys = append(keys, key)
		}
	}
	return keys
}

// minKeyLength matches the Odoo module's minimum key length.
const minKeyLength = 32

// Clone shallow copies the configuration. Your implementation may require a deep copy if
// your configuration has reference types.
func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

// getConfiguration retrieves the active configuration under lock, making it safe to use
// concurrently. The active configuration may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
//
// Do not call setConfiguration while holding the configurationLock, as sync.Mutex is not
// reentrant. In particular, avoid using the plugin API entirely, as this may in turn trigger a
// hook back into the plugin. If that hook attempts to acquire this lock, a deadlock may occur.
//
// This method panics if setConfiguration is called with the existing configuration. This almost
// certainly means that the configuration was modified without being cloned and may result in
// an unsafe access.
func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		// Ignore assignment if the configuration struct is empty. Go will optimize the
		// allocation for same to point at the same memory address, breaking the check
		// above.
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}

		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	configuration := new(configuration)

	// Load the public configuration fields from the Mattermost server configuration.
	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	p.setConfiguration(configuration)

	return nil
}
