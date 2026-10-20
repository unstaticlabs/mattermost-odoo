import json
import time
import uuid
from unittest.mock import Mock, patch

from odoo import Command
from odoo.exceptions import AccessError
from odoo.models import get_public_method
from odoo.tests import HttpCase, tagged

from .. import signing
from ..models import card as card_module

PLUGIN_KEY = "plugin-key-for-tests-" + "p" * 20
WEBHOOK_KEY = "webhook-key-for-tests-" + "w" * 20
CHANNEL = "kq7x3m9pa1b2c3d4e5f6g7h8i9"
OTHER_CHANNEL = "zz7x3m9pa1b2c3d4e5f6g7h8i9"


def _test_keys(env_var, _default):
    return [PLUGIN_KEY] if env_var == signing.PLUGIN_KEY_ENV else [WEBHOOK_KEY]


@tagged("post_install", "-at_install", "usl_mattermost")
class TestMattermostApi(HttpCase):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.provider = cls.env.ref("usl_pocketid.provider_pocketid")
        cls.provider._usl_pocketid_environment_write(
            {
                "enabled": True,
                "client_id": "odoo-client",
                "auth_endpoint": "https://id.example.test/authorize",
                "token_endpoint": "https://id.example.test/token",
                "jwks_uri": "https://id.example.test/jwks",
                "usl_oidc_issuer": "https://id.example.test",
                "usl_public_base_url": "https://odoo.example.test",
                "usl_required_group": "odoo-preprod",
            },
        )
        project_user = cls.env.ref("project.group_project_user")
        project_manager = cls.env.ref("project.group_project_manager")
        cls.manager = cls._user("manager", project_manager)
        cls.member = cls._user("member", project_user)
        cls.outsider = cls._user("outsider", project_user)
        Projects = cls.env["project.project"].with_context(mail_create_nolog=True)
        cls.open_project = Projects.create(
            {"name": "Open project", "privacy_visibility": "employees"},
        )
        cls.private_project = Projects.create(
            {"name": "Private project", "privacy_visibility": "followers"},
        )
        cls.private_project.message_subscribe(partner_ids=cls.member.partner_id.ids)
        Tasks = cls.env["project.task"].with_context(mail_create_nolog=True)
        cls.open_task = Tasks.create(
            {
                "name": "Open task",
                "project_id": cls.open_project.id,
                "user_ids": [Command.set(cls.member.ids)],
            },
        )
        cls.private_task = Tasks.create(
            {"name": "Private task", "project_id": cls.private_project.id},
        )

    @classmethod
    def _user(cls, name, group):
        user = cls.env["res.users"].with_context(no_reset_password=True).create(
            {
                "login": f"{name}@example.test",
                "name": name.capitalize(),
                "email": f"{name}@example.test",
                "company_id": cls.env.company.id,
                "company_ids": [Command.set(cls.env.company.ids)],
                "group_ids": [Command.set((cls.env.ref("base.group_user") | group).ids)],
                "usl_pocketid_access": True,
                "usl_identity_classification": "active",
            },
        )
        cls.env["usl.oidc.identity"].create(
            {
                "issuer": cls.provider.usl_oidc_issuer,
                "subject": f"subject-{name}",
                "provider_id": cls.provider.id,
                "user_id": user.id,
            },
        )
        return user

    def setUp(self):
        super().setUp()
        self.startPatcher(patch.object(signing, "load_keys", side_effect=_test_keys))

    # --- helpers --------------------------------------------------------

    def _post(self, path, payload, *, key=PLUGIN_KEY, timestamp=None, raw=None, query=""):
        if raw is None and isinstance(payload, dict) and "rid" not in payload:
            payload = {**payload, "rid": uuid.uuid4().hex}
        body = raw if raw is not None else json.dumps(payload).encode()
        timestamp = int(time.time()) if timestamp is None else timestamp
        headers = {
            "Content-Type": "application/json",
            "X-USL-Timestamp": str(timestamp),
            "X-USL-Signature": signing.signature(key, timestamp, "POST", path, body),
        }
        return self.url_open(path + query, data=body, headers=headers)

    def _read(self, who, task, channel=CHANNEL):
        response = self._post(
            "/usl/mattermost/task/read",
            {"v": 1, "user": f"subject-{who}", "channel_id": channel, "task": {"id": task.id}},
        )
        self.assertEqual(response.status_code, 200, response.text)
        return response.json()

    def _link(self, who, op, project, *, member=True, channel=CHANNEL):
        response = self._post(
            "/usl/mattermost/link",
            {
                "v": 1,
                "user": f"subject-{who}",
                "op": op,
                "channel": {"id": channel, "name": "general", "display_name": "General", "team_name": "usl"},
                "channel_member": member,
                "project": project,
            },
        )
        self.assertEqual(response.status_code, 200, response.text)
        return response.json()

    # --- protocol -------------------------------------------------------

    def test_rpc_cannot_call_the_api_methods(self):
        # Only the signed endpoints may name a Pocket ID subject.
        service = self.env["usl.mattermost.service"]
        for name in ("_api_read_task", "_api_watch", "_api_link", "_api_links"):
            with self.assertRaises(AccessError):
                get_public_method(service, name)
        for name in ("read_task", "watch", "link", "links"):
            self.assertFalse(hasattr(service, name), name)

    def test_a_copied_request_is_refused(self):
        payload = {"v": 1, "user": "subject-member", "channel_id": CHANNEL, "rid": "same"}
        timestamp = int(time.time())
        first = self._post("/usl/mattermost/links", payload, timestamp=timestamp)
        self.assertEqual(first.status_code, 200, first.text)
        second = self._post("/usl/mattermost/links", payload, timestamp=timestamp)
        self.assertEqual(second.status_code, 401)
        self.assertEqual(second.json()["error"], "replayed")

    def test_a_query_string_is_refused(self):
        response = self._post(
            "/usl/mattermost/links",
            {"v": 1, "user": "subject-member", "channel_id": CHANNEL},
            query="?debug=1",
        )
        self.assertEqual(response.status_code, 400)

    def test_a_non_ascii_signature_is_refused(self):
        body = b'{"v": 1}'
        response = self.url_open(
            "/usl/mattermost/links",
            data=body,
            headers={"X-USL-Timestamp": str(int(time.time())), "X-USL-Signature": "v1=\u00e9"},
        )
        self.assertEqual(response.status_code, 401)

    def test_a_bad_action_is_not_a_server_error(self):
        for action in ("foo", "²", "a.b.c", "1²"):
            with self.subTest(action=action):
                response = self._post(
                    "/usl/mattermost/task/read",
                    {"v": 1, "user": "subject-member", "channel_id": CHANNEL, "task": {"action": action, "id": self.open_task.id}},
                )
                self.assertEqual(response.status_code, 200, response.text)
                self.assertEqual(response.json()["reason"], "not_found")

    def test_wrong_key_is_refused(self):
        response = self._post(
            "/usl/mattermost/task/read",
            {"v": 1, "user": "subject-member", "channel_id": CHANNEL, "task": {"id": 1}},
            key="another-key-" + "x" * 30,
        )
        self.assertEqual(response.status_code, 401)
        self.assertEqual(response.json()["error"], "bad_signature")

    def test_old_timestamp_is_refused(self):
        response = self._post(
            "/usl/mattermost/task/read",
            {"v": 1, "user": "subject-member", "channel_id": CHANNEL, "task": {"id": 1}},
            timestamp=int(time.time()) - 600,
        )
        self.assertEqual(response.status_code, 401)
        self.assertEqual(response.json()["error"], "stale_timestamp")

    def test_missing_key_answers_not_configured(self):
        with patch.object(signing, "load_keys", return_value=[]):
            response = self._post("/usl/mattermost/links", {"v": 1})
        self.assertEqual(response.status_code, 503)
        self.assertEqual(response.json()["error"], "not_configured")

    def test_bad_bodies_are_refused(self):
        for payload, error in (
            ({"v": 2, "user": "subject-member", "channel_id": CHANNEL}, "unsupported_version"),
            ({"v": 1, "channel_id": CHANNEL}, "bad_request"),
            ({"v": 1, "user": "subject-member", "channel_id": CHANNEL, "task": {"id": True}}, "bad_request"),
        ):
            with self.subTest(error=error):
                response = self._post("/usl/mattermost/task/read", payload)
                self.assertEqual(response.status_code, 400)
                self.assertEqual(response.json()["error"], error)
        response = self._post("/usl/mattermost/task/read", None, raw=b"{not json")
        self.assertEqual(response.json()["error"], "bad_request")

    def test_large_body_is_refused(self):
        raw = b'{"v": 1, "pad": "' + b"x" * (signing.MAX_BODY_BYTES + 1) + b'"}'
        response = self._post("/usl/mattermost/task/read", None, raw=raw)
        self.assertEqual(response.status_code, 413)

    def test_health(self):
        path = "/usl/mattermost/health"
        timestamp = int(time.time())
        response = self.url_open(
            path,
            headers={
                "X-USL-Timestamp": str(timestamp),
                "X-USL-Signature": signing.signature(PLUGIN_KEY, timestamp, "GET", path, b""),
            },
        )
        self.assertEqual(response.status_code, 200, response.text)
        answer = response.json()
        self.assertTrue(answer["ok"])
        self.assertEqual(answer["module"], "usl_mattermost")
        self.assertFalse(answer["webhook_configured"])

    # --- task cards -----------------------------------------------------

    def test_unknown_user_gets_no_card(self):
        answer = self._read("nobody", self.open_task)
        self.assertEqual(answer, {"v": 1, "ok": True, "visible": False, "reason": "no_user"})

    def test_open_project_task_is_visible(self):
        answer = self._read("member", self.open_task)
        self.assertTrue(answer["visible"])
        task = answer["task"]
        self.assertEqual(task["id"], self.open_task.id)
        self.assertEqual(task["name"], "Open task")
        self.assertEqual(task["project"], {"id": self.open_project.id, "name": "Open project"})
        self.assertEqual(task["assignees"], [{"id": self.member.id, "name": "Member"}])
        self.assertTrue(
            task["url"].endswith(f"/odoo/project/{self.open_project.id}/tasks/{self.open_task.id}"),
        )

    def test_private_project_needs_a_link(self):
        self.assertEqual(self._read("member", self.private_task)["reason"], "not_shared")

    def test_task_the_user_cannot_read(self):
        self.assertEqual(self._read("outsider", self.private_task)["reason"], "no_access")

    def test_archived_task_of_another_user_looks_unreadable(self):
        self.private_task.active = False
        self.assertEqual(self._read("outsider", self.private_task)["reason"], "no_access")

    def test_links_are_for_project_managers(self):
        self._link("manager", "add", "Private project")
        Links = self.env["usl.mattermost.channel.link"]
        with self.assertRaises(AccessError):
            Links.with_user(self.member).search_read([], ["channel_name", "project_id"])
        self.assertEqual(len(Links.with_user(self.manager).search([])), 1)

    def test_lost_access_refreshes_the_cards(self):
        self._link("manager", "add", "Private project")
        self._watch("member", self.private_task)
        card = self.env["usl.mattermost.card"].search([("task_id", "=", self.private_task.id)])
        self.assertTrue(card)
        self.assertFalse(card.needs_push)
        self._link("manager", "remove", "Private project")
        self.assertTrue(card.needs_push)
        card.needs_push = False
        self.private_project.privacy_visibility = "employees"
        self.assertTrue(card.needs_push)
        card.needs_push = False
        self.private_project.message_unsubscribe(partner_ids=self.member.partner_id.ids)
        self.assertTrue(card.needs_push)
        card.needs_push = False
        self.outsider.active = False
        self.assertTrue(card.needs_push)

    def test_archived_and_missing_tasks(self):
        self.open_task.active = False
        self.assertEqual(self._read("member", self.open_task)["reason"], "archived")
        missing = self.env["project.task"].browse(999999)
        self.assertEqual(self._read("member", missing)["reason"], "not_found")

    def test_action_link_must_open_tasks(self):
        def read(action):
            return self._post(
                "/usl/mattermost/task/read",
                {
                    "v": 1,
                    "user": "subject-member",
                    "channel_id": CHANNEL,
                    "task": {"action": action, "id": self.open_task.id},
                },
            ).json()

        self.assertTrue(read("project.act_project_project_2_project_task_all")["visible"])
        self.assertEqual(read("project.open_view_project_all")["reason"], "not_found")

    # --- channel links --------------------------------------------------

    def test_link_opens_a_private_project_to_one_channel(self):
        answer = self._link("manager", "add", "Private project")
        self.assertEqual(
            answer,
            {
                "v": 1,
                "ok": True,
                "changed": True,
                "project": {"id": self.private_project.id, "name": "Private project"},
            },
        )
        self.assertTrue(self._read("member", self.private_task)["visible"])
        other = self._read("member", self.private_task, channel=OTHER_CHANNEL)
        self.assertEqual(other["reason"], "not_shared")
        self.assertEqual(self._read("outsider", self.private_task)["reason"], "no_access")
        self.assertFalse(self._link("manager", "add", self.private_project.id)["changed"])
        notes = self.private_project.message_ids.mapped("body")
        self.assertTrue(any("General" in (body or "") for body in notes))

    def test_add_needs_channel_member_and_edit_right(self):
        self.assertEqual(
            self._link("manager", "add", "Private project", member=False)["error"],
            "forbidden",
        )
        self.assertEqual(self._link("member", "add", "Private project")["error"], "forbidden")

    def test_remove_needs_one_of_the_two(self):
        self._link("manager", "add", "Private project")
        self.assertTrue(self._link("member", "remove", "Private project")["changed"])
        self.assertEqual(self._read("member", self.private_task)["reason"], "not_shared")

    def test_project_names(self):
        Projects = self.env["project.project"]
        Projects.create({"name": "Alpha one"})
        Projects.create({"name": "Alpha two"})
        answer = self._link("manager", "add", "alpha")
        self.assertEqual(answer["error"], "project_ambiguous")
        self.assertEqual({c["name"] for c in answer["candidates"]}, {"Alpha one", "Alpha two"})
        self.assertTrue(self._link("manager", "add", "alpha ONE")["ok"])
        self.assertEqual(self._link("outsider", "add", "Private project")["error"], "project_not_found")

    def test_links_hide_unreadable_projects(self):
        self._link("manager", "add", "Private project")
        self._link("manager", "add", "Open project")
        answer = self._post(
            "/usl/mattermost/links",
            {"v": 1, "user": "subject-outsider", "channel_id": CHANNEL},
        ).json()
        self.assertEqual([p["name"] for p in answer["projects"]], ["Open project"])
        self.assertEqual(answer["hidden"], 1)

    # --- live cards -----------------------------------------------------

    def _watch(self, who, task, op="add", post="p4w8n2k6r0t5y9u3i7o1a5s8d2"):
        return self._post(
            "/usl/mattermost/task/watch",
            {"v": 1, "user": f"subject-{who}", "op": op, "channel_id": CHANNEL, "post_id": post, "task_id": task.id},
        ).json()

    def test_watch_follows_the_card_rule(self):
        self.assertEqual(self._watch("outsider", self.private_task)["reason"], "no_access")
        self.assertTrue(self._watch("member", self.open_task)["watching"])
        self.assertTrue(self._watch("member", self.open_task)["watching"])
        cards = self.env["usl.mattermost.card"].search([("task_id", "=", self.open_task.id)])
        self.assertEqual(len(cards), 1)
        self.assertFalse(self._watch("member", self.open_task, op="remove")["watching"])
        self.assertFalse(cards.exists())

    def test_a_change_is_sent_to_mattermost(self):
        self._watch("member", self.open_task)
        card = self.env["usl.mattermost.card"].search([("task_id", "=", self.open_task.id)])
        self.open_task.description = "<p>Not on the card</p>"
        self.assertFalse(card.needs_push)
        self.open_task.name = "Renamed task"
        self.assertTrue(card.needs_push)
        self.env["ir.config_parameter"].sudo().set_str(
            card_module.MATTERMOST_URL_PARAMETER,
            "https://chat.example.test/",
        )
        sent = Mock(status_code=200)
        with patch.object(card_module.requests, "post", return_value=sent) as post:
            self.env["usl.mattermost.card"]._cron_push()
        post.assert_called_once()
        self.assertIs(post.call_args.kwargs["allow_redirects"], False)
        url = post.call_args.args[0]
        self.assertEqual(url, "https://chat.example.test" + card_module.TASK_CHANGED_PATH)
        body = post.call_args.kwargs["data"]
        headers = post.call_args.kwargs["headers"]
        self.assertIsNone(
            signing.verify(
                [WEBHOOK_KEY],
                headers["X-USL-Timestamp"],
                headers["X-USL-Signature"],
                "POST",
                card_module.TASK_CHANGED_PATH,
                body,
            ),
        )
        self.assertEqual(
            json.loads(body),
            {
                "v": 1,
                "task_id": self.open_task.id,
                "posts": [{"post_id": "p4w8n2k6r0t5y9u3i7o1a5s8d2", "channel_id": CHANNEL}],
            },
        )
        self.assertFalse(card.needs_push)

    def test_a_failed_send_stays_queued(self):
        self._watch("member", self.open_task)
        card = self.env["usl.mattermost.card"].search([("task_id", "=", self.open_task.id)])
        self.open_task.name = "Renamed again"
        self.env["ir.config_parameter"].sudo().set_str(
            card_module.MATTERMOST_URL_PARAMETER,
            "https://chat.example.test",
        )
        with patch.object(card_module.requests, "post", return_value=Mock(status_code=502)):
            self.env["usl.mattermost.card"]._cron_push()
        self.assertTrue(card.needs_push)
        self.assertEqual(card.push_attempts, 1)
        self.assertEqual(card.last_push_error, "HTTP 502")

    def test_only_an_https_mattermost_url_is_used(self):
        self._watch("member", self.open_task)
        self.open_task.name = "Renamed for the URL test"
        Cards = self.env["usl.mattermost.card"]
        for url in ("http://chat.example.test", "https://chat.example.test?x=1", "https://user@chat.example.test", "file:///etc/passwd"):
            with self.subTest(url=url):
                self.env["ir.config_parameter"].sudo().set_str(card_module.MATTERMOST_URL_PARAMETER, url)
                with patch.object(card_module.requests, "post") as post:
                    Cards._cron_push()
                post.assert_not_called()
