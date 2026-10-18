# API between the plugin and Odoo, version 1

The Mattermost plugin (`com.unstaticlabs.odoo`) and the Odoo module
(`usl_mattermost`) talk only through this API. Change it here first, then in
both parts.

## Rules for every request

- JSON over HTTPS.
- Every body carries `"v": 1`. Each side accepts the current version and the
  one before it, so the plugin and the module can deploy in either order.
- Every request carries a signature: HMAC-SHA256 over the method, the path,
  the timestamp, and the body, with the key of its direction (below).
- The receiver refuses a timestamp more than 5 minutes old or in the future.

| Direction | Key | Holders |
|---|---|---|
| Plugin → Odoo | plugin signing key | plugin settings; Odoo file secret `/opt/usl-odoo/secrets/mattermost-plugin-key.secret` |
| Odoo → plugin | Odoo webhook key | Odoo file secret `/opt/usl-odoo/secrets/mattermost-webhook-key.secret`; plugin settings |

Both keys are tracked in rotator. During a rotation, each side accepts the
current and the previous key.

## The user

A request that acts for a person names them by their Pocket ID user id: the
Mattermost user's `authdata`, for a user whose `authservice` is `openid`. The
module finds the active Odoo user whose `res.users.oauth_uid` is that id, with
the Pocket ID provider, and runs the request as that user (`with_user`). It
never matches by email: Odoo logins are not always emails.

A Mattermost user who is not `openid`, or who has no matching Odoo user, gets
no card and no action.

## Endpoints

### Plugin → Odoo

| Endpoint | Purpose | Runs as |
|---|---|---|
| `POST /usl/mattermost/task/read` | The card data for one task in one channel | the user who posted the link |
| `POST /usl/mattermost/task/watch` | Records that a post shows a task, so Odoo can ask the plugin to refresh it | the user who posted the link |
| `POST /usl/mattermost/link` | Adds or removes a link between a project and a channel | the user who typed the command |
| `POST /usl/mattermost/links` | The projects linked to a channel | the user who typed the command |
| `GET /usl/mattermost/health` | Version and status | no user |

### Odoo → plugin

| Endpoint | Purpose |
|---|---|
| `POST /plugins/com.unstaticlabs.odoo/odoo/task-changed` | "Task N changed": the plugin reads it again, as each card's poster, and updates the cards |

## When a channel may see a task

`task/read` returns the task only if all of these are true:

1. The poster may read the task in Odoo.
2. The task's project is linked to the channel, or the project's visibility is
   "All internal users" (`employees`) or "All internal users and invited
   portal users" (`portal`).

Otherwise it answers "not visible", and the link stays a plain link.

## Channel links

- Many to many: a project can have many channels, and a channel many projects.
- Odoo stores them, with the Mattermost channel id, the channel name at link
  time, who linked it, and when.
- Adding a link needs both: the plugin checks that the user is a member of
  the channel, and Odoo checks that the user may edit the project.
- Removing a link needs one of the two.
- Each change posts a note in the project's chatter.

## Task links the plugin recognises

On the Odoo host in the plugin settings (`odoo.unstaticlabs.com`), the task id
is the last number in the link:

| Link form | Example |
|---|---|
| `/odoo/project/<project id>/tasks/<task id>` | `/odoo/project/18/tasks/3278` |
| `/odoo/project.task/<task id>` | `/odoo/project.task/3278` |
| `/odoo/tasks/<id>`, `/odoo/my-tasks/<id>`, `/odoo/all-tasks/<id>` | `/odoo/my-tasks/3278` |
| `/odoo/action-<action>/<task id>` | checked in Odoo, because the name alone does not show that it opens a task |
| `/my/tasks/<task id>` | `/my/tasks/3278` |
| `/mail/view?model=project.task&res_id=<task id>` | from notification emails |
| `/web#id=<task id>&model=project.task` | legacy links |

Query strings such as `?debug=1` are ignored.

## Request and response bodies

Not written yet. Define each body here before you implement the endpoint.
