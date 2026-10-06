"""Tests for device input sources: hub encoding, NFC, rotary, sensors, buttons wiring."""

import json
import math

from ledit_device import inputs as inputs_mod
from ledit_device import nfc
from ledit_device import rotary
from ledit_device import sensors
from ledit_device.buttons import ButtonHandler


class FakeSource:
    def __init__(self, fail_start=False):
        self.started = False
        self.stopped = False
        self.fail_start = fail_start

    def start(self):
        if self.fail_start:
            raise RuntimeError("boom")
        self.started = True

    def stop(self):
        self.stopped = True


class FakeWS:
    def __init__(self):
        self.sent = []

    def send(self, payload):
        self.sent.append(payload)


def collect_hub():
    sent = []
    hub = inputs_mod.InputHub(sender=sent.append)
    return hub, sent


def test_hub_emit_encoding():
    hub, sent = collect_hub()
    hub.emit(inputs_mod.SOURCE_NFC, inputs_mod.EVENT_TAP, "04A1B2")
    assert len(sent) == 1
    data = json.loads(sent[0])
    assert data["type"] == "input"
    assert data["source"] == "nfc"
    assert data["event"] == "tap"
    assert data["value"] == "04A1B2"
    assert isinstance(data["ts"], int)


def test_hub_active_gate():
    hub, sent = collect_hub()
    hub.set_active(False)
    hub.emit(inputs_mod.SOURCE_BUTTON_NEXT, inputs_mod.EVENT_PRESS)
    assert sent == []
    hub.set_active(True)
    hub.emit(inputs_mod.SOURCE_BUTTON_NEXT, inputs_mod.EVENT_PRESS)
    assert len(sent) == 1
    assert "value" not in json.loads(sent[0])


def test_hub_sender_failure_is_swallowed():
    def boom(_payload):
        raise RuntimeError("socket gone")

    hub = inputs_mod.InputHub(sender=boom)
    hub.emit(inputs_mod.SOURCE_ENCODER, inputs_mod.EVENT_ROTATE, 1)


def test_hub_start_disabled_by_env(monkeypatch):
    monkeypatch.setenv("LEDIT_INPUTS", "0")
    hub, _ = collect_hub()
    hub.start()
    assert hub.started is True
    assert hub._sources == []


def test_hub_broken_source_does_not_break_others(monkeypatch, caplog):
    hub, _ = collect_hub()
    broken = FakeSource(fail_start=True)
    good = FakeSource()
    monkeypatch.setattr(
        hub,
        "_source_factories",
        lambda: [("broken", lambda: broken), ("good", lambda: good)],
    )
    hub.start()
    assert broken.started is False
    assert good.started is True
    assert "input source disabled" in caplog.text
    hub.close()
    assert good.stopped is True


def test_hub_close_stops_sources():
    hub, _ = collect_hub()
    src = FakeSource()
    hub._sources = [src]
    hub.close()
    assert src.stopped is True


def test_client_send_input_json_negotiated(client):
    client._inputs_cap = True
    ws = FakeWS()
    client._ws = ws
    payload = '{"type":"input","source":"nfc","event":"tap","value":"04"}'
    client.send_input_json(payload)
    assert ws.sent == [payload]


def test_client_send_input_json_legacy_mapping(client):
    client._inputs_cap = False
    ws = FakeWS()
    client._ws = ws

    client.send_input_json(
        json.dumps({"type": "input", "source": "button:next", "event": "press"})
    )
    client.send_input_json(
        json.dumps({"type": "input", "source": "button:pause", "event": "press"})
    )
    client.send_input_json(
        json.dumps({"type": "input", "source": "nfc", "event": "tap", "value": "04"})
    )
    assert [json.loads(p) for p in ws.sent] == [
        {"action": "next"},
        {"action": "pause"},
    ]


def test_client_send_input_json_hold_maps_to_hold(client):
    client._inputs_cap = False
    ws = FakeWS()
    client._ws = ws
    client.send_input_json(
        json.dumps({"type": "input", "source": "button:next", "event": "hold"})
    )
    assert json.loads(ws.sent[0]) == {"action": "hold"}


def test_client_welcome_tracks_inputs_capability(client):
    class FakeHub:
        def __init__(self):
            self.active = None

        def set_active(self, active):
            self.active = active

        def close(self):
            pass

    hub = FakeHub()
    client.set_input_hub(hub)
    client._handle_welcome(
        {"type": "welcome", "protocol": 2, "capabilities": ["brightness", "inputs"]}
    )
    assert client._inputs_cap is True
    assert hub.active is True


def test_client_close_closes_hub(client):
    class FakeHub:
        def __init__(self):
            self.closed = False

        def close(self):
            self.closed = True

    hub = FakeHub()
    client.set_input_hub(hub)
    client.close()
    assert hub.closed is True


