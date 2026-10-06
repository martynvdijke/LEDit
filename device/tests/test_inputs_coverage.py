"""Coverage-focused tests for the physical input modules (task 7.2)."""

import builtins
import logging
import time

from ledit_device import config, inputs, nfc, rotary, sensors


class GPIOChip:
    def __init__(self):
        self.closed = False

    def close(self):
        self.closed = True


class FakeGPIO:
    def __init__(self, raise_on_chip=False):
        self.raise_on_chip = raise_on_chip
        self.chip = None

    def Chip(self, name):  # noqa: N802 - mirrors gpiod API
        if self.raise_on_chip:
            raise RuntimeError("chip unavailable")
        self.chip = GPIOChip()
        return self.chip


class FakeBus:
    def __init__(self, sample=(0x01, 0x40), raise_on_read=False):
        self.sample = sample
        self.raise_on_read = raise_on_read
        self.closed = False

    def write_byte(self, address, value):
        pass

    def read_i2c_block_data(self, address, command, length):
        if self.raise_on_read:
            raise OSError("i2c read failed")
        return list(self.sample)

    def close(self):
        self.closed = True


class FakeTag:
    def __init__(self, identifier):
        self.identifier = identifier


class FakeDriver:
    def __init__(self, uid=b"\x04\xa1\xb2\xc3"):
        self.uid = uid
        self.closed = False

    def read_passive_target(self, timeout=0.5):
        return FakeTag(self.uid)

    def close(self):
        self.closed = True


class FakeSource:
    def __init__(self, stop_error=False):
        self.stop_called = False
        self.stop_error = stop_error

    def start(self):
        pass

    def stop(self):
        self.stop_called = True
        if self.stop_error:
            raise RuntimeError("stop boom")


def _recorder():
    events = []

    def sender(source, event, value=None):
        events.append((source, event, value))

    return events, sender


def _missing_gpiod(monkeypatch):
    real_import = builtins.__import__

    def fake_import(name, *args, **kwargs):
        if name == "gpiod":
            raise ImportError("gpiod not installed")
        return real_import(name, *args, **kwargs)

    monkeypatch.setattr(builtins, "__import__", fake_import)


def _has_log(caplog, needle):
    return any(needle in record.getMessage() for record in caplog.records)


def test_rotary_start_close_and_failures(monkeypatch, caplog):
    events, sender = _recorder()
    gpio = FakeGPIO()
    enc = rotary.RotaryEncoder(sender, clk_pin=5, dt_pin=6, sw_pin=13, gpio=gpio)
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        enc.start()
        assert enc.started
        assert _has_log(caplog, "rotary enabled")
    enc.rotate(1)
    enc.press()
    assert ("encoder", "rotate", 1) in events
    assert ("encoder", "press", None) in events
    enc.close()
    assert not enc.started
    assert gpio.chip.closed

    caplog.clear()
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        broken = rotary.RotaryEncoder(sender, clk_pin=5, dt_pin=6,
                                      gpio=FakeGPIO(raise_on_chip=True))
        broken.start()
    assert not broken.started
    assert _has_log(caplog, "rotary disabled")

    caplog.clear()
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        no_pins = rotary.RotaryEncoder(sender)
        no_pins.start()
    assert _has_log(caplog, "no encoder pins")

    _missing_gpiod(monkeypatch)
    caplog.clear()
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        no_lib = rotary.RotaryEncoder(sender, clk_pin=5, dt_pin=6, gpio=None)
        no_lib.start()
    assert _has_log(caplog, "gpiod not available")


def test_presence_start_edges_and_failures(monkeypatch, caplog):
    events, sender = _recorder()
    gpio = FakeGPIO()
    pir = sensors.PresenceSensor(sender, kind="pir", pin=17, gpio=gpio)
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        pir.start()
        assert pir.started
        assert _has_log(caplog, "presence enabled")
    assert pir.source == "pir"
    pir.trigger(True)
    pir.trigger(True)  # edge-only
    pir.trigger(False)
    assert events == [("pir", "presence", "present"), ("pir", "presence", "absent")]
    pir.close()
    assert gpio.chip.closed

    mm, sender2 = _recorder()
    wave = sensors.PresenceSensor(sender2, kind="mmwave", pin=18, gpio=FakeGPIO())
    assert wave.source == "mmwave"
    wave.trigger(True)
    assert mm == [("mmwave", "presence", "present")]

    caplog.clear()
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        no_pin = sensors.PresenceSensor(sender)
        no_pin.start()
        assert _has_log(caplog, "no pin configured")
        broken = sensors.PresenceSensor(sender, pin=17, gpio=FakeGPIO(raise_on_chip=True))
        broken.start()
        assert _has_log(caplog, "presence disabled")

    _missing_gpiod(monkeypatch)
    caplog.clear()
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        no_lib = sensors.PresenceSensor(sender, pin=17, gpio=None)
        no_lib.start()
    assert _has_log(caplog, "gpiod not available")


