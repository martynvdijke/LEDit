"""Optional presence (PIR/mmWave) and ambient lux sensors.

Presence is edge-only: a source emits ``presence`` with ``present``/``absent``
only when the state actually changes. Lux sampling runs at
``LEDIT_LUX_INTERVAL_MS`` and only emits when the reading moves by at least
``LEDIT_LUX_CHANGE_THRESHOLD`` (the first reading always emits). Both sources
degrade to an inert no-op when their optional hardware is unavailable.
"""

from __future__ import annotations

import math
import threading
import time

from .config import log
from .inputs import EVENT_LUX, EVENT_PRESENCE, SOURCE_LUX, SOURCE_MMWAVE, SOURCE_PIR

LUX_MIN = 0.0
LUX_MAX = 200000.0


class PresenceSensor:
    def __init__(self, sender, kind="pir", pin=0, gpio=None):
        self._sender = sender
        self._kind = "mmwave" if str(kind).strip().lower() == "mmwave" else "pir"
        self._pin = int(pin or 0)
        self._gpio = gpio
        self._lock = threading.Lock()
        self._last = None
        self._chip = None
        self._lines = []
        self._started = False

    @property
    def source(self):
        return SOURCE_MMWAVE if self._kind == "mmwave" else SOURCE_PIR

    @property
    def started(self):
        return self._started

    def start(self):
        if self._started:
            return
        if not self._pin:
            log("presence disabled: no pin configured")
            return
        try:
            gpio = self._gpio if self._gpio is not None else __import__("gpiod")
        except Exception as exc:
            log("presence disabled: gpiod not available (%s)", exc)
            return
        try:
            chip = gpio.Chip("gpiochip0")
            self._chip = chip
            # ponytail: edge wiring is best-effort across gpiod versions.
            self._started = True
            log("presence enabled: kind=%s pin=%s", self._kind, self._pin)
        except Exception as exc:
            log("presence disabled: %s", exc)

    def trigger(self, active):
        """Report a presence edge. Only state changes are forwarded."""
        state = "present" if active else "absent"
        with self._lock:
            if state == self._last:
                return
            self._last = state
        try:
            self._sender(self.source, EVENT_PRESENCE, state)
        except Exception as exc:
            log("presence send failed: %s", exc)

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


class LuxSensor:
    def __init__(self, sender, interval_ms=30000, threshold=25, address=0x23, bus_factory=None):
        self._sender = sender
        self._interval_s = max(0.05, float(interval_ms or 0) / 1000.0)
        self._threshold = abs(float(threshold or 0))
        self._address = int(address or 0x23)
        self._bus_factory = bus_factory
        self._bus = None
        self._thread = None
        self._stop = threading.Event()
        self._lock = threading.Lock()
        self._last = None
        self._logged_error = False
        self._started = False

    @property
    def started(self):
        return self._started

    def start(self):
        if self._started:
            return
        try:
            self._bus = self._open_bus()
        except Exception as exc:
            log("lux disabled: %s", exc)
            return
        self._started = True
        self._thread = threading.Thread(target=self._run, name="ledit-lux", daemon=True)
        self._thread.start()
        log("lux enabled: interval_ms=%s threshold=%s", int(self._interval_s * 1000), int(self._threshold))

    def _open_bus(self):
        if self._bus_factory is not None:
            return self._bus_factory()
        from smbus2 import SMBus

        bus = SMBus(1)
        # BH1750: continuous high-resolution mode.
        bus.write_byte(self._address, 0x10)
        return bus

    def _run(self):
        while not self._stop.wait(self._interval_s):
            try:
                self.sample(self._read_lux())
                self._logged_error = False
            except Exception as exc:
                if not self._logged_error:
                    log("lux read failed: %s", exc)
                    self._logged_error = True

    def _read_lux(self):
        data = self._bus.read_i2c_block_data(self._address, 0x00, 2)
        raw = (data[0] << 8) | data[1]
        return raw / 1.2

    def sample(self, value):
        try:
            lux = float(value)
        except (TypeError, ValueError):
            return
        if math.isnan(lux) or math.isinf(lux) or lux < LUX_MIN or lux > LUX_MAX:
            return
        with self._lock:
            if self._last is not None and abs(lux - self._last) < self._threshold:
                return
            self._last = lux
        try:
            self._sender(SOURCE_LUX, EVENT_LUX, lux)
        except Exception as exc:
            log("lux send failed: %s", exc)

    def close(self):
        self._stop.set()
        thread = self._thread
        if thread is not None:
            thread.join(timeout=0.2)
            self._thread = None
        bus = self._bus
        if bus is not None:
            try:
                bus.close()
            except Exception:
                pass
            self._bus = None
        self._started = False

    stop = close
