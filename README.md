# mattermost-odoo

Odoo task links in Mattermost become live cards. This repository holds both
parts of the integration for `chat.unstaticlabs.com` and
`odoo.unstaticlabs.com`:

| Folder | What it is | How it ships |
|---|---|---|
| `mattermost-plugin/` | The Mattermost plugin `com.unstaticlabs.odoo` (Go and React) | `make usl-deploy` in that folder, from a Mac with the `odoo` SSH alias |
| `odoo/usl_mattermost/` | The Odoo module | The Odoo repository (`unstaticlabs/odoo`) pins a commit of this repository, and its image build copies the module into `/mnt/custom-addons` |
| `docs/api.md` | The API between the two, version 1 | Change it first, then both parts |

Nobody links an account. Both apps log in with Pocket ID, and the Pocket ID
user id joins a Mattermost user to an Odoo user.

## Status

- `docs/api.md`: version 1, with every request and response body.
- `odoo/usl_mattermost/`: written and tested (27 tests). It is not in the Odoo
  image yet.
- `mattermost-plugin/`: written and tested (12 Go tests, and an end-to-end
  test in a local Mattermost 12 with a fake Odoo that uses the module's own
  signing code). Not installed on `chat.unstaticlabs.com` yet.

Known limit: a live refresh changes the post's attachments, so Mattermost shows
the poster's message as "(edited)". The first card, added while the message is
posted, does not.

## Plugin settings

System Console > Plugins > Odoo: the Odoo URL, the plugin signing key, the
Odoo webhook key, and an optional previous webhook key for rotations. The keys
are the same values as Odoo's key files (`docs/api.md`).

The plugin also creates the bot `@odoo`, which announces in a channel when
someone links or unlinks a project there.

## Test the Odoo module

Run the tests with the `test` service of the Odoo repository, with this
module mounted next to its add-ons. From `~/projects/odoo`:

```bash
docker compose -p usl-odoo-saas-19-3 --profile test run --rm -v ~/projects/mattermost-odoo/odoo:/mnt/mattermost-odoo:ro -e ODOO_ADDONS_PATH=/opt/odoo/addons,/opt/odoo/odoo/addons,/mnt/custom-addons,/mnt/oca-addons,/mnt/mattermost-odoo -e ODOO_INIT_DB=odoo_addon_test_usl_mattermost -e USL_EINVOICE_LIVE_ENABLED=0 -e USL_EREPORTING_LIVE_ENABLED=0 test odoo --config=/etc/odoo/odoo.conf --database=odoo_addon_test_usl_mattermost --init=usl_mattermost --without-demo=true --workers=0 --max-cron-threads=0 --test-enable --test-tags=/usl_mattermost --stop-after-init
```

The test image must exist: `docker compose -p usl-odoo-saas-19-3 --profile test build test`.

## The plugin

- Built from `mattermost/mattermost-plugin-starter-template` at commit
  `c332fdcc` (2026-10-15). The template's licence is in
  `mattermost-plugin/LICENSE` (Apache 2.0).
- `make usl-dist` builds a bundle with Linux binaries only. `make usl-deploy`
  builds it and installs it with `scripts/usl-install.sh` (SSH to the host,
  `mmctl --local` in the container).
- The version comes from git: a tag `v<version>` on the commit you build, for
  example `v0.1.0`. Use build metadata (`+usl.N`) for a rebuild of the same
  version, never a pre-release (`-usl.N`): Mattermost treats a pre-release as
  older.

## The Odoo module

The module lives here, so that a change to the API changes both parts in one
commit. It follows `ARCHITECTURE.md` of the Odoo repository: an isolated custom
module that depends only on `project` and `usl_pocketid`, and changes no Odoo
core file.

## Secrets

Two keys, one for each direction, both tracked in rotator. See `docs/api.md`.
No Odoo API key and no per-user secret exist.
