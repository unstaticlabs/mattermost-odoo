from datetime import timedelta

from odoo import api, fields, models

from .. import signing


class UslMattermostNonce(models.Model):
    """A signature that Odoo accepted in the last minutes.

    A signed request that Odoo receives a second time is a copy, so Odoo
    refuses it. The plugin adds a random request id to every body, so two
    equal requests never have the same signature.
    """

    _name = "usl.mattermost.nonce"
    _description = "Accepted Mattermost API signature"
    _log_access = False

    signature = fields.Char(required=True)
    accepted_at = fields.Datetime(required=True, index=True)

    _unique_signature = models.Constraint(
        "UNIQUE(signature)",
        "This signature was already used.",
    )

    @api.model
    def _usl_first_use(self, signature):
        """Record `signature`. Return False when it was already used."""
        now = fields.Datetime.now()
        window = timedelta(seconds=2 * signing.MAX_SKEW_SECONDS)
        cr = self.env.cr
        cr.execute(
            "DELETE FROM usl_mattermost_nonce WHERE accepted_at < %s",
            (now - window,),
        )
        cr.execute(
            """
            INSERT INTO usl_mattermost_nonce (signature, accepted_at)
            VALUES (%s, %s)
            ON CONFLICT (signature) DO NOTHING
            RETURNING id
            """,
            (signature, now),
        )
        return bool(cr.fetchone())