def test_nfc_normalize_uid():
    assert nfc.normalize_uid(b"\x04\xa1\xb2") == "04a1b2"
    assert nfc.normalize_uid("04:A1:B2") == "04a1b2"
    assert nfc.normalize_uid(" 04-a1-b2 ") == "04a1b2"


def test_nfc_handle_uid_dedupe():
    sent = []
    reader = nfc.NFCReader(
        sender=lambda *args: sent.append(args), dedupe_ms=60000
    )
    reader.handle_uid("04:A1:B2")
    reader.handle_uid("04a1b2")
    assert len(sent) == 1
    assert sent[0] == (inputs_mod.SOURCE_NFC, inputs_mod.EVENT_TAP, "04a1b2")


def test_nfc_missing_driver_is_inert(caplog):
    reader = nfc.NFCReader(
        sender=lambda *args: None,
        driver_factory=lambda: (_ for _ in ()).throw(ImportError("no nfcpy")),
    )
    reader.start()
    assert "nfc disabled" in caplog.text
    reader.close()


def test_rotary_rotate_and_press():
    sent = []
    enc = rotary.RotaryEncoder(
        sender=lambda *args: sent.append(args),
        clk_pin=1,
        dt_pin=2,
        debounce_ms=0,
    )
    enc.rotate(1)
    enc.rotate(-1)
    enc.rotate(0)
    enc.rotate(5)
    enc.press()
    assert sent == [
        (inputs_mod.SOURCE_ENCODER, inputs_mod.EVENT_ROTATE, 1),
        (inputs_mod.SOURCE_ENCODER, inputs_mod.EVENT_ROTATE, -1),
        (inputs_mod.SOURCE_ENCODER, inputs_mod.EVENT_PRESS, None),
    ]


def test_rotary_debounce():
    sent = []
    enc = rotary.RotaryEncoder(
        sender=lambda *args: sent.append(args),
        clk_pin=1,
        dt_pin=2,
        debounce_ms=60000,
    )
    enc.rotate(1)
    enc.rotate(1)
    assert len(sent) == 1


def test_rotary_missing_gpiod_is_inert(caplog):
    enc = rotary.RotaryEncoder(
        sender=lambda *args: None,
        clk_pin=1,
        dt_pin=2,
        gpio=None,
    )
    # Simulate missing library by breaking the import used in start().
    import builtins

    real_import = builtins.__import__

    def fake_import(name, *args, **kwargs):
        if name == "gpiod":
            raise ImportError("no gpiod")
        return real_import(name, *args, **kwargs)

    builtins.__import__ = fake_import
    try:
        enc.start()
    finally:
        builtins.__import__ = real_import
    assert "rotary disabled" in caplog.text


def test_presence_edge_only():
    sent = []
    ps = sensors.PresenceSensor(
        sender=lambda *args: sent.append(args), kind="mmwave", pin=3
    )
    assert ps.source == inputs_mod.SOURCE_MMWAVE
    ps.trigger(True)
    ps.trigger(True)
    ps.trigger(False)
    ps.trigger(False)
    assert sent == [
        (inputs_mod.SOURCE_MMWAVE, inputs_mod.EVENT_PRESENCE, "present"),
        (inputs_mod.SOURCE_MMWAVE, inputs_mod.EVENT_PRESENCE, "absent"),
    ]


def test_presence_kind_defaults_to_pir():
    ps = sensors.PresenceSensor(sender=lambda *args: None, kind="strange", pin=1)
    assert ps.source == inputs_mod.SOURCE_PIR


def test_lux_threshold_and_range():
    sent = []
    ls = sensors.LuxSensor(sender=lambda *args: sent.append(args), threshold=25)
    ls.sample(100)
    ls.sample(110)
    ls.sample(130)
    ls.sample(-1)
    ls.sample(1e12)
    ls.sample(float("nan"))
    assert sent == [
        (inputs_mod.SOURCE_LUX, inputs_mod.EVENT_LUX, 100.0),
        (inputs_mod.SOURCE_LUX, inputs_mod.EVENT_LUX, 130.0),
    ]
    assert math.isfinite(sent[0][2])


def test_buttons_hub_mode_emits_input_events():
    hub, sent = collect_hub()
    bh = ButtonHandler(hub=hub)
    bh._last_press.clear()
    bh.press_next()
    data = json.loads(sent[0])
    assert data["source"] == "button:next"
    assert data["event"] == "press"

    bh._last_press.clear()
    bh.press_pause()
    data = json.loads(sent[1])
    assert data["source"] == "button:pause"
    assert data["event"] == "press"


def test_buttons_hub_mode_hold_emits_hold_event():
    hub, sent = collect_hub()
    bh = ButtonHandler(hub=hub)
    bh._hold_sent["next"] = False
    bh._emit_hold("next")
    data = json.loads(sent[0])
    assert data["source"] == "button:next"
    assert data["event"] == "hold"
