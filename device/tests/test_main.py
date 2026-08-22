from unittest import mock

import pytest


def test_main_builds_url_and_wiring(monkeypatch):
    monkeypatch.setenv("LEDIT_TOKEN", "abc")
    monkeypatch.setenv("LEDIT_SERVER", "ws://example:8080/")
    monkeypatch.setenv("LEDIT_PREVIEW_DIR", "/tmp/preview_test_main")

    mock_ws_instance = mock.MagicMock()
    mock_ws_cls = mock.MagicMock(return_value=mock_ws_instance)
    mock_display = mock.MagicMock(width=64, height=32)
    mock_client_instance = mock.MagicMock()
    mock_telemetry = mock.MagicMock()

    with mock.patch("ledit_device.__main__.websocket.WebSocketApp", mock_ws_cls) as ws_cls, \
         mock.patch("ledit_device.__main__.make_display", return_value=mock_display), \
         mock.patch("ledit_device.__main__.Client", return_value=mock_client_instance) as mock_client_cls, \
         mock.patch("ledit_device.__main__.init_telemetry", return_value=mock_telemetry):
        from ledit_device.__main__ import main

        main()

        # URL built from token and server (slash stripped)
        ws_cls.assert_called_once()
        args, kwargs = ws_cls.call_args
        assert args[0] == "ws://example:8080/ws/device/abc"
        assert kwargs["on_message"] == mock_client_instance.on_message
        assert kwargs["on_error"] == mock_client_instance.on_error
        assert kwargs["on_close"] == mock_client_instance.on_close
        assert kwargs["on_reconnect"] == mock_client_instance.on_reconnect

        mock_ws_instance.run_forever.assert_called_once_with(ping_interval=30, ping_timeout=10, reconnect=5)
        mock_telemetry.shutdown.assert_called_once()


def test_main_shutdown_on_exception(monkeypatch):
    monkeypatch.setenv("LEDIT_TOKEN", "abc")
    monkeypatch.setenv("LEDIT_SERVER", "ws://localhost:8080")
    mock_ws_instance = mock.MagicMock()
    mock_ws_instance.run_forever.side_effect = RuntimeError("boom")
    mock_ws_cls = mock.MagicMock(return_value=mock_ws_instance)
    mock_display = mock.MagicMock(width=64, height=64)
    mock_telemetry = mock.MagicMock()

    with mock.patch("ledit_device.__main__.websocket.WebSocketApp", mock_ws_cls), \
         mock.patch("ledit_device.__main__.make_display", return_value=mock_display), \
         mock.patch("ledit_device.__main__.Client", return_value=mock.MagicMock()), \
         mock.patch("ledit_device.__main__.init_telemetry", return_value=mock_telemetry):
        from ledit_device.__main__ import main

        with pytest.raises(RuntimeError, match="boom"):
            main()
        mock_telemetry.shutdown.assert_called_once()
