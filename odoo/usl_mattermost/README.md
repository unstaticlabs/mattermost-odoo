# USL Mattermost

The Odoo side of the Mattermost integration. It gives the Mattermost plugin
of this repository a signed API to read Odoo tasks as the Mattermost user, and
it stores the links between Odoo projects and Mattermost channels.

The API is in `../../docs/api.md`. The design is the document "Mattermost and
Odoo integration: design".

The API methods of `usl.mattermost.service` are private. Odoo RPC refuses
them, so only the signed HTTP endpoints can call them.

## Dependencies

- `project`: the tasks and projects.
- `usl_pocketid`: the Pocket ID login. It stores the Pocket ID user id in
  `res.users.oauth_uid`, which is how this module finds the Odoo user for a
  Mattermost user.
