import os
from unittest import mock

from PIL import Image

from ledit_device.display import FileDisplay, _truetype_font


def test_file_display_writes_png_and_increments(tmp_path):
    fd = FileDisplay(str(tmp_path), 64, 64)
    img = Image.new("RGB", (64, 64), "red")
    fd.show(img)
    fd.show(img)
    assert fd.counter == 2
    files = sorted(f for f in os.listdir(tmp_path) if f.endswith(".png"))
    assert len(files) == 2
    assert files[0] == "frame_000001.png"
    assert files[1] == "frame_000002.png"
    # files exist and are valid PNGs
    for f in files:
        im = Image.open(os.path.join(tmp_path, f))
        assert im.size == (64, 64)


def test_file_display_respects_dims(tmp_path):
    fd = FileDisplay(str(tmp_path), 32, 48)
    assert fd.width == 32
    assert fd.height == 48
    img = Image.new("RGB", (32, 48), "blue")
    fd.show(img)
    files = os.listdir(tmp_path)
    assert len(files) == 1
    im = Image.open(os.path.join(tmp_path, files[0]))
    assert im.size == (32, 48)


def test_matrix_display_options_wired(monkeypatch):
    # Ensure rgbmatrix mock is set up
    import sys

    mock_rgbmatrix = mock.MagicMock()
    mock_options_cls = mock.MagicMock()
    mock_options_instance = mock_options_cls.return_value
    mock_rgbmatrix.RGBMatrixOptions = mock_options_cls
    mock_rgbmatrix.RGBMatrix.return_value = mock.MagicMock(width=32, height=16)
    monkeypatch.setitem(sys.modules, "rgbmatrix", mock_rgbmatrix)

    monkeypatch.setenv("LEDIT_ROWS", "16")
    monkeypatch.setenv("LEDIT_COLS", "32")
    monkeypatch.setenv("LEDIT_BRIGHTNESS", "60")

    from ledit_device.display import MatrixDisplay

    # Need to reload or ensure import uses mocked module; MatrixDisplay deferred import will use sys.modules
    md = MatrixDisplay()
    assert mock_options_instance.rows == 16
    assert mock_options_instance.cols == 32
    assert mock_options_instance.brightness == 60
    assert mock_options_instance.pwm_bits == 11


def test_truetype_font_fallback(monkeypatch):
    # Simulate no DejaVu fonts
    monkeypatch.setattr(os.path, "exists", lambda p: False)
    font = _truetype_font(12)
    # Should return default font without raising
    assert font is not None


def test_truetype_font_with_existing_file(monkeypatch, tmp_path):
    # Create a fake ttf - but ImageFont.truetype will fail if not valid ttf
    # so test the path where file exists but we mock truetype
    fake_path = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"
    monkeypatch.setattr(os.path, "exists", lambda p: p == fake_path)
    with mock.patch("PIL.ImageFont.truetype") as mock_tt:
        mock_tt.return_value = mock.MagicMock()
        font = _truetype_font(12)
        mock_tt.assert_called_once_with(fake_path, 12)
        assert font == mock_tt.return_value


def test_make_display_preview(monkeypatch, tmp_path):
    monkeypatch.setenv("LEDIT_PREVIEW_DIR", str(tmp_path))
    monkeypatch.setenv("LEDIT_COLS", "32")
    monkeypatch.setenv("LEDIT_ROWS", "16")
    from ledit_device.display import make_display, FileDisplay

    disp = make_display()
    assert isinstance(disp, FileDisplay)
    assert disp.width == 32
    assert disp.height == 16


def test_make_display_fallback(monkeypatch):
    monkeypatch.delenv("LEDIT_PREVIEW_DIR", raising=False)
    # Force MatrixDisplay to raise ImportError
    with mock.patch("ledit_device.display.MatrixDisplay", side_effect=ImportError("no hw")):
        from ledit_device.display import make_display, FileDisplay

        disp = make_display()
        assert isinstance(disp, FileDisplay)


def _matrix_display(monkeypatch):
    import sys

    mock_rgbmatrix = mock.MagicMock()
    mock_options_cls = mock.MagicMock()
    mock_rgbmatrix.RGBMatrixOptions = mock_options_cls
    matrix = mock.MagicMock(width=32, height=16)
    matrix.brightness = 80
    mock_rgbmatrix.RGBMatrix.return_value = matrix
    monkeypatch.setitem(sys.modules, "rgbmatrix", mock_rgbmatrix)
    from ledit_device.display import MatrixDisplay

    md = MatrixDisplay()
    return md, matrix


def test_matrix_set_brightness_updates_without_recreating(monkeypatch):
    md, matrix = _matrix_display(monkeypatch)
    md.set_brightness(30)
    assert matrix.brightness == 30
    # Same underlying matrix object, never recreated.
    assert md.matrix is matrix
    md.set_brightness(70)
    assert matrix.brightness == 70
    assert md.matrix is matrix


def test_matrix_set_brightness_ignores_invalid(monkeypatch):
    md, matrix = _matrix_display(monkeypatch)
    md.set_brightness(30)
    for bad in (150, -10, 101, "40", 40.5, True, None):
        md.set_brightness(bad)
    assert matrix.brightness == 30


def test_matrix_set_brightness_noop_without_attribute(monkeypatch):
    md, matrix = _matrix_display(monkeypatch)
    # A driver matrix without a writable brightness attribute must not raise.
    md.matrix = mock.MagicMock(spec=[])
    md.set_brightness(50)


def test_file_display_set_brightness_is_noop(tmp_path):
    fd = FileDisplay(str(tmp_path), 8, 8)
    # Base no-op keeps FileDisplay usable when the client applies hints.
    fd.set_brightness(20)

