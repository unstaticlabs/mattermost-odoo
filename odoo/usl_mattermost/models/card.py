import json
import logging
import time
from collections import defaultdict

import requests

from odoo import api, fields, models

from .. import signing

_logger = logging.getLogger(__name__)

PLUGIN_ID = "com.unstaticlabs.odoo"
TASK_CHANGED_PATH = f"/plugins/{PLUGIN_ID}/odoo/task-changed"
MATTERMOST_URL_PARAMETER = "usl_mattermost.mattermost_url"
MAX_ATTEMPTS = 50
TASKS_PER_RUN = 100


class UslMattermostCard(models.Model):
    """A Mattermost post that shows an Odoo task card.

    The task is stored as a plain id, not a relation, so the record
    survives the task's deletion and the plugin still learns that the
    card must be emptied.
    """

    _name = "usl.mattermost.card"
    _description = "Mattermost post that shows a task card"
    _order = "id"

    task_id = fields.Integer(string="Task ID", required=True, index=True)
    post_id = fields.Char(string="Post ID", required=True, index=True)
    channel_id = fields.Char(string="Channel ID", required=True)
    needs_push = fields.Boolean(index=True, default=False)
    push_attempts = fields.Integer(default=0)
    last_push_error = fields.Char()
    last_push_at = fields.Datetime()

    _unique_post_task = models.Constraint(
        "UNIQUE(post_id, task_id)",
        "This post already shows this task.",
    )

    @api.model
    def _usl_mark_changed(self, task_ids):
        cards = self.sudo().search([("task_id", "in", list(task_ids))])
        if cards:
            cards.write({"needs_push": True})
            self.env.ref("usl_mattermost.ir_cron_push_cards").sudo()._trigger()

    @api.model
    def _cron_push(self):
        url = (
            self.env["ir.config_parameter"].sudo().get_str(MATTERMOST_URL_PARAMETER)
            or ""
        ).rstrip("/")
        keys = signing.load_keys(signing.WEBHOOK_KEY_ENV, signing.WEBHOOK_KEY_DEFAULT)
        if not url or not keys:
            return
        pending = self.sudo().search([("needs_push", "=", True)])
        by_task = defaultdict(lambda: self.browse())
        for card in pending:
            by_task[card.task_id] |= card
        for task_id in sorted(by_task)[:TASKS_PER_RUN]:
            self._usl_push_task(url, keys[0], task_id, by_task[task_id])

    def _usl_push_task(self, url, key, task_id, cards):
        body = json.dumps(
            {
                "v": 1,
                "task_id": task_id,
                "posts": [
                    {"post_id": card.post_id, "channel_id": card.channel_id}
                    for card in cards
                ],
            },
            separators=(",", ":"),
        ).encode()
        timestamp = int(time.time())
        headers = {
            "Content-Type": "application/json",
            "X-USL-Timestamp": str(timestamp),
            "X-USL-Signature": signing.signature(
                key,
                timestamp,
                "POST",
                TASK_CHANGED_PATH,
                body,
            ),
        }
        error = False
        try:
            response = requests.post(
                url + TASK_CHANGED_PATH,
                data=body,
                headers=headers,
                timeout=10,
            )
            if response.status_code != 200:
                error = f"HTTP {response.status_code}"
        except requests.RequestException as exc:
            error = type(exc).__name__
        now = fields.Datetime.now()
        if not error:
            cards.write(
                {
                    "needs_push": False,
                    "push_attempts": 0,
                    "last_push_error": False,
                    "last_push_at": now,
                },
            )
            return
        _logger.warning("Task %s: the Mattermost refresh failed: %s", task_id, error)
        for card in cards:
            attempts = card.push_attempts + 1
            card.write(
                {
                    "push_attempts": attempts,
                    "last_push_error": error,
                    "last_push_at": now,
                    "needs_push": attempts < MAX_ATTEMPTS,
                },
            )
