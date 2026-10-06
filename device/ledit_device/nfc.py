"""Optional NFC/RFID reader (PN532-class) reporting tag taps.

The reader is built behind an optional import (nfcpy); when the library or
hardware is unavailable the source logs once and stays inert. Tag UIDs are
normalized to lowercase hex and suppressed for ``LEDIT_NFC_DEDUPE_MS`` when
the same tag is presented repeatedly.
"""

from __future__ import annotations

import threading
import time

from .config import log
from .inputs import EVENT_TAP, SOURCE_NFC


def normalize_uid(uid):
    """Normalize a tag UID to lowercase hex without separators."""
    if isinstance(uid, (bytes, bytearray)):
        return bytes(uid).hex().lower()
    text = str(uid).strip().lower()
    for sep in (":", " ", "-"):
        text = text.replace(sep, "")
    return text


class NFCReader:
    def __init__(self, sender, dedupe_ms=30000, driver_factory=None):
        self._sender = sender
        self._dedupe_s = max(0.0, float(dedupe_ms or 0) / 1000.0)
        self._driver_factory = driver_factory
        self._driver = None
        self._thread = None
        self._stop = threading.Event()
        self._lock = threading.Lock()
        self._last_uid = None
        self._last_time = 0.0
        self._started = False

    @property
    def started(self):
        return self._started

    def start(self):
        if self._started:
            return
        try:
            self._driver = self._open_driver()
        except Exception as exc:
            log("nfc disabled: %s", exc)
            return
        self._started = True
        self._thread = threading.Thread(target=self._run, name="ledit-nfc", daemon=True)
        self._thread.start()
        log("nfc enabled")

    def _open_driver(self):
        if self._driver_factory is not None:
            return self._driver_factory()
        import nfc  # nfcpy

        return nfc.ContactlessFrontend("usb")

    def _run(self):
        while not self._stop.is_set():
            try:
                tag = self._driver.read_passive_target(timeout=0.5)
            except Exception as exc:
                log("nfc read failed: %s", exc)
                time.sleep(1.0)
                continue
            if not tag:
                continue
            uid = getattr(tag, "identifier", tag)
            self.handle_uid(uid)

    def handle_uid(self, uid):
        """Handle one tag UID, applying normalization and dedupe."""
        text = normalize_uid(uid)
        if not text:
            return
        now = time.monotonic()
        with self._lock:
            if text == self._last_uid and (now - self._last_time) < self._dedupe_s:
                return
            self._last_uid = text
            self._last_time = now
        try:
            self._sender(SOURCE_NFC, EVENT_TAP, text)
        except Exception as exc:
            log("nfc send failed: %s", exc)

    def close(self):
        self._stop.set()
        thread = self._thread
        if thread is not None:
            thread.join(timeout=0.2)
            self._thread = None
        driver = self._driver
        if driver is not None:
            try:
                close = getattr(driver, "close", None)
                if close is not None:
                    close()
            except Exception:
                pass
            self._driver = None
        self._started = False

    stop = close
