"""WebSocket client: pulls frames from the server and renders them."""

import base64
import io
import json
import threading
import time

from PIL import Image, ImageDraw

from . import config
from .config import log
from .telemetry import get_telemetry

# Pillow >=10 moved LANCZOS into Image.Resampling; keep compatibility with both.
try:
    RESAMPLE = Image.Resampling.LANCZOS
except AttributeError:  # pragma: no cover
    RESAMPLE = Image.LANCZOS

# Device protocol version requested on connect. The server falls back to v1 for
# a missing/unknown version, so this is safe against older servers.
PROTOCOL_VERSION = 2

# Client spectrum tap: number of FFT bins sent (server accepts 16-32) and the
# target send rate.
SPECTRUM_BINS = 16
SPECTRUM_HZ = 20.0

# Frame `source` values that mean the audio visualizer is on screen. The
# protocol names the source "audio:visualizer"; the server's built-in visualizer
# currently renders under its display name "Audio Visualizer", so accept both.
VISUALIZER_SOURCES = ("audio:visualizer", "Audio Visualizer")


def build_ws_url(server, token):
    """Build the device WebSocket URL, opting into protocol v2."""
    return "%s/ws/device/%s?protocol=%d" % (
        server.rstrip("/"),
        token,
        PROTOCOL_VERSION,
    )


def _capture_spectrum_bins():
    """Best-effort microphone FFT bins (0-255) for the visualizer tap.

    Returns ``None`` when audio capture or numpy is unavailable so the caller
    simply does not stream. No third-party audio library is required; if
    ``sounddevice`` is installed it is used opportunistically.
    """
    try:
        import numpy as np
        import sounddevice as sd
    except Exception:  # pragma: no cover - exercised when libs missing
        return None
    try:
        rate = 16000
        block = 512
        samples = sd.rec(block, samplerate=rate, channels=1, dtype="float32")
        sd.wait()
        mono = samples[:, 0]
        windowed = mono * np.hanning(len(mono))
        spectrum = np.abs(np.fft.rfft(windowed))
        # Aggregate the linear spectrum into log-spaced bands, normalise to 0-255.
        n = len(spectrum)
        bins = []
        for i in range(SPECTRUM_BINS):
            lo = int((n ** (i / SPECTRUM_BINS)))
            hi = int((n ** ((i + 1) / SPECTRUM_BINS)))
            if hi <= lo:
                hi = lo + 1
            band = float(np.max(spectrum[lo:hi])) if hi <= n else 0.0
            bins.append(band)
        peak = max(bins) or 1.0
        return [int(min(255, max(0, round(b / peak * 255)))) for b in bins]
    except Exception:  # pragma: no cover - mic/permission failures
        return None


def _hello_payload():
    try:
        from . import __version__
    except Exception:
        __version__ = "0.0.0"
    return {"type": "hello", "version": __version__, "capabilities": ["ota"]}


