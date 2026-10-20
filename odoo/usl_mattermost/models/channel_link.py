from markupsafe import Markup

from odoo import fields, models


class UslMattermostChannelLink(models.Model):
    """A Mattermost channel that may see the tasks of a project.

    A linked channel gets task cards whatever the visibility of the
    project. The plugin creates links with /odoo link; a project manager
    can remove them on the project form.
    """

    _name = "usl.mattermost.channel.link"
    _description = "Mattermost channel linked to a project"
    _order = "project_id, channel_display_name, id"
    _rec_name = "channel_display_name"

    project_id = fields.Many2one(
        "project.project",
        required=True,
        index=True,
        ondelete="cascade",
    )
    channel_id = fields.Char(
        string="Channel ID",
        required=True,
        index=True,
        help="The Mattermost channel id. It does not change when the channel is renamed.",
    )
    channel_name = fields.Char(help="The channel's name when it was linked.")
    channel_display_name = fields.Char(
        string="Channel",
        help="The channel's display name when it was linked.",
    )
    team_name = fields.Char(string="Team")
    linked_by_id = fields.Many2one("res.users", string="Linked by", readonly=True)

    _unique_project_channel = models.Constraint(
        "UNIQUE(project_id, channel_id)",
        "This channel is already linked to this project.",
    )

    def _usl_post_note(self, text, author):
        for link in self:
            name = link.channel_display_name or link.channel_name or link.channel_id
            link.project_id.sudo().message_post(
                body=Markup("%s <b>%s</b>") % (text, name),
                author_id=author.partner_id.id,
                message_type="comment",
                subtype_xmlid="mail.mt_note",
            )

    def unlink(self):
        self._usl_post_note(self.env._("Mattermost channel unlinked:"), self.env.user)
        projects = self.project_id
        result = super().unlink()
        # The cards in the channel may now be hidden, so the plugin reads
        # them again.
        self.env["usl.mattermost.card"]._usl_mark_projects_changed(projects)
        return result
