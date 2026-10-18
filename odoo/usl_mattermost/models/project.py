from odoo import fields, models

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
    )


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
