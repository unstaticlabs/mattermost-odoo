"""HTTP endpoints of the Mattermost API (docs/api.md).

Each request is checked here: size, signature, timestamp, JSON, version,
and field types. The rules about users, tasks, and projects are in the
usl.mattermost.service model.
"""

import json

from odoo import http
from odoo.http import request, route

from .. import signing

API_VERSIONS = {1}
MESSAGES = {
    "bad_signature": "The signature does not match.",
    "stale_timestamp": "The timestamp is missing or more than 5 minutes away.",
    "too_large": "The body is above 64 KiB.",
    "not_configured": "Odoo has no key for the plugin.",
    "unsupported_version": "This API version is not supported.",
}
STATUS = {
    "bad_signature": 401,
    "stale_timestamp": 401,
    "too_large": 413,
    "bad_request": 400,
    "unsupported_version": 400,
    "not_configured": 503,
}
ROUTE_OPTIONS = {
    "type": "http",
    "auth": "public",
    "csrf": False,
    "sitemap": False,
    "save_session": False,
}


class BadRequest(Exception):
    pass


def _field(data, name, kind, *, required=True):
    value = data.get(name)
    if value is None and not required:
        return None
    if kind is int and isinstance(value, bool):
        raise BadRequest(f"'{name}' must be an integer.")
    if not isinstance(value, kind) or (kind is str and not value.strip()):
        raise BadRequest(f"'{name}' is missing or has the wrong type.")
    return value


def _op(data):
    op = _field(data, "op", str)
    if op not in ("add", "remove"):
        raise BadRequest("'op' must be 'add' or 'remove'.")
    return op


class UslMattermostApi(http.Controller):
    def _answer(self, payload, status=200):
        return request.make_json_response({"v": 1, **payload}, status=status)

    def _error(self, code, message=None):
        return self._answer(
            {"ok": False, "error": code, "message": message or MESSAGES.get(code, code)},
            status=STATUS[code],
        )

    def _authenticated(self, handler, *, with_body=True):
        httprequest = request.httprequest
        body = httprequest.get_data(cache=True)
        if len(body) > signing.MAX_BODY_BYTES:
            return self._error("too_large")
        keys = signing.load_keys(signing.PLUGIN_KEY_ENV, signing.PLUGIN_KEY_DEFAULT)
        if not keys:
            return self._error("not_configured")
        error = signing.verify(
            keys,
            httprequest.headers.get("X-USL-Timestamp"),
            httprequest.headers.get("X-USL-Signature"),
            httprequest.method,
            httprequest.path,
            body,
        )
        if error:
            return self._error(error)
        data = {}
        if with_body:
            try:
                data = json.loads(body or b"null")
            except ValueError:
                return self._error("bad_request", "The body is not valid JSON.")
            if not isinstance(data, dict):
                return self._error("bad_request", "The body must be a JSON object.")
            if data.get("v") not in API_VERSIONS:
                return self._error("unsupported_version")
        service = request.env["usl.mattermost.service"].sudo()
        try:
            return self._answer(handler(service, data))
        except BadRequest as exc:
            return self._error("bad_request", str(exc))

    @route("/usl/mattermost/task/read", methods=["POST"], **ROUTE_OPTIONS)
    def task_read(self, **_kwargs):
        def handler(service, data):
            task = _field(data, "task", dict)
            task_ref = {"id": _field(task, "id", int)}
            action = _field(task, "action", str, required=False)
            if action:
                task_ref["action"] = action
            return service._api_read_task(
                _field(data, "user", str),
                _field(data, "channel_id", str),
                task_ref,
            )

        return self._authenticated(handler)

    @route("/usl/mattermost/task/watch", methods=["POST"], **ROUTE_OPTIONS)
    def task_watch(self, **_kwargs):
        def handler(service, data):
            return service._api_watch(
                _field(data, "user", str),
                _op(data),
                _field(data, "channel_id", str),
                _field(data, "post_id", str),
                _field(data, "task_id", int),
            )

        return self._authenticated(handler)

    @route("/usl/mattermost/link", methods=["POST"], **ROUTE_OPTIONS)
    def link(self, **_kwargs):
        def handler(service, data):
            channel = _field(data, "channel", dict)
            channel_info = {"id": _field(channel, "id", str)}
            for name in ("name", "display_name", "team_name"):
                channel_info[name] = _field(channel, name, str, required=False)
            project = data.get("project")
            if isinstance(project, bool) or not isinstance(project, (int, str)) or (
                isinstance(project, str) and not project.strip()
            ):
                raise BadRequest("'project' must be a project id or a name.")
            return service._api_link(
                _field(data, "user", str),
                _op(data),
                channel_info,
                _field(data, "channel_member", bool),
                project,
            )

        return self._authenticated(handler)

    @route("/usl/mattermost/links", methods=["POST"], **ROUTE_OPTIONS)
    def links(self, **_kwargs):
        def handler(service, data):
            return service._api_links(_field(data, "user", str), _field(data, "channel_id", str))

        return self._authenticated(handler)

    @route("/usl/mattermost/health", methods=["GET"], **ROUTE_OPTIONS)
    def health(self, **_kwargs):
        def handler(service, _data):
            module = request.env["ir.module.module"].sudo().search(
                [("name", "=", "usl_mattermost")],
                limit=1,
            )
            mattermost_url = request.env["ir.config_parameter"].sudo().get_str(
                "usl_mattermost.mattermost_url",
            )
            webhook_keys = signing.load_keys(
                signing.WEBHOOK_KEY_ENV,
                signing.WEBHOOK_KEY_DEFAULT,
            )
            return {
                "ok": True,
                "module": "usl_mattermost",
                "version": module.latest_version or None,
                "webhook_configured": bool(mattermost_url and webhook_keys),
            }

        return self._authenticated(handler, with_body=False)