class Client:
    def __init__(self, display, spectrum_capture=None, spectrum_interval=None):
        self.display = display
        telemetry = get_telemetry()
        self._tracer = telemetry.get_tracer()
        meter = telemetry.get_meter()
        self._frames_rendered = meter.create_counter(
            "device.frames_rendered_total",
            unit="{frame}",
            description="Total number of frames rendered to the display",
        )
        self._connection_errors = meter.create_counter(
            "device.connection_errors_total",
            unit="{error}",
            description="Total number of WebSocket connection errors",
        )
        self._reconnects = meter.create_counter(
            "device.reconnects_total",
            unit="{reconnect}",
            description="Total number of WebSocket reconnects",
        )

        # Protocol v2 state. Defaults are v1: no welcome means no v2 features.
        self._protocol = 1
        self._capabilities = set()
        self._last_source = None
        self._ws = None

        # Audio spectrum tap (opt-in, off by default).
        self._spectrum_opt_in = config.spectrum_enabled()
        self._spectrum_capture = spectrum_capture or _capture_spectrum_bins
        self._spectrum_interval = (
            spectrum_interval if spectrum_interval is not None else 1.0 / SPECTRUM_HZ
        )
        self._spectrum_active = threading.Event()
        self._spectrum_stop = threading.Event()
        self._spectrum_thread = None
        self._spectrum_lock = threading.Lock()

        # OTA firmware polling (off render loop)
        self._ota_thread = None
        self._ota_stop = threading.Event()
        self._ota_lock = threading.Lock()

    # -- rendering -----------------------------------------------------------

    def render_image(self, b64):
        raw = base64.b64decode(b64)
        img = Image.open(io.BytesIO(raw)).convert("RGB")
        img = img.resize((self.display.width, self.display.height), RESAMPLE)
        self.display.show(img)
        self._frames_rendered.add(1, {"frame.type": "image"})

    def render_text(self, title, message):
        from .display import _truetype_font

        img = Image.new("RGB", (self.display.width, self.display.height), (0, 0, 0))
        draw = ImageDraw.Draw(img)
        size = max(8, min(self.display.width, self.display.height) // 8)
        font = _truetype_font(size)
        lines = []
        if title:
            lines.append(title)
        if message:
            lines.extend(message.splitlines() or [message])
        y = 2
        for line in lines:
            if y > self.display.height:
                break
            draw.text((2, y), line, fill=(255, 255, 255), font=font)
            y += size + 2
        self.display.show(img)
        self._frames_rendered.add(1, {"frame.type": "text"})

    # -- protocol v2 negotiation --------------------------------------------

    def _handle_welcome(self, data):
        """Enable v2 features only when the server advertises protocol >= 2."""
        caps = data.get("capabilities")
        proto = data.get("protocol", 0)
        if not isinstance(proto, int) or proto < 2:
            return
        if not isinstance(caps, list):
            return
        self._protocol = 2
        self._capabilities = {c for c in caps if isinstance(c, str)}
        log("info", "server protocol v2: capabilities=%s" % sorted(self._capabilities))
        self._maybe_start_ota()

    def _maybe_start_ota(self):
        # OTA gated on server capability and interval > 0
        if "ota" not in self._capabilities and "firmware" not in self._capabilities:
            return
        interval = config.update_interval()
        if interval == 0:
            return
        self._ensure_ota_thread()

    def _ensure_ota_thread(self):
        with self._ota_lock:
            if self._ota_thread is not None and self._ota_thread.is_alive():
                return
            self._ota_stop.clear()
            self._ota_thread = threading.Thread(target=self._ota_loop, name="ledit-ota", daemon=True)
            self._ota_thread.start()

    def _ota_loop(self):
        # Import lazily to avoid cost when OTA disabled
        try:
            from . import firmware as _fw
            from . import __version__ as _ver
        except Exception:
            return
        # current version
        try:
            from . import __version__
            cur = __version__
        except Exception:
            cur = "0.0.0"
        while not self._ota_stop.is_set():
            try:
                tok = config.token_optional()
                if tok:
                    srv = config.server_url()
                    ch = config.update_channel() or None
                    _fw.check_and_update(srv, tok, cur, channel=ch)
            except Exception:
                log("warning", "ota poll failed")
            interval = config.update_interval()
            if interval == 0:
                return
            # sleep with stop check
            self._ota_stop.wait(timeout=interval)

    def on_open(self, ws):
        self._ws = ws
        try:
            ws.send(json.dumps(_hello_payload()))
        except Exception:
            pass

    def _track_source(self, data):
        src = data.get("source")
        if isinstance(src, str):
            self._last_source = src

    def _apply_brightness(self, data):
        """Apply a live brightness hint, ignoring absent/out-of-range/invalid."""
        if "brightness" not in self._capabilities:
            return
        level = data.get("brightness")
        if isinstance(level, bool) or not isinstance(level, int):
            return
        if level < 0 or level > 100:
            return
        try:
            self.display.set_brightness(level)
        except Exception:  # noqa: BLE001 - display must never take the feed down
            log("warning", "failed to apply brightness hint")

    # -- spectrum tap --------------------------------------------------------

    def _update_spectrum_gate(self):
        active = (
            self._spectrum_opt_in
            and self._protocol >= 2
            and "spectrum" in self._capabilities
            and self._last_source in VISUALIZER_SOURCES
        )
        if active:
            self._spectrum_active.set()
            self._ensure_spectrum_thread()
        else:
            self._spectrum_active.clear()

    def _ensure_spectrum_thread(self):
        with self._spectrum_lock:
            if self._spectrum_thread is not None and self._spectrum_thread.is_alive():
                return
            self._spectrum_stop.clear()
            self._spectrum_thread = threading.Thread(
                target=self._spectrum_loop, name="ledit-spectrum", daemon=True
            )
            self._spectrum_thread.start()

    def _spectrum_loop(self):
        while not self._spectrum_stop.is_set():
            if self._spectrum_active.is_set():
                bins = self._capture_bins()
                if bins:
                    self._send_spectrum(bins)
            time.sleep(self._spectrum_interval)

    def _capture_bins(self):
        if not self._spectrum_opt_in:
            return None
        try:
            return self._spectrum_capture()
        except Exception:  # noqa: BLE001 - capture failure must not crash
            return None

    def _send_spectrum(self, bins):
        ws = self._ws
        if ws is None:
            return
        try:
            ws.send(json.dumps({"type": "spectrum", "bins": list(bins)}))
        except Exception:  # noqa: BLE001 - disconnected socket is best-effort
            pass

    def close(self):
        """Stop the spectrum thread (called on shutdown)."""
        self._spectrum_active.clear()
        self._spectrum_stop.set()
        self._ota_stop.set()
        thread = self._spectrum_thread
        if thread is not None and thread.is_alive():
            thread.join(timeout=0.2)
        ota = self._ota_thread
        if ota is not None and ota.is_alive():
            ota.join(timeout=0.2)

    # -- websocket callbacks -------------------------------------------------

    def on_message(self, _ws, message):
        if _ws is not None:
            self._ws = _ws
        with self._tracer.start_as_current_span("device.message.received"):
            try:
                data = json.loads(message)
            except (ValueError, TypeError):
                log("warning", "received invalid JSON frame")
                return
            if isinstance(data, dict) and data.get("type") == "welcome":
                self._handle_welcome(data)
                return
            self._apply_brightness(data)
            self._track_source(data)
            self._update_spectrum_gate()
            if "image" in data:
                with self._tracer.start_as_current_span("device.render.image"):
                    self.render_image(data["image"])
            else:
                with self._tracer.start_as_current_span("device.render.text"):
                    self.render_text(data.get("title", ""), data.get("message", ""))

    def on_error(self, _ws, error):
        self._connection_errors.add(1)
        with self._tracer.start_as_current_span("device.connection.error") as span:
            span.set_attribute("error.type", str(error))
            span.record_exception(Exception(error))
            span.set_status("ERROR", str(error))
        log("error", str(error))

    def on_close(self, _ws, *_args):
        log("info", "connection closed")

    def on_reconnect(self):
        self._reconnects.add(1)
