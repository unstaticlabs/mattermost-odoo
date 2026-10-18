"""Request signing for the API between the Mattermost plugin and Odoo.

See docs/api.md of this repository. This file has no Odoo import, so the
plugin's rules can be checked against it without a database.
"""

import hashlib
import hmac
import os
import time

SCHEME = "v1="
MAX_SKEW_SECONDS = 300
MAX_BODY_BYTES = 64 * 1024
MIN_KEY_LENGTH = 32

PLUGIN_KEY_ENV = "USL_MATTERMOST_PLUGIN_KEY_FILE"
PLUGIN_KEY_DEFAULT = "/run/secrets/usl_mattermost_plugin_key"
WEBHOOK_KEY_ENV = "USL_MATTERMOST_WEBHOOK_KEY_FILE"
WEBHOOK_KEY_DEFAULT = "/run/secrets/usl_mattermost_webhook_key"


def load_keys(env_var, default_path):
    """Return the accepted keys: the current one, then the previous one.

    The file holds the current key on its first non-empty line and,
    during a rotation, the previous key on the second. A missing file or
    a key shorter than MIN_KEY_LENGTH gives no key.
    """
    path = os.environ.get(env_var) or default_path
    try:
        with open(path, encoding="utf-8") as key_file:
            lines = [line.strip() for line in key_file.read().splitlines()]
    except OSError:
        return []
    keys = [line for line in lines if line][:2]
    return [key for key in keys if len(key) >= MIN_KEY_LENGTH]


def signature(key, timestamp, method, path, body):
    """The X-USL-Signature value for one request."""
    message = b"\n".join(
        [str(timestamp).encode(), method.upper().encode(), path.encode(), body],
    )
    digest = hmac.new(key.encode(), message, hashlib.sha256).hexdigest()
    return SCHEME + digest


def verify(keys, timestamp_header, signature_header, method, path, body, now=None):
    """Check one request. Return None when it is valid, else an error code."""
    try:
        timestamp = int(timestamp_header or "")
    except ValueError:
        return "stale_timestamp"
    current = time.time() if now is None else now
    if abs(current - timestamp) > MAX_SKEW_SECONDS:
        return "stale_timestamp"
    if not signature_header or not signature_header.startswith(SCHEME):
        return "bad_signature"
    for key in keys:
        expected = signature(key, timestamp, method, path, body)
        if hmac.compare_digest(expected, signature_header):
            return None
    return "bad_signature"
