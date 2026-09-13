import json
import logging
import sys
import time
from unittest import mock

import pytest

from ledit_device.buttons import ButtonHandler


def test_callback_invocation_without_hardware(monkeypatch):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    monkeypatch.setenv("LEDIT_BTN_PAUSE_PIN", "6")
    called = []
    bh = ButtonHandler(on_next=lambda: called.append("next"), on_pause=lambda: called.append("pause"))
    bh.press_next()
    assert called == ["next"]
    # reset debounce for next press
    bh._last_press.clear()
    bh.press_pause()
    assert called == ["next", "pause"]


def test_sender_default(monkeypatch):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "4")
    sent = []
    bh = ButtonHandler(sender=lambda msg: sent.append(json.loads(msg)))
    bh.press_next()
    assert sent[0] == {"action": "next"}
    bh._last_press.clear()
    bh.press_pause()
    assert sent[1] == {"action": "pause"}


def test_noop_when_gpiod_missing_unconfigured_pins(monkeypatch, caplog):
    monkeypatch.delenv("LEDIT_BTN_NEXT_PIN", raising=False)
    monkeypatch.delenv("LEDIT_BTN_PAUSE_PIN", raising=False)
    # Ensure gpiod not importable
    monkeypatch.delitem(sys.modules, "gpiod", raising=False)
    bh = ButtonHandler(on_next=lambda: None)
    with caplog.at_level(logging.INFO, logger="ledit_device.buttons"):
        caplog.clear()
        bh.start()
    # exactly one log record
    records = [r for r in caplog.records if r.name == "ledit_device.buttons"]
    assert len(records) == 1
    assert "no pins" in records[0].message.lower() or "disabled" in records[0].message.lower()


def test_noop_when_gpiod_missing_but_pins_configured(monkeypatch, caplog):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    monkeypatch.delenv("LEDIT_BTN_PAUSE_PIN", raising=False)
    monkeypatch.delitem(sys.modules, "gpiod", raising=False)
    # Ensure import fails: if gpiod installed, hide it via monkeypatch
    import builtins
    orig_import = builtins.__import__

    def fake_import(name, *a, **kw):
        if name == "gpiod":
            raise ImportError("no gpiod")
        return orig_import(name, *a, **kw)

    monkeypatch.setattr(builtins, "__import__", fake_import)
    bh = ButtonHandler(on_next=lambda: None)
    with caplog.at_level(logging.INFO, logger="ledit_device.buttons"):
        caplog.clear()
        bh.start()
    records = [r for r in caplog.records if r.name == "ledit_device.buttons"]
    assert len(records) == 1
    assert "gpiod" in records[0].message.lower()


def test_debounce_coalescing(monkeypatch):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    count = []
    bh = ButtonHandler(on_next=lambda: count.append(1))
    bh.press_next()
    bh.press_next()  # within 20ms -> debounced
    assert len(count) == 1
    # after 30ms should trigger again
    time.sleep(0.03)
    bh.press_next()
    assert len(count) == 2


def test_ws_disconnected_press_ignored(monkeypatch, caplog):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")

    def bad_sender(msg):
        raise RuntimeError("disconnected")

    bh = ButtonHandler(sender=bad_sender)
    # should not propagate
    with caplog.at_level(logging.WARNING, logger="ledit_device.buttons"):
        bh.press_next()
    # no exception, same for second call debounced vs not
    bh._last_press.clear()
    with caplog.at_level(logging.WARNING, logger="ledit_device.buttons"):
        bh.press_next()  # again, should be caught


def test_env_parsing(monkeypatch):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "notanint")
    monkeypatch.setenv("LEDIT_BTN_PAUSE_PIN", "")
    bh = ButtonHandler()
    assert bh._next_pin == 0
    assert bh._pause_pin == 0
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "0")
    monkeypatch.setenv("LEDIT_BTN_PAUSE_PIN", "12")
    bh2 = ButtonHandler()
    assert bh2._next_pin == 0
    assert bh2._pause_pin == 12


def test_non_posix_noop(monkeypatch, caplog):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    # Patch the module seam, NOT os.name: mutating os.name flips
    # pathlib.Path flavour on Python 3.12+ and corrupts pytest internals.
    monkeypatch.setattr("ledit_device.buttons._platform", lambda: "nt")
    bh = ButtonHandler(on_next=lambda: None)
    with caplog.at_level(logging.INFO, logger="ledit_device.buttons"):
        caplog.clear()
        bh.start()
    records = [r for r in caplog.records if r.name == "ledit_device.buttons"]
    assert len(records) == 1


