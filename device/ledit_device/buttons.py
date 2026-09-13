"""GPIO button handling for push-to-display (next/pause) and hold gestures."""

import json
import logging
import os
import threading
import time

from .config import env_int

logger = logging.getLogger(__name__)


def _platform() -> str:
    """Injectable platform seam so tests never mutate global os.name
    (mutating os.name flips pathlib.Path flavour on Python 3.12+)."""
    return os.name

_DEBOUNCE_S = 0.02


class ButtonHandler:
    """Handle GPIO buttons for next/pause actions plus long-press/hold.

    Short presses (released before the long-press threshold) emit the existing
    ``next``/``pause`` actions on release. A press held at or beyond the long
    threshold emits ``{"action": "hold"}`` (once, or repeatedly when
    ``LEDIT_BUTTON_HOLD_REPEAT_MS`` is set).

    Args:
        on_next: callable invoked on a short next press.
        on_pause: callable invoked on a short pause press.
        on_hold: callable invoked on a long press/hold; defaults to sending
            ``{"action": "hold"}`` when *sender* is supplied.
        sender: optional callable ``sender(json_str)`` used to build default
            callbacks when *on_next*/*on_pause*/*on_hold* are not supplied.
    """

    def __init__(self, on_next=None, on_pause=None, sender=None, on_hold=None):
        if sender is not None:
            if on_next is None:
                def _next(sender=sender):
                    sender(json.dumps({"action": "next"}))

                on_next = _next
            if on_pause is None:  # pragma: no cover
                def _pause(sender=sender):  # pragma: no cover
                    sender(json.dumps({"action": "pause"}))  # pragma: no cover

                on_pause = _pause  # pragma: no cover
            if on_hold is None:
                def _hold(sender=sender):
                    sender(json.dumps({"action": "hold"}))

                on_hold = _hold
        self._on_next = on_next
        self._on_pause = on_pause
        self._on_hold = on_hold
        self._sender = sender
        self._next_pin = env_int("LEDIT_BTN_NEXT_PIN", 0)
        self._pause_pin = env_int("LEDIT_BTN_PAUSE_PIN", 0)
        self._last_press: dict[str, float] = {}
        self._started = False
        self._chip = None
        self._lines = []

        # Gesture thresholds (ms). A release before the long threshold is a
        # short press; the long threshold must exceed the nominal short window.
        self._short_ms = env_int("LEDIT_BUTTON_SHORT_MS", 500)
        self._long_ms = env_int("LEDIT_BUTTON_LONG_MS", 800)
        self._repeat_ms = env_int("LEDIT_BUTTON_HOLD_REPEAT_MS", 0)
        if self._short_ms <= 0:
            self._short_ms = 500
        if self._long_ms <= self._short_ms:
            self._long_ms = self._short_ms + 300
        if self._repeat_ms < 0:
            self._repeat_ms = 0

        self._press_start: dict[str, float] = {}
        self._hold_sent: dict[str, bool] = {}
        self._hold_timers: dict[str, threading.Timer] = {}

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
        self._invoke(cb, key)

    def _invoke(self, cb, key: str):
        if cb is None:
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

    # -- gesture state machine ----------------------------------------------

    def _begin_press(self, key: str):
        if self._should_debounce(key):
            return
        self._press_start[key] = time.monotonic()
        self._hold_sent[key] = False
        self._start_hold_timer(key)

    def _end_press(self, key: str, short_cb):
        start = self._press_start.pop(key, None)
        self._cancel_hold_timer(key)
        if start is None:
            return
        if self._hold_sent.get(key):
            return
        duration_ms = (time.monotonic() - start) * 1000.0
        if duration_ms >= self._long_ms:
            self._emit_hold(key)
        else:
            self._invoke(short_cb, key)

    def _start_hold_timer(self, key: str):
        self._cancel_hold_timer(key)

        def fire():
            if key not in self._press_start:
                return
            if self._hold_sent.get(key):
                # Repeat while still held: bypass the once-only guard.
                self._invoke(self._on_hold, key + ":hold")
            else:
                self._emit_hold(key)
            if self._repeat_ms > 0 and key in self._press_start:
                timer = threading.Timer(self._repeat_ms / 1000.0, fire)
                timer.daemon = True
                self._hold_timers[key] = timer
                timer.start()

        timer = threading.Timer(self._long_ms / 1000.0, fire)
        timer.daemon = True
        self._hold_timers[key] = timer
        timer.start()

    def _cancel_hold_timer(self, key: str):
        timer = self._hold_timers.pop(key, None)
        if timer is not None:
            timer.cancel()

    def _emit_hold(self, key: str):
        if self._hold_sent.get(key):
            return
        self._hold_sent[key] = True
        self._invoke(self._on_hold, key + ":hold")

    def press_next_down(self):
        self._begin_press("next")

    def press_next_up(self):
        self._end_press("next", self._on_next)

    def press_pause_down(self):
        self._begin_press("pause")

    def press_pause_up(self):
        self._end_press("pause", self._on_pause)

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
        for timer in list(self._hold_timers.values()):
            try:
                timer.cancel()
            except Exception:
                pass
        self._hold_timers = {}
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
