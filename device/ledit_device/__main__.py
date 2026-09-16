"""Command-line entry point.

Run as ``python -m ledit_device`` or via the ``ledit-device`` console script.
"""

import websocket  # websocket-client

from .client import Client, build_ws_url
from .config import log, server_url, token
from .display import make_display
from .telemetry import init_telemetry


def main():
    telemetry = init_telemetry()
    buttons = None
    client = None
    adv = None
    try:
        from .config import token_optional

        token_value = token_optional()
        if not token_value:
            # Discovery + provisioning mode
            try:
                from .discovery import fingerprint, new_nonce, start_advertising, provision
                fp = fingerprint()
                nonce = new_nonce()
                adv = start_advertising(fp, nonce)
                tok = provision(server_url(), fp, nonce, interval=2.0, timeout=300)
                if tok:
                    token_value = tok
                else:
                    # fallback to original token() which will exit with message
                    token_value = token()
            except SystemExit:
                raise
            except Exception:
                token_value = token()
        url = build_ws_url(server_url(), token_value)

        display = make_display()
        client = Client(display)
        log("info", "connecting to %s (matrix %dx%d)" % (url, display.width, display.height))

        ws = websocket.WebSocketApp(
            url,
            on_message=client.on_message,
            on_error=client.on_error,
            on_close=client.on_close,
            on_open=client.on_open,
            on_reconnect=client.on_reconnect,
        )
        try:  # pragma: no cover - hardware wiring, tested via mock
            from .buttons import ButtonHandler  # pragma: no cover

            def _sender(msg):  # pragma: no cover
                try:  # pragma: no cover
                    ws.send(msg)  # pragma: no cover
                except Exception:  # pragma: no cover
                    pass  # pragma: no cover

            buttons = ButtonHandler(sender=_sender)  # pragma: no cover
            buttons.start()  # pragma: no cover
        except Exception:  # pragma: no cover
            pass  # pragma: no cover
        # run_forever with reconnect=True keeps the device online across drops.
        ws.run_forever(ping_interval=30, ping_timeout=10, reconnect=5)
    finally:
        if adv is not None:
            try:
                from .discovery import stop_advertising
                stop_advertising(adv)
            except Exception:
                pass
        if client is not None:  # pragma: no cover
            try:  # pragma: no cover
                client.close()  # pragma: no cover
            except Exception:  # pragma: no cover
                pass  # pragma: no cover
        if buttons is not None:  # pragma: no cover
            try:  # pragma: no cover
                buttons.close()  # pragma: no cover
            except Exception:  # pragma: no cover
                pass  # pragma: no cover
        telemetry.shutdown()


if __name__ == "__main__":
    main()
