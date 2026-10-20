{
    "name": "USL Mattermost",
    "summary": "Odoo task cards in Mattermost: signed API for the Mattermost plugin, and project-channel links",
    "version": "saas~19.3.0.2.0",
    "category": "Services/Project",
    "author": "Unstatic Labs",
    "website": "https://github.com/unstaticlabs/mattermost-odoo",
    "license": "LGPL-3",
    "depends": [
        "mail",
        "project",
        "usl_pocketid",
    ],
    "external_dependencies": {"python": ["requests"]},
    "data": [
        "security/ir.model.access.csv",
        "data/ir_cron.xml",
        "views/project_project_views.xml",
    ],
    "application": False,
    "installable": True,
}
