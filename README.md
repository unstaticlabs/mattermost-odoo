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

Scaffold only. The plugin is the Mattermost plugin template, renamed. The Odoo
module has no code yet.

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
