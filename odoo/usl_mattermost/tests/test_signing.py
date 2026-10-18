import os
import tempfile

from odoo.tests import BaseCase, tagged

from .. import signing

KEY = "k" * 40


@tagged("post_install", "-at_install", "usl_mattermost")
class TestSigning(BaseCase):
    def test_valid_signature(self):
        sig = signing.signature(KEY, 1000, "post", "/p", b"{}")
        self.assertTrue(sig.startswith("v1="))
        self.assertIsNone(signing.verify([KEY], "1000", sig, "POST", "/p", b"{}", now=1100))

    def test_every_signed_part_counts(self):
        sig = signing.signature(KEY, 1000, "POST", "/p", b"{}")
        for args in (
            ("1001", sig, "POST", "/p", b"{}"),
            ("1000", sig, "GET", "/p", b"{}"),
            ("1000", sig, "POST", "/q", b"{}"),
            ("1000", sig, "POST", "/p", b"{ }"),
        ):
            with self.subTest(args=args[:4]):
                self.assertEqual(signing.verify([KEY], *args, now=1000), "bad_signature")

    def test_previous_key_is_accepted(self):
        old = "o" * 40
        sig = signing.signature(old, 1000, "POST", "/p", b"")
        self.assertIsNone(signing.verify([KEY, old], "1000", sig, "POST", "/p", b"", now=1000))

    def test_timestamp_window(self):
        sig = signing.signature(KEY, 1000, "POST", "/p", b"")
        self.assertEqual(signing.verify([KEY], "1000", sig, "POST", "/p", b"", now=1301), "stale_timestamp")
        self.assertEqual(signing.verify([KEY], "1000", sig, "POST", "/p", b"", now=699), "stale_timestamp")
        self.assertEqual(signing.verify([KEY], "soon", sig, "POST", "/p", b"", now=1000), "stale_timestamp")
        self.assertEqual(signing.verify([KEY], "1000", "", "POST", "/p", b"", now=1000), "bad_signature")

    def test_key_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "key")
            with open(path, "w", encoding="utf-8") as key_file:
                key_file.write("\n" + KEY + "\n" + "o" * 40 + "\nignored-third-line-" + "x" * 30 + "\n")
            os.environ["USL_MATTERMOST_TEST_KEY_FILE"] = path
            try:
                self.assertEqual(
                    signing.load_keys("USL_MATTERMOST_TEST_KEY_FILE", "/nonexistent"),
                    [KEY, "o" * 40],
                )
                with open(path, "w", encoding="utf-8") as key_file:
                    key_file.write("too-short\n")
                self.assertEqual(signing.load_keys("USL_MATTERMOST_TEST_KEY_FILE", "/nonexistent"), [])
            finally:
                del os.environ["USL_MATTERMOST_TEST_KEY_FILE"]
        self.assertEqual(signing.load_keys("USL_MATTERMOST_NO_SUCH_VAR", "/nonexistent/key"), [])
