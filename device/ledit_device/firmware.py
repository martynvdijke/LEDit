"""Firmware OTA: poll manifest, verify sha256, stage atomically."""

from __future__ import annotations

import hashlib
import json
import os
import tempfile
import urllib.request
import urllib.error
import urllib.parse
import logging

logger = logging.getLogger("ledit_device")

# Marker dir for staged updates - ponytail: simple file marker, systemd can watch it
_STAGING_DIR_ENV = "LEDIT_STAGING_DIR"


def _staging_dir():
    return os.getenv(_STAGING_DIR_ENV, os.path.expanduser("~/.config/ledit/staging"))


def _report(server_url: str, token: str, version: str, status: str):
    base = server_url.rstrip("/")
    if base.startswith("ws://"):
        base = "http://" + base[len("ws://"):]
    elif base.startswith("wss://"):
        base = "https://" + base[len("wss://"):]
    url = base + "/api/device/firmware/report"
    body = json.dumps({"version": version, "status": status}).encode()
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Device-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            resp.read()
    except Exception as e:
        logger.warning("firmware report failed: %s", e)


def check_and_update(server_url: str, token: str, current_version: str, interval=None, channel: str | None = None) -> None:
    """Single poll cycle: check manifest and stage update if needed.

    Non-fatal on any failure; logs and returns.
    Guarantee: running process is never self-overwritten; staging uses temp file + atomic move into versioned dir.
    """
    base = server_url.rstrip("/")
    if base.startswith("ws://"):
        base = "http://" + base[len("ws://"):]
    elif base.startswith("wss://"):
        base = "https://" + base[len("wss://"):]

    # Build manifest URL
    qs = {"version": current_version}
    if channel:
        qs["channel"] = channel
    url = base + "/api/device/firmware?" + urllib.parse.urlencode(qs)

    try:
        req = urllib.request.Request(url, method="GET")
        req.add_header("X-Device-Token", token)
        with urllib.request.urlopen(req, timeout=10) as resp:
            body = resp.read().decode(errors="replace")
            try:
                manifest = json.loads(body) if body else {}
            except Exception:
                logger.warning("firmware manifest malformed")
                return
    except Exception as e:
        logger.warning("firmware manifest unreachable: %s", e)
        return

    if not isinstance(manifest, dict):
        logger.warning("firmware manifest unexpected shape")
        return

    action = manifest.get("action", "none")
    if action == "none" or not action:
        return

    version = manifest.get("version") or manifest.get("target_version") or ""
    sha256 = manifest.get("sha256") or ""
    size = manifest.get("size") or manifest.get("size_bytes") or 0
    # artifact url may be explicit or constructed
    artifact_url = manifest.get("url") or manifest.get("artifact_url") or ""
    if not artifact_url and version:
        artifact_url = "%s/api/device/firmware/%s/artifact" % (base, urllib.parse.quote(str(version)))

    if not version or not sha256 or not artifact_url:
        logger.warning("firmware manifest missing required fields")
        return

    # Download to temp staging file
    staging_dir = _staging_dir()
    try:
        os.makedirs(staging_dir, exist_ok=True)
    except Exception:
        pass

    tmp_fd = None
    tmp_path = None
    try:
        tmp_fd, tmp_path = tempfile.mkstemp(dir=staging_dir, prefix="firmware-")
        os.close(tmp_fd)
        tmp_fd = None
        req2 = urllib.request.Request(artifact_url, method="GET")
        req2.add_header("X-Device-Token", token)
        hasher = hashlib.sha256()
        downloaded = 0
        with urllib.request.urlopen(req2, timeout=60) as resp2:
            with open(tmp_path, "wb") as out:
                while True:
                    chunk = resp2.read(8192)
                    if not chunk:
                        break
                    out.write(chunk)
                    hasher.update(chunk)
                    downloaded += len(chunk)
        # size check if manifest provides size
        try:
            expected_size = int(size) if size else 0
        except Exception:
            expected_size = 0
        if expected_size and downloaded != expected_size:
            logger.error("firmware download truncated: expected %d got %d", expected_size, downloaded)
            try:
                os.remove(tmp_path)
            except Exception:
                pass
            _report(server_url, token, str(version), "failed")
            return
        digest = hasher.hexdigest()
        if digest.lower() != sha256.lower():
            logger.error("firmware sha256 mismatch: expected %s got %s", sha256, digest)
            try:
                os.remove(tmp_path)
            except Exception:
                pass
            _report(server_url, token, str(version), "failed")
            return

        # Stage atomically: move into versioned file and write activate marker
        # Guarantee: interrupted update leaves previous version bootable; running process never self-overwritten.
        versioned_path = os.path.join(staging_dir, "firmware-%s.bin" % str(version))
        try:
            os.replace(tmp_path, versioned_path)
        except Exception:
            # fallback
            import shutil
            shutil.move(tmp_path, versioned_path)
        marker = os.path.join(staging_dir, "activate")
        with open(marker, "w") as mf:
            mf.write(str(version))
        logger.info("firmware %s staged, restart required", version)
        _report(server_url, token, str(version), "success")
        # Do not self-overwrite; service manager should handle restart. Optionally exit could be added by caller.
    except Exception as e:
        logger.warning("firmware update failed: %s", e)
        if tmp_path and os.path.exists(tmp_path):
            try:
                os.remove(tmp_path)
            except Exception:
                pass
        try:
            _report(server_url, token, str(version) if version else current_version, "failed")
        except Exception:
            pass
        return
