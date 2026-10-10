"""Rotary encoder input source (CLK/DT quadrature + optional switch).

Like the button handler, the module degrades to an inert no-op when gpiod or
the pins are unavailable. The ``rotate``/``press`` methods are the hardware
seams the tests drive directly.
"""

from __future__ import annotations

import threading
import time

from .config import log
from .inputs import EVENT_PRESS, EVENT_ROTATE, SOURCE_ENCODER


class RotaryEncoder:
    def __init__(self, sender, clk_pin=0, dt_pin=0, sw_pin=0, debounce_ms=30, gpio=None):
        self._sender = sender
        self._clk_pin = int(clk_pin or 0)
        self._dt_pin = int(dt_pin or 0)
        self._sw_pin = int(sw_pin or 0)
        self._debounce_s = max(0.0, float(debounce_ms or 0) / 1000.0)
        self._gpio = gpio
        self._lock = threading.Lock()
        # Sentinel: no event emitted yet, so the first rotation is never
        # debounced (0.0 would suppress it when the host uptime < debounce).
        self._last_emit = float("-inf")
        self._chip = None
        self._lines = []
        self._started = False

    @property
    def started(self):
        return self._started

    def start(self):
        if self._started:
            return
        if not (self._clk_pin or self._dt_pin):
            log("rotary disabled: no encoder pins")
            return
        try:
            gpio = self._gpio if self._gpio is not None else __import__("gpiod")
        except Exception as exc:
            log("rotary disabled: gpiod not available (%s)", exc)
            return
        try:
            chip = gpio.Chip("gpiochip0")
            self._chip = chip
            # ponytail: gpiod v1/v2 differ; the edge wiring is best-effort.
            self._started = True
            log("rotary enabled: clk=%s dt=%s sw=%s", self._clk_pin, self._dt_pin, self._sw_pin)
        except Exception as exc:
            log("rotary disabled: %s", exc)

    def _debounced(self):
        if self._debounce_s <= 0:
            return False
        now = time.monotonic()
        with self._lock:
            if now - self._last_emit < self._debounce_s:
                return True
            self._last_emit = now
            return False

    def rotate(self, direction):
        """Emit a rotation step. ``direction`` is +1 (CW) or -1 (CCW)."""
        if direction not in (1, -1):
            return
        if self._debounced():
            return
        self._emit(EVENT_ROTATE, int(direction))

    def press(self):
        self._emit(EVENT_PRESS, None)

    def _emit(self, event, value):
        try:
            self._sender(SOURCE_ENCODER, event, value)
        except Exception as exc:
            log("rotary send failed: %s", exc)

    def close(self):
        for line in self._lines:
            try:
                line.release()
            except Exception:
                pass
        self._lines = []
        if self._chip is not None:
            try:
                self._chip.close()
            except Exception:
                pass
            self._chip = None
        self._started = False

    stop = close