def test_stop_close_swallow(monkeypatch):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    bh = ButtonHandler(on_next=lambda: None)
    bh._chip = mock.MagicMock()
    bh._chip.close.side_effect = RuntimeError("fail")
    line = mock.MagicMock()
    line.release.side_effect = RuntimeError("fail")
    bh._lines = [line]
    # should not raise
    bh.close()
    bh.stop()


def test_no_callback_noop(monkeypatch):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    bh = ButtonHandler()
    # neither callback nor sender -> press does nothing but not error
    bh.press_next()
    bh.press_pause()


def test_sender_with_custom_on_next(monkeypatch):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    called = []
    sent = []
    bh = ButtonHandler(on_next=lambda: called.append(1), sender=lambda m: sent.append(m))
    bh.press_next()
    assert called == [1]
    assert sent == []
    bh._last_press.clear()
    # pause should still use sender default
    bh.press_pause()
    assert len(sent) == 1


def test_setup_alias(monkeypatch, caplog):
    monkeypatch.delenv("LEDIT_BTN_NEXT_PIN", raising=False)
    monkeypatch.delenv("LEDIT_BTN_PAUSE_PIN", raising=False)
    bh = ButtonHandler()
    with caplog.at_level(logging.INFO, logger="ledit_device.buttons"):
        bh.setup()
    assert any("disabled" in r.message.lower() for r in caplog.records)


def _gesture_handler(monkeypatch, short_ms=300, long_ms=600, repeat_ms=0):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    monkeypatch.setenv("LEDIT_BUTTON_SHORT_MS", str(short_ms))
    monkeypatch.setenv("LEDIT_BUTTON_LONG_MS", str(long_ms))
    monkeypatch.setenv("LEDIT_BUTTON_HOLD_REPEAT_MS", str(repeat_ms))
    sent = []
    bh = ButtonHandler(sender=lambda m: sent.append(json.loads(m)))
    return bh, sent


def test_short_press_emits_next_on_release(monkeypatch):
    bh, sent = _gesture_handler(monkeypatch)
    bh.press_next_down()
    time.sleep(0.05)
    bh.press_next_up()
    assert sent == [{"action": "next"}]
    bh.close()


def test_short_press_pause(monkeypatch):
    bh, sent = _gesture_handler(monkeypatch)
    bh.press_pause_down()
    time.sleep(0.05)
    bh.press_pause_up()
    assert sent == [{"action": "pause"}]
    bh.close()


def test_long_press_emits_hold_not_next(monkeypatch):
    bh, sent = _gesture_handler(monkeypatch)
    bh.press_next_down()
    time.sleep(0.65)
    bh.press_next_up()
    actions = [m["action"] for m in sent]
    assert "hold" in actions
    assert "next" not in actions
    bh.close()


def test_configurable_thresholds_respected(monkeypatch):
    bh, sent = _gesture_handler(monkeypatch, short_ms=300, long_ms=600)
    # 400ms press: below the long threshold -> short next.
    bh.press_next_down()
    time.sleep(0.4)
    bh.press_next_up()
    assert sent == [{"action": "next"}]

    # 700ms hold: at/after the long threshold -> hold.
    bh._last_press.clear()
    bh.press_next_down()
    time.sleep(0.7)
    bh.press_next_up()
    assert any(m["action"] == "hold" for m in sent)
    bh.close()


def test_hold_repeat(monkeypatch):
    bh, sent = _gesture_handler(monkeypatch, short_ms=100, long_ms=200, repeat_ms=50)
    bh.press_next_down()
    time.sleep(0.5)
    bh.press_next_up()
    holds = [m for m in sent if m["action"] == "hold"]
    assert len(holds) >= 2
    bh.close()


def test_default_hold_sender_used(monkeypatch):
    monkeypatch.setenv("LEDIT_BTN_NEXT_PIN", "5")
    monkeypatch.setenv("LEDIT_BUTTON_SHORT_MS", "50")
    monkeypatch.setenv("LEDIT_BUTTON_LONG_MS", "100")
    sent = []
    bh = ButtonHandler(sender=lambda m: sent.append(json.loads(m)))
    bh.press_next_down()
    time.sleep(0.15)
    bh.press_next_up()
    assert {"action": "hold"} in sent
    bh.close()


def test_down_debounce(monkeypatch):
    bh, sent = _gesture_handler(monkeypatch)
    bh.press_next_down()
    bh.press_next_down()  # within debounce -> ignored
    bh.press_next_up()
    assert sent == [{"action": "next"}]
    bh.close()


def test_close_cancels_pending_timers(monkeypatch):
    bh, sent = _gesture_handler(monkeypatch, long_ms=50)
    bh.press_next_down()
    bh.close()
    time.sleep(0.1)
    assert sent == []
