"""The rules of the Mattermost API (docs/api.md), without HTTP.

Every method takes the Pocket ID user id that the plugin sent, finds the
Odoo user, and checks rights as that user. The service itself runs as
superuser only to look things up and to write its own records.
"""

from odoo import api, models

# Project visibilities that every internal user may see, so any channel
# may get their task cards without a link.
OPEN_VISIBILITIES = ("employees", "portal")
CANDIDATES_LIMIT = 10


def _utc(value):
    if not value:
        return None
    return value.replace(tzinfo=None).isoformat(timespec="seconds") + "Z"


class UslMattermostService(models.AbstractModel):
    _name = "usl.mattermost.service"
    _description = "Rules of the Mattermost API"

    # --- users ---------------------------------------------------------

    @api.model
    def _user_for_subject(self, subject):
        """The active internal user whose Pocket ID identity is `subject`."""
        Users = self.env["res.users"]
        if not subject or not isinstance(subject, str):
            return Users
        identities = self.env["usl.oidc.identity"].sudo().search(
            [
                ("subject", "=", subject),
                ("active", "=", True),
                ("provider_id.usl_pocketid", "=", True),
                ("provider_id.enabled", "=", True),
            ],
            limit=2,
        )
        if len(identities) != 1:
            return Users
        user = identities.user_id
        if not user.active or user.share:
            return Users
        return user

    # --- tasks ---------------------------------------------------------

    @api.model
    def _resolve_task(self, task_ref):
        Task = self.env["project.task"].sudo().with_context(active_test=False)
        task_id = task_ref.get("id")
        action = task_ref.get("action")
        if action:
            act = self._task_action(action)
            if not act:
                return Task
        return Task.browse(task_id).exists()

    @api.model
    def _task_action(self, action):
        """The window action named by an /odoo/action-<action> link, if it opens tasks."""
        Actions = self.env["ir.actions.act_window"].sudo()
        if action.isdigit():
            act = Actions.browse(int(action)).exists()
        else:
            act = self.env.ref(action, raise_if_not_found=False)
            if not act or act._name != "ir.actions.act_window":
                return Actions
        return act if act.res_model == "project.task" else Actions

    @api.model
    def _hidden_reason(self, user, task, channel_id):
        """None when the channel may see the task, else the reason why not."""
        if not task:
            return "not_found"
        if not task.active:
            return "archived"
        if not task.with_user(user).has_access("read"):
            return "no_access"
        project = task.project_id
        if not project:
            return "not_shared"
        if project.privacy_visibility in OPEN_VISIBILITIES:
            return None
        if self._is_linked(project, channel_id):
            return None
        return "not_shared"

    @api.model
    def _is_linked(self, project, channel_id):
        return bool(
            self.env["usl.mattermost.channel.link"].sudo().search_count(
                [("project_id", "=", project.id), ("channel_id", "=", channel_id)],
                limit=1,
            ),
        )

    @api.model
    def _task_card(self, task):
        task = task.sudo()
        base_url = self.env["ir.config_parameter"].sudo().get_str("web.base.url", "")
        project = task.project_id
        if project:
            url = f"{base_url}/odoo/project/{project.id}/tasks/{task.id}"
        else:
            url = f"{base_url}/odoo/project.task/{task.id}"
        return {
            "id": task.id,
            "name": task.name,
            "url": url,
            "project": {"id": project.id, "name": project.name} if project else None,
            "stage": (
                {"id": task.stage_id.id, "name": task.stage_id.name, "folded": task.stage_id.fold}
                if task.stage_id
                else None
            ),
            "state": task.state,
            "assignees": [{"id": user.id, "name": user.name} for user in task.user_ids],
            "date_deadline": _utc(task.date_deadline),
            "tags": [
                {"id": tag.id, "name": tag.name, "color": tag.color} for tag in task.tag_ids
            ],
            "priority": task.priority,
            "color": task.color,
            "write_date": _utc(task.write_date),
        }

    @api.model
    def read_task(self, subject, channel_id, task_ref):
        user = self._user_for_subject(subject)
        if not user:
            return {"ok": True, "visible": False, "reason": "no_user"}
        task = self._resolve_task(task_ref)
        reason = self._hidden_reason(user, task, channel_id)
        if reason:
            return {"ok": True, "visible": False, "reason": reason}
        return {"ok": True, "visible": True, "task": self._task_card(task)}

    @api.model
    def watch(self, subject, op, channel_id, post_id, task_id):
        Cards = self.env["usl.mattermost.card"].sudo()
        existing = Cards.search([("post_id", "=", post_id), ("task_id", "=", task_id)])
        if op == "remove":
            existing.unlink()
            return {"ok": True, "watching": False}
        user = self._user_for_subject(subject)
        if not user:
            return {"ok": True, "watching": False, "reason": "no_user"}
        task = self._resolve_task({"id": task_id})
        reason = self._hidden_reason(user, task, channel_id)
        if reason:
            return {"ok": True, "watching": False, "reason": reason}
        if not existing:
            Cards.create({"task_id": task.id, "post_id": post_id, "channel_id": channel_id})
        return {"ok": True, "watching": True}

    # --- channel links -------------------------------------------------

    @api.model
    def _find_project(self, user, query):
        """(project, error, candidates) for a project id or name, as `user` may read."""
        Projects = self.env["project.project"].with_user(user)
        if isinstance(query, int) or (isinstance(query, str) and query.strip().isdigit()):
            project = Projects.browse(int(query)).exists()
            if project and project.has_access("read"):
                return project, None, []
            return Projects, "project_not_found", []
        text = query.strip()
        matches = Projects.search([("name", "ilike", text)], limit=50)
        exact = matches.filtered(lambda p: (p.name or "").lower() == text.lower())
        found = exact or matches
        if len(found) == 1:
            return found, None, []
        if not found:
            return Projects, "project_not_found", []
        candidates = [{"id": p.id, "name": p.name} for p in found[:CANDIDATES_LIMIT]]
        return Projects, "project_ambiguous", candidates

    @api.model
    def link(self, subject, op, channel, channel_member, project_query):
        user = self._user_for_subject(subject)
        if not user:
            return {"ok": False, "error": "no_user"}
        project, error, candidates = self._find_project(user, project_query)
        if error:
            answer = {"ok": False, "error": error}
            if candidates:
                answer["candidates"] = candidates
            return answer
        can_edit = project.has_access("write")
        allowed = (channel_member and can_edit) if op == "add" else (channel_member or can_edit)
        if not allowed:
            return {"ok": False, "error": "forbidden"}
        project = project.sudo()
        Links = self.env["usl.mattermost.channel.link"].sudo()
        existing = Links.search(
            [("project_id", "=", project.id), ("channel_id", "=", channel["id"])],
        )
        changed = False
        if op == "add" and not existing:
            link = Links.create(
                {
                    "project_id": project.id,
                    "channel_id": channel["id"],
                    "channel_name": channel.get("name"),
                    "channel_display_name": channel.get("display_name"),
                    "team_name": channel.get("team_name"),
                    "linked_by_id": user.id,
                },
            )
            link._usl_post_note(self.env._("Mattermost channel linked:"), user)
            changed = True
        elif op == "remove" and existing:
            existing.with_user(user).sudo().unlink()
            changed = True
        return {
            "ok": True,
            "changed": changed,
            "project": {"id": project.id, "name": project.name},
        }

    @api.model
    def links(self, subject, channel_id):
        user = self._user_for_subject(subject)
        if not user:
            return {"ok": False, "error": "no_user"}
        links = self.env["usl.mattermost.channel.link"].sudo().search(
            [("channel_id", "=", channel_id)],
        )
        readable = links.filtered(lambda link: link.project_id.with_user(user).has_access("read"))
        return {
            "ok": True,
            "projects": [
                {
                    "id": link.project_id.id,
                    "name": link.project_id.name,
                    "linked_by": link.linked_by_id.name or None,
                    "linked_at": _utc(link.create_date),
                }
                for link in readable
            ],
            "hidden": len(links) - len(readable),
        }
