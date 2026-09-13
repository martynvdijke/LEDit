"""Display surfaces: a hardware RGB matrix or a file-based preview."""

import os
import sys
import threading

from .config import env_int, log

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:  # pragma: no cover
    sys.stderr.write("Pillow is required: pip install pillow\n")
    raise


def _truetype_font(size):
    candidates = [
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    ]
    for path in candidates:
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    return ImageFont.load_default()


class Display:
    """Abstraction over the output surface (hardware matrix or file preview)."""

    @property
    def width(self):  # pragma: no cover
        raise NotImplementedError  # pragma: no cover

    @property
    def height(self):  # pragma: no cover
        raise NotImplementedError  # pragma: no cover

    def show(self, image):  # pragma: no cover
        raise NotImplementedError  # pragma: no cover

    def set_brightness(self, level):  # pragma: no cover
        """Apply a live brightness hint (0-100). No-op on file/preview surfaces.

        Overridden by :class:`MatrixDisplay`; out-of-range values are ignored by
        the hardware implementation.
        """
        return


class MatrixDisplay(Display):  # pragma: no cover - hardware
    def __init__(self):  # pragma: no cover
        from rgbmatrix import RGBMatrix, RGBMatrixOptions  # deferred import  # pragma: no cover

        options = RGBMatrixOptions()  # pragma: no cover
        options.rows = env_int("LEDIT_ROWS", 64)  # pragma: no cover
        options.cols = env_int("LEDIT_COLS", 64)  # pragma: no cover
        options.chain_length = env_int("LEDIT_CHAIN", 1)  # pragma: no cover
        options.parallel = env_int("LEDIT_PARALLEL", 1)  # pragma: no cover
        options.hardware_mapping = os.getenv("LEDIT_HARDWARE_MAPPING", "regular")  # pragma: no cover
        options.brightness = env_int("LEDIT_BRIGHTNESS", 80)  # pragma: no cover
        options.gpio_slowdown = env_int("LEDIT_GPIO_SLOWDOWN", 1)  # pragma: no cover
        options.pwm_bits = 11  # pragma: no cover
        self.matrix = RGBMatrix(options=options)  # pragma: no cover
        self._brightness_lock = threading.Lock()  # pragma: no cover

    @property
    def width(self):  # pragma: no cover
        return self.matrix.width  # pragma: no cover

    @property
    def height(self):  # pragma: no cover
        return self.matrix.height  # pragma: no cover

    def show(self, image):  # pragma: no cover
        canvas = self.matrix.CreateFrameCanvas()  # pragma: no cover
        canvas.SetImage(image)  # pragma: no cover
        self.matrix.SwapOnVSync(canvas)  # pragma: no cover

    def set_brightness(self, level):
        """Update the hardware PWM brightness live, without recreating matrix.

        Ignores non-integer and out-of-range values; a no-op when the driver
        does not expose a writable ``brightness`` attribute.
        """
        if isinstance(level, bool) or not isinstance(level, int):
            return
        if level < 0 or level > 100:
            return
        lock = getattr(self, "_brightness_lock", None)
        if lock is None:
            return
        with lock:
            matrix = getattr(self, "matrix", None)
            if matrix is None or not hasattr(matrix, "brightness"):
                return
            try:
                matrix.brightness = level
            except Exception:  # noqa: BLE001 - driver may reject; best effort
                pass


class FileDisplay(Display):
    """Writes each frame as a PNG file; for testing without hardware."""

    def __init__(self, outdir, width, height):
        os.makedirs(outdir, exist_ok=True)
        self.outdir = outdir
        self._width = width
        self._height = height
        self.counter = 0

    @property
    def width(self):
        return self._width

    @property
    def height(self):
        return self._height

    def show(self, image):
        self.counter += 1
        path = os.path.join(self.outdir, "frame_%06d.png" % self.counter)
        image.save(path)
        log("info", "saved frame %s" % path)


def make_display():
    preview = os.getenv("LEDIT_PREVIEW_DIR")
    if preview:
        return FileDisplay(preview, env_int("LEDIT_COLS", 64), env_int("LEDIT_ROWS", 64))
    try:
        return MatrixDisplay()
    except ImportError:
        log("warning", "rpi-rgb-led-matrix not installed; falling back to preview mode")
        return FileDisplay("/tmp/ledit_frames", env_int("LEDIT_COLS", 64), env_int("LEDIT_ROWS", 64))
