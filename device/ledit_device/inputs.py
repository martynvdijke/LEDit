"""Input hub: collects local hardware events and forwards them to the server.

Events are encoded as ``{"type": "input", "source": ..., "event": ...,
"value": ..., "ts": ...}`` per the device protocol v2 ``inputs`` capability.
The hub is intentionally best-effort: every optional source is built in its
own try/except so a missing library or broken wiring degrades to a single log
line without affecting the other sources.

Capability gating happens on the transport side (`Client.send_input_json`):
when the server did not advertise ``inputs``, button events are mapped to the
legacy ``{"action": ...}`` messages and everything else is dropped.
"""

from __future__ import annotations

import json
import threading
import time

from . import config
from .config import log

# Closed source vocabulary shared with the server (handlers/input_events.go).
SOURCE_BUTTON_NEXT = "button:next"
SOURCE_BUTTON_PAUSE = "button:pause"
SOURCE_ENCODER = "encoder"
SOURCE_NFC = "nfc"
SOURCE_PIR = "pir"
SOURCE_MMWAVE = "mmwave"
SOURCE_LUX = "lux"

# Hub-level gesture events. The server vocabulary is press/rotate/tap/
# presence/lux; "hold" only exists so the client can fall back to the legacy
# action when the inputs capability was not negotiated.
EVENT_PRESS = "press"
EVENT_HOLD = "hold"
EVENT_ROTATE = "rotate"
EVENT_TAP = "tap"
EVENT_PRESENCE = "presence"
EVENT_LUX = "lux"


class InputHub:
    """Owns the optional input sources and encodes their events."""

    def __init__(self, sender=None):
        self._sender = sender
        self._lock = threading.Lock()
        self._started = False
        self._active = True
        self._sources = []

    def set_sender(self, sender):
        with self._lock:
            self._sender = sender

    def set_active(self, active):
        """Enable/disable event emission (transport-level capability gate)."""
        with self._lock:
            self._active = bool(active)

    @property
    def started(self):
        return self._started

    def start(self):
        with self._lock:
            if self._started:
                return
            self._started = True
        if not config.inputs_enabled():
            log("inputs disabled: LEDIT_INPUTS=0")
            return
        for name, factory in self._source_factories():
            try:
                source = factory()
                if source is None:
                    continue
                source.start()
                with self._lock:
                    self._sources.append(source)
                log("input source enabled: %s", name)
            except Exception as exc:  # best-effort: one broken source is inert
                log("input source disabled: %s (%s)", name, exc)

    def _source_factories(self):
        from . import nfc as nfc_mod
        from . import rotary as rotary_mod
        from . import sensors as sensors_mod

        def rotary_factory():
            clk = config.encoder_clk_pin()
            dt = config.encoder_dt_pin()
            sw = config.encoder_sw_pin()
            if not (clk or dt or sw):
                return None
            return rotary_mod.RotaryEncoder(
                sender=self.emit,
                clk_pin=clk,
                dt_pin=dt,
                sw_pin=sw,
                debounce_ms=config.encoder_debounce_ms(),
            )

        def nfc_factory():
            if not config.nfc_enabled():
                return None
            return nfc_mod.NFCReader(
                sender=self.emit,
                dedupe_ms=config.nfc_dedupe_ms(),
            )

        def presence_factory():
            pin = config.presence_pin()
            if not pin:
                return None
            return sensors_mod.PresenceSensor(
                sender=self.emit,
                kind=config.presence_kind(),
                pin=pin,
            )

        def lux_factory():
            if not config.lux_enabled():
                return None
            return sensors_mod.LuxSensor(
                sender=self.emit,
                interval_ms=config.lux_interval_ms(),
                threshold=config.lux_change_threshold(),
            )

        return [
            ("rotary", rotary_factory),
            ("nfc", nfc_factory),
            ("presence", presence_factory),
            ("lux", lux_factory),
        ]

    def emit(self, source, event, value=None):
        """Encode one input event and hand it to the sender best-effort."""
        with self._lock:
            if not self._active:
                return
            sender = self._sender
        payload = {"type": "input", "source": source, "event": event}
        if value is not None:
            payload["value"] = value
        payload["ts"] = int(time.time() * 1000)
        if sender is None:
            return
        try:
            sender(json.dumps(payload))
        except Exception as exc:
            log("input send failed: %s", exc)

    def close(self):
        with self._lock:
            sources = self._sources
            self._sources = []
            self._started = False
        for source in sources:
            try:
                source.stop()
            except Exception as exc:
                log("input source stop failed: %s", exc)

    stop = close
