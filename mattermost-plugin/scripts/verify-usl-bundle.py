#!/usr/bin/env python3
"""Verify the Linux-only archive produced by make usl-dist."""

import argparse
import hashlib
import json
import struct
import tarfile
from pathlib import Path, PurePosixPath


def verify(archive: Path) -> dict:
    if archive.stat().st_size >= 90 * 1024 * 1024:
        raise ValueError("USL bundle must be smaller than the 90 MiB upload limit")
    plugin_id = "com.unstaticlabs.odoo"
    machines = {"linux-amd64": 62, "linux-arm64": 183}
    with tarfile.open(archive, "r:gz") as bundle:
        manifest_file = bundle.extractfile(f"{plugin_id}/plugin.json")
        if manifest_file is None:
            raise ValueError("Bundle manifest is missing")
        manifest = json.load(manifest_file)
        if manifest.get("id") != plugin_id or not manifest.get("version"):
            raise ValueError("Bundle must identify the Odoo plugin and its version")
        executables = manifest.get("server", {}).get("executables", {})
        if set(executables) != set(machines):
            raise ValueError("USL manifest must list exactly Linux amd64 and arm64")
        for platform, machine in machines.items():
            expected = f"server/dist/plugin-{platform}"
            if executables[platform] != expected:
                raise ValueError(f"Unexpected executable path for {platform}")
            member = bundle.getmember(f"{plugin_id}/{expected}")
            executable = bundle.extractfile(member)
            if not member.isfile() or executable is None:
                raise ValueError(f"Missing executable for {platform}")
            header = executable.read(20)
            if (len(header) != 20 or header[:6] != b"\x7fELF\x02\x01"
                    or struct.unpack_from("<H", header, 18)[0] != machine):
                raise ValueError(f"Executable is not a 64-bit {platform} ELF")
        web_path = manifest.get("webapp", {}).get("bundle_path")
        if web_path != "webapp/dist/main.js":
            raise ValueError("Manifest must name the native webapp entry")
        web_entry = bundle.getmember(f"{plugin_id}/{web_path}")
        if not web_entry.isfile() or web_entry.size == 0:
            raise ValueError("Webapp entry is missing or empty")
        for member in bundle.getmembers():
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts:
                raise ValueError("Archive member escapes the plugin directory")
            if member.isfile() and not member.name.startswith(f"{plugin_id}/"):
                raise ValueError("Archive member is outside the plugin directory")
    digest = hashlib.sha256()
    with archive.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return {"archive": archive.name, "bytes": archive.stat().st_size,
            "sha256": digest.hexdigest(), "id": plugin_id, "version": manifest["version"],
            "platforms": list(machines), "webapp": web_path}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(verify(args.archive), indent=2))
    except (OSError, ValueError, KeyError, tarfile.TarError) as error:
        parser.exit(1, f"Bundle verification failed: {error}\n")