def test_lux_start_read_loop_and_failures(caplog):
    events, sender = _recorder()
    bus = FakeBus(sample=(0x01, 0x40))
    lux = sensors.LuxSensor(sender, interval_ms=50, threshold=1, bus_factory=lambda: bus)
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        lux.start()
        assert lux.started
        assert _has_log(caplog, "lux enabled")
    assert lux._read_lux() == 320 / 1.2
    deadline = time.time() + 2
    while not events and time.time() < deadline:
        time.sleep(0.02)
    assert events and events[0][0] == "lux"
    lux.close()
    assert not lux.started
    assert bus.closed

    caplog.clear()
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        def raising_factory():
            raise OSError("no bus")

        disabled = sensors.LuxSensor(sender, bus_factory=raising_factory)
        disabled.start()
    assert not disabled.started
    assert _has_log(caplog, "lux disabled")


def test_nfc_start_loop_dedupe_and_close(caplog):
    events, sender = _recorder()
    driver = FakeDriver(uid=b"\x04\xA1\xB2\xC3")
    reader = nfc.NFCReader(sender, driver_factory=lambda: driver)
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        reader.start()
        assert reader.started
    deadline = time.time() + 2
    while not events and time.time() < deadline:
        time.sleep(0.02)
    assert events == [("nfc", "tap", "04a1b2c3")]
    reader.handle_uid("04A1B2C3")  # deduped (same uid within window)
    assert len(events) == 1
    reader.close()
    assert not reader.started
    assert driver.closed

    caplog.clear()
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        def raising_factory():
            raise OSError("no reader")

        disabled = nfc.NFCReader(sender, driver_factory=raising_factory)
        disabled.start()
    assert not disabled.started
    assert _has_log(caplog, "nfc disabled")


def test_input_hub_factory_gating(monkeypatch, caplog):
    monkeypatch.setattr(config, "encoder_clk_pin", lambda: 0)
    monkeypatch.setattr(config, "encoder_dt_pin", lambda: 0)
    monkeypatch.setattr(config, "encoder_sw_pin", lambda: 0)
    monkeypatch.setattr(config, "nfc_enabled", lambda: False)
    monkeypatch.setattr(config, "presence_pin", lambda: 0)
    monkeypatch.setattr(config, "lux_enabled", lambda: False)

    hub = inputs.InputHub(sender=None)
    with caplog.at_level(logging.INFO, logger="ledit_device"):
        hub.start()
    assert hub.started
    assert list(hub._sources) == []
    hub.emit("nfc", "tap", "04a1b2c3")  # sender None: no crash
    hub.close()

    source = FakeSource(stop_error=True)
    hub2 = inputs.InputHub(sender=None)
    hub2._sources.append(source)
    hub2._started = True
    hub2.close()
    assert source.stop_called


def test_client_send_input_json_edges(client):
    class FakeWS:
        def __init__(self):
            self.sent = []
            self.raise_on_send = False

        def send(self, payload):
            if self.raise_on_send:
                raise RuntimeError("closed")
            self.sent.append(payload)

    client._inputs_cap = True
    client._ws = None
    client.send_input_json('{"type":"input","source":"nfc","event":"tap"}')

    client._inputs_cap = False
    client.send_input_json("not json")
    client.send_input_json("[1, 2]")
    client.send_input_json('{"source":"lux","event":"lux"}')

    sink = FakeWS()
    client._ws = sink
    sink.raise_on_send = True
    client.send_input_json('{"source":"button:next","event":"press"}')
    sink.raise_on_send = False
    client.send_input_json('{"source":"button:next","event":"press"}')
    assert sink.sent == ['{"action": "next"}']
