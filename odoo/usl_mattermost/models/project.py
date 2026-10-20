from odoo import fields, models

# A change to one of these project fields can hide its tasks from a channel.
PROJECT_ACCESS_FIELDS = {"privacy_visibility", "active", "company_id"}

# The task fields that a card shows. A change to one of them refreshes the
# cards of the task.
CARD_FIELDS = {
    "name",
    "stage_id",
    "state",
    "user_ids",
    "date_deadline",
    "tag_ids",
    "priority",
    "color",
    "project_id",
    "active",
}


class ProjectProject(models.Model):
    _inherit = "project.project"

    usl_mattermost_link_ids = fields.One2many(
        "usl.mattermost.channel.link",
        "project_id",
        string="Mattermost channels",
        # The links name channels and teams; only project managers read them.
        groups="project.group_project_manager",
    )


    def write(self, vals):
        result = super().write(vals)
        if PROJECT_ACCESS_FIELDS.intersection(vals):
            self.env["usl.mattermost.card"]._usl_mark_projects_changed(self)
        return result

    def message_unsubscribe(self, partner_ids=None):
        result = super().message_unsubscribe(partner_ids=partner_ids)
        self.env["usl.mattermost.card"]._usl_mark_projects_changed(self)
        return result


class ProjectTask(models.Model):
    _inherit = "project.task"

    def write(self, vals):
        result = super().write(vals)
        if CARD_FIELDS.intersection(vals):
            self.env["usl.mattermost.card"]._usl_mark_changed(self.ids)
        return result

    def unlink(self):
        task_ids = self.ids
        result = super().unlink()
        self.env["usl.mattermost.card"]._usl_mark_changed(task_ids)
        return result

    def message_unsubscribe(self, partner_ids=None):
        result = super().message_unsubscribe(partner_ids=partner_ids)
        self.env["usl.mattermost.card"]._usl_mark_changed(self.ids)
        return result


class ResUsers(models.Model):
    _inherit = "res.users"

    def write(self, vals):
        result = super().write(vals)
        # A card shows a task with the rights of the post's author. When a
        # user loses rights, the plugin reads every card again.
        if vals.get("active") is False or "group_ids" in vals or "share" in vals:
            self.env["usl.mattermost.card"]._usl_mark_all_changed()
        return result
