# API between the plugin and Odoo, version 1

The Mattermost plugin (`com.unstaticlabs.odoo`) and the Odoo module
(`usl_mattermost`) talk only through this API. Change it here first, then in
both parts.

## Rules for every request

- JSON over HTTPS.
- Every body carries `"v": 1`. Version 1 is the only version, so each side
  accepts only `1`. A later version must keep the one before it for one
  release, so the plugin and the module can deploy in either order.
- Every plugin body also carries `"rid"`: a random request id (24 hex
  characters). It makes every signature unique.
- Every request carries two headers:
  - `X-USL-Timestamp`: the time in Unix seconds.
  - `X-USL-Signature`: `v1=` and the lowercase hex HMAC-SHA256, with the key
    of its direction (below), over these bytes:
    `<timestamp>\n<METHOD>\n<path>\n<body>`. A `GET` has an empty body.
- The receiver refuses:
  - a timestamp more than 5 minutes old or in the future;
  - a body above 64 KiB, before it reads the body when the size is declared;
  - a query string, because the signature does not cover it;
  - a signature that it accepted before (`replayed`). Odoo checks this for
    every `POST`, and the plugin for `task-changed`.

| Direction | Key | Holders |
|---|---|---|
| Plugin → Odoo | plugin signing key | plugin settings; Odoo file secret: on the host `/opt/usl-odoo/secrets/mattermost-plugin-key.secret`, in the container `/run/secrets/usl_mattermost_plugin_key` |
| Odoo → plugin | Odoo webhook key | Odoo file secret: on the host `/opt/usl-odoo/secrets/mattermost-webhook-key.secret`, in the container `/run/secrets/usl_mattermost_webhook_key`; plugin settings |

Both keys are tracked in rotator. During a rotation, each side accepts the
current and the previous key.

## The user

A request that acts for a person names them by their Pocket ID user id: the
Mattermost user's `authdata`, for a user whose `authservice` is `openid`. The
module finds the one active Pocket ID identity (`usl.oidc.identity`, module
`usl_pocketid`) with that subject on an enabled Pocket ID provider. Its user
must be active and internal. The module runs the request as that user
(`with_user`). It never matches by email: Odoo logins are not always emails.

The plugin sends a user only for a change that the user makes in their own
session. A post that a plugin or an admin makes or edits for another user
never reads Odoo with that user's rights: the old cards stay.

Trust: the plugin key acts for any Pocket ID user, and a Mattermost system
admin can change the `authdata` of any user. So a Mattermost system admin can
read Odoo as any user. Give the Mattermost system admin role only to people
who are Odoo administrators too. Every Mattermost server plugin runs with the
same rights as this plugin, so install only plugins that you trust.

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

## Errors

A protocol error is an HTTP 4xx or 5xx status with this body:

```json
{"v": 1, "ok": false, "error": "bad_signature", "message": "The signature does not match."}
```

| `error` | Status | Meaning |
|---|---|---|
| `bad_signature` | 401 | No signature, or it does not match any accepted key |
| `stale_timestamp` | 401 | The timestamp is missing, or more than 5 minutes away |
| `replayed` | 401 | The receiver accepted this signature before |
| `too_large` | 413 | The body is above 64 KiB |
| `bad_request` | 400 | The body is not valid JSON, a field is missing or has the wrong type, or the URL has a query string |
| `unsupported_version` | 400 | `v` is not a version this side accepts |
| `not_configured` | 503 | The receiver has no key for this direction |
| `internal_error` | 500 | The receiver failed; its log has the details |

A refusal that concerns the user, the task, or the project is a normal answer:
status 200 and `"ok": true` with `"visible": false`, or `"ok": false` with an
`error` from the endpoint's own list.

## Bodies

### `POST /usl/mattermost/task/read`

Request. `task` is either `{"id": <task id>}`, or `{"action": "<action>", "id": <record id>}`
for an `/odoo/action-<action>/<id>` link:

```json
{"v": 1, "user": "7f8d5bad-3b03-44c0-a758-fd351d7bc0d4", "channel_id": "kq7x3m9pa1b2c3d4e5f6g7h8i9", "task": {"id": 3278}}
```

Answer when the channel may see the task:

```json
{
  "v": 1, "ok": true, "visible": true,
  "task": {
    "id": 3278,
    "name": "Restore the Mattermost connector",
    "url": "https://odoo.unstaticlabs.com/odoo/project/18/tasks/3278",
    "project": {"id": 18, "name": "[USL] CIO"},
    "stage": {"id": 5, "name": "In progress", "folded": false},
    "state": "01_in_progress",
    "assignees": [{"id": 8, "name": "Roger"}],
    "date_deadline": "2026-10-20T16:00:00Z",
    "tags": [{"id": 3, "name": "infra", "color": 4}],
    "priority": "0",
    "color": 0,
    "write_date": "2026-10-18T09:12:44Z"
  }
}
```

`date_deadline` is `null` when the task has none. Times are UTC.

Answer when it may not:

```json
{"v": 1, "ok": true, "visible": false, "reason": "not_shared"}
```

