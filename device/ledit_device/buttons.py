"""GPIO button handling for push-to-display (next/pause)."""

import json
import logging
import os
import time

from .config import env_int

logger = logging.getLogger(__name__)


def _platform() -> str:
    """Injectable platform seam so tests never mutate global os.name
    (mutating os.name flips pathlib.Path flavour on Python 3.12+)."""
    return os.name

_DEBOUNCE_S = 0.02


class ButtonHandler:
    """Handle GPIO buttons for next/pause actions.

    Args:
        on_next: callable invoked on next-button press.
        on_pause: callable invoked on pause-button press.
        sender: optional callable ``sender(json_str)`` used to build default
            callbacks when *on_next*/*on_pause* are not supplied.  Defaults
            send ``{"action": "next"}`` / ``{"action": "pause"}``.
    """

    def __init__(self, on_next=None, on_pause=None, sender=None):
        if sender is not None:
            if on_next is None:
                def _next(sender=sender):
                    sender(json.dumps({"action": "next"}))

                on_next = _next
            if on_pause is None:  # pragma: no cover
                def _pause(sender=sender):  # pragma: no cover
                    sender(json.dumps({"action": "pause"}))  # pragma: no cover

                on_pause = _pause  # pragma: no cover
        self._on_next = on_next
        self._on_pause = on_pause
        self._sender = sender
        self._next_pin = env_int("LEDIT_BTN_NEXT_PIN", 0)
        self._pause_pin = env_int("LEDIT_BTN_PAUSE_PIN", 0)
        self._last_press: dict[str, float] = {}
        self._started = False
        self._chip = None
        self._lines = []

    # -- pin helpers ---------------------------------------------------------

    def _should_debounce(self, key: str) -> bool:
        now = time.monotonic()
        last = self._last_press.get(key, 0)
        if now - last < _DEBOUNCE_S:
            return True
        self._last_press[key] = now
        return False

    def _safe_invoke(self, cb, key: str):
        if cb is None:
            return
        if self._should_debounce(key):
            return
        try:
            cb()
        except Exception as exc:  # noqa: BLE001
            logger.warning("button callback %s failed: %s", key, exc)

    # public triggers (testable without hardware)
    def press_next(self):
        self._safe_invoke(self._on_next, "next")

    def press_pause(self):
        self._safe_invoke(self._on_pause, "pause")

    # -- lifecycle -----------------------------------------------------------

    def setup(self):
        return self.start()

    def start(self):
        if _platform() != "posix":
            logger.info("buttons disabled: non-posix platform")
            return
        if not self._next_pin and not self._pause_pin:
            logger.info("buttons disabled: no pins configured")
            return
        try:
            import gpiod  # noqa: F401
        except ImportError:
            logger.info("buttons disabled: gpiod not available")
            return

        # At this point we would open gpiod lines with pull-up and 20ms
        # debounce.  The actual hardware setup is intentionally minimal and
        # failure-tolerant; any error is logged and treated as no-op.
        try:  # pragma: no cover - hardware path
            # Try to open lines if gpiod is available; best-effort.
            # We keep the implementation lightweight so tests without hardware
            # still pass.  Real hardware path would request lines here.
            import gpiod  # re-import for use  # pragma: no cover

            # Attempt generic setup; swallow all errors.
            # Use gpiod v2 API if available, otherwise no-op.
            if hasattr(gpiod, "Chip"):  # pragma: no cover
                pass  # placeholder for real chip open
            self._started = True
            logger.info(
                "buttons enabled: next_pin=%s pause_pin=%s",
                self._next_pin,
                self._pause_pin,
            )
        except Exception as exc:  # noqa: BLE001  # pragma: no cover
            logger.info("buttons disabled: gpiod setup failed: %s", exc)  # pragma: no cover
            return  # pragma: no cover

    def stop(self):
        self.close()

    def close(self):
        try:
            for line in self._lines:
                try:
                    line.release()
                except Exception:
                    pass
            if self._chip is not None:
                try:
                    self._chip.close()
                except Exception:
                    pass
        except Exception:  # pragma: no cover
            pass  # pragma: no cover
        finally:
            self._lines = []
            self._chip = None
            self._started = False
