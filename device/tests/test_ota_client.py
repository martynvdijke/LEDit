import json
from unittest import mock

def test_hello_payload_contains_version_and_ota():
    from ledit_device.client import _hello_payload
    p = _hello_payload()
    assert p["type"] == "hello"
    assert "ota" in p["capabilities"]
    assert "version" in p

def test_on_open_sends_hello(tmp_path):
    from ledit_device.client import Client
    from ledit_device.display import FileDisplay
    c = Client(FileDisplay(str(tmp_path), 8, 8))
    ws = mock.MagicMock()
    c.on_open(ws)
    assert ws.send.called
    payload = json.loads(ws.send.call_args[0][0])
    assert payload["type"] == "hello"
    c.close()

def test_welcome_triggers_ota_when_capable(tmp_path, monkeypatch):
    from ledit_device.client import Client
    from ledit_device.display import FileDisplay
    monkeypatch.setenv("LEDIT_UPDATE_INTERVAL", "3600")
    c = Client(FileDisplay(str(tmp_path), 8, 8))
    # mock ensure to avoid thread
    with mock.patch.object(c, "_ensure_ota_thread") as m:
        c._handle_welcome({"protocol": 2, "capabilities": ["ota"]})
        m.assert_called_once()
    c.close()

def test_welcome_no_ota_when_interval_zero(tmp_path, monkeypatch):
    from ledit_device.client import Client
    from ledit_device.display import FileDisplay
    monkeypatch.setenv("LEDIT_UPDATE_INTERVAL", "0")
    c = Client(FileDisplay(str(tmp_path), 8, 8))
    with mock.patch.object(c, "_ensure_ota_thread") as m:
        c._handle_welcome({"protocol": 2, "capabilities": ["ota"]})
        m.assert_not_called()
    c.close()

def test_main_provisioning_path(monkeypatch, tmp_path):
    monkeypatch.setenv("LEDIT_TOKEN", "")
    monkeypatch.delenv("LEDIT_TOKEN", raising=False)
    monkeypatch.setenv("LEDIT_CONFIG_DIR", str(tmp_path))
    monkeypatch.setenv("LEDIT_SERVER", "ws://example:8080")
    mock_ws_instance = mock.MagicMock()
    mock_ws_cls = mock.MagicMock(return_value=mock_ws_instance)
    mock_display = mock.MagicMock(width=64, height=32)
    mock_telemetry = mock.MagicMock()
    with mock.patch("ledit_device.__main__.websocket.WebSocketApp", mock_ws_cls), \
         mock.patch("ledit_device.__main__.make_display", return_value=mock_display), \
         mock.patch("ledit_device.__main__.Client", return_value=mock.MagicMock()), \
         mock.patch("ledit_device.__main__.init_telemetry", return_value=mock_telemetry), \
         mock.patch("ledit_device.discovery.start_advertising", return_value=None), \
         mock.patch("ledit_device.discovery.provision", return_value="newtok123"):
        from ledit_device.__main__ import main
        main()
        args,_ = mock_ws_cls.call_args
        assert "newtok123" in args[0]