| `reason` | Meaning |
|---|---|
| `no_user` | No active internal Odoo user has this Pocket ID user id |
| `not_found` | The task does not exist, or the action does not open tasks |
| `archived` | The task is archived, and the user may read it |
| `no_access` | The user may not read the task |
| `not_shared` | The user may read it, but the channel may not see it: no link, and the project is not visible to all internal users |

### `POST /usl/mattermost/task/watch`

Records, or forgets, that a post shows a task. Odoo then sends "task changed"
for that post. `op` is `add` or `remove`:

```json
{"v": 1, "user": "7f8d5bad-3b03-44c0-a758-fd351d7bc0d4", "op": "add", "channel_id": "kq7x3m9pa1b2c3d4e5f6g7h8i9", "post_id": "p4w8n2k6r0t5y9u3i7o1a5s8d2", "task_id": 3278}
```

```json
{"v": 1, "ok": true, "watching": true}
```

An `add` follows the same rule as `task/read`. When the channel may not see
the task, the answer is `"watching": false` with a `reason` from the
`task/read` table. A `remove` needs no visibility.

### `POST /usl/mattermost/link`

`op` is `add` or `remove`. `project` is a project id, or a name. `channel_member`
is what the plugin found: whether the user is a member of the channel.

```json
{
  "v": 1, "user": "7f8d5bad-3b03-44c0-a758-fd351d7bc0d4", "op": "add",
  "channel": {"id": "kq7x3m9pa1b2c3d4e5f6g7h8i9", "name": "kinkverse", "display_name": "Kinkverse", "team_name": "unstaticlabs"},
  "channel_member": true,
  "project": "Kinkverse"
}
```

```json
{"v": 1, "ok": true, "changed": true, "project": {"id": 21, "name": "Kinkverse"}}
```

`changed` is `false` when the link already existed (`add`) or did not exist
(`remove`).

```json
{"v": 1, "ok": false, "error": "project_ambiguous", "candidates": [{"id": 21, "name": "Kinkverse"}, {"id": 34, "name": "Kinkverse admin"}]}
```

| `error` | Meaning |
|---|---|
| `no_user` | No active internal Odoo user has this Pocket ID user id |
| `project_not_found` | No project that the user may read matches |
| `project_ambiguous` | Several match; `candidates` lists up to 10 |
| `forbidden` | `add` needs `channel_member` and the right to edit the project; `remove` needs one of the two |

A name matches a project exactly, case-insensitive. If none does, it matches
the projects whose name contains it.

### `POST /usl/mattermost/links`

```json
{"v": 1, "user": "7f8d5bad-3b03-44c0-a758-fd351d7bc0d4", "channel_id": "kq7x3m9pa1b2c3d4e5f6g7h8i9"}
```

```json
{"v": 1, "ok": true, "projects": [{"id": 21, "name": "Kinkverse", "linked_by": "Roger", "linked_at": "2026-10-18T10:00:00Z"}], "hidden": 0}
```

`projects` lists only the projects the user may read. `hidden` counts the
other links of the channel. Errors: `no_user`.

### `GET /usl/mattermost/health`

```json
{"v": 1, "ok": true, "module": "usl_mattermost", "version": "saas~19.3.0.1.0", "webhook_configured": true}
```

`webhook_configured` is `true` when Odoo has a webhook key and a Mattermost URL.

### `POST /plugins/com.unstaticlabs.odoo/odoo/task-changed`

Odoo sends it after a task changes in a field that a card shows: name, stage,
state, assignees, deadline, tags, priority, colour, project, or archive state.

```json
{"v": 1, "task_id": 3278, "posts": [{"post_id": "p4w8n2k6r0t5y9u3i7o1a5s8d2", "channel_id": "kq7x3m9pa1b2c3d4e5f6g7h8i9"}]}
```

```json
{"v": 1, "ok": true}
```

The plugin reads the task again with `task/read`, as each post's author, and
updates or empties each card. Odoo sends from a scheduled job, started right
after the change. A failed send stays queued and is tried again by the next
run.

## Odoo configuration

| Setting | Where | Example |
|---|---|---|
| Plugin signing key | File named by `USL_MATTERMOST_PLUGIN_KEY_FILE` (default `/run/secrets/usl_mattermost_plugin_key`) | 64 random characters |
| Odoo webhook key | File named by `USL_MATTERMOST_WEBHOOK_KEY_FILE` (default `/run/secrets/usl_mattermost_webhook_key`) | 64 random characters |
| Mattermost URL | System parameter `usl_mattermost.mattermost_url` | `https://chat.unstaticlabs.com` |

The Mattermost URL must be an `https` URL with no user, query, or fragment.
Odoo does not follow a redirect when it sends a task change.

Only project managers can read the channel links in Odoo, because a link
names a Mattermost channel and team. When a link goes away, or a project or
user loses access, Odoo sends the affected tasks again, so their cards hide.

A key file holds the current key on its first line, and optionally the
previous key on its second line, for a rotation. A key shorter than 32
characters is refused.
