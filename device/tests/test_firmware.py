import hashlib
import json
import os
from unittest import mock

def _manifest(version="1.2.0", sha="abc", size=4, action="upgrade"):
    return {"action": action, "version": version, "sha256": sha, "size": size, "url": "http://example/api/device/firmware/1.2.0/artifact"}

def test_checksum_mismatch_aborts(tmp_path, monkeypatch):
    from ledit_device import firmware
    monkeypatch.setenv("LEDIT_STAGING_DIR", str(tmp_path))
    data = b"hello"
    sha_wrong = "0"*64
    manifest = _manifest(sha=sha_wrong, size=len(data))
    def fake_urlopen(req, timeout=10):
        url = req.full_url if hasattr(req, 'full_url') else req
        class R:
            def __enter__(self): return self
            def __exit__(self,*a): return False
            def read(self, n=-1):
                if "firmware?" in url:
                    return json.dumps(manifest).encode()
                # artifact
                if not hasattr(self, "_done"):
                    self._done = True
                    return data
                return b""
            def __iter__(self): return iter([data])
        # need to support read with size for firmware loop
        class R2:
            def __init__(self): self._pos=0
            def __enter__(self): return self
            def __exit__(self,*a): return False
            def read(self, n=8192):
                if "firmware?" in url:
                    # manifest read without n
                    return json.dumps(manifest).encode() if self._pos==0 else b""
                # artifact streaming
                if self._pos >= len(data):
                    return b""
                chunk = data[self._pos:self._pos+n]
                self._pos+=len(chunk)
                return chunk
        # distinguish: manifest url contains firmware?version
        if "firmware?" in str(url):
            class MR:
                def __enter__(self): return self
                def __exit__(self,*a): return False
                def read(self): return json.dumps(manifest).encode()
            return MR()
        else:
            return R2()
    monkeypatch.setattr("ledit_device.firmware.urllib.request.urlopen", fake_urlopen)
    monkeypatch.setattr("ledit_device.firmware._report", lambda *a,**kw: None)
    firmware.check_and_update("http://example", "tok", "1.0.0")
    # no staged file
    assert not os.path.exists(os.path.join(str(tmp_path), "firmware-1.2.0.bin"))
    assert not os.path.exists(os.path.join(str(tmp_path), "activate"))

def test_truncated_download_aborts(tmp_path, monkeypatch):
    from ledit_device import firmware
    monkeypatch.setenv("LEDIT_STAGING_DIR", str(tmp_path))
    data = b"hi"
    sha = hashlib.sha256(data).hexdigest()
    manifest = _manifest(sha=sha, size=100)  # expects 100
    def fake_urlopen(req, timeout=10):
        url = req.full_url if hasattr(req, 'full_url') else str(req)
        if "firmware?" in str(url):
            class MR:
                def __enter__(self): return self
                def __exit__(self,*a): return False
                def read(self): return json.dumps(manifest).encode()
            return MR()
        else:
            class R:
                def __enter__(self): return self
                def __exit__(self,*a): return False
                _done=False
                def read(self, n=8192):
                    if self._done: return b""
                    self._done=True
                    return data
            return R()
    monkeypatch.setattr("ledit_device.firmware.urllib.request.urlopen", fake_urlopen)
    monkeypatch.setattr("ledit_device.firmware._report", lambda *a,**kw: None)
    firmware.check_and_update("http://example", "tok", "1.0.0")
    assert not os.path.exists(os.path.join(str(tmp_path), "activate"))

def test_manifest_unreachable_non_fatal(tmp_path, monkeypatch):
    from ledit_device import firmware
    monkeypatch.setenv("LEDIT_STAGING_DIR", str(tmp_path))
    def boom(*a,**kw): raise ConnectionError("down")
    monkeypatch.setattr("ledit_device.firmware.urllib.request.urlopen", boom)
    # should not raise
    firmware.check_and_update("http://example", "tok", "1.0.0")
    assert not os.path.exists(os.path.join(str(tmp_path), "activate"))

def test_malformed_manifest_non_fatal(tmp_path, monkeypatch):
    from ledit_device import firmware
    monkeypatch.setenv("LEDIT_STAGING_DIR", str(tmp_path))
    class MR:
        def __enter__(self): return self
        def __exit__(self,*a): return False
        def read(self): return b"not json {"
    monkeypatch.setattr("ledit_device.firmware.urllib.request.urlopen", lambda *a,**kw: MR())
    firmware.check_and_update("http://example", "tok", "1.0.0")

def test_successful_verify_stages_and_reports(tmp_path, monkeypatch):
    from ledit_device import firmware
    monkeypatch.setenv("LEDIT_STAGING_DIR", str(tmp_path))
    data = b"hello world"
    sha = hashlib.sha256(data).hexdigest()
    manifest = _manifest(sha=sha, size=len(data))
    reported = {}
    def fake_report(srv, tok, ver, status):
        reported["status"]=status
        reported["version"]=ver
    monkeypatch.setattr("ledit_device.firmware._report", fake_report)
    def fake_urlopen(req, timeout=10):
        url = req.full_url if hasattr(req, 'full_url') else str(req)
        if "firmware?" in str(url):
            class MR:
                def __enter__(self): return self
                def __exit__(self,*a): return False
                def read(self): return json.dumps(manifest).encode()
            return MR()
        else:
            class R:
                def __enter__(self): return self
                def __exit__(self,*a): return False
                _done=False
                def read(self, n=8192):
                    if self._done: return b""
                    self._done=True
                    return data
            return R()
    monkeypatch.setattr("ledit_device.firmware.urllib.request.urlopen", fake_urlopen)
    firmware.check_and_update("http://example", "tok", "1.0.0")
    assert os.path.exists(os.path.join(str(tmp_path), "firmware-1.2.0.bin"))
    assert open(os.path.join(str(tmp_path), "activate")).read().strip() == "1.2.0"
    assert reported["status"] == "success"

def test_action_none_does_nothing(tmp_path, monkeypatch):
    from ledit_device import firmware
    monkeypatch.setenv("LEDIT_STAGING_DIR", str(tmp_path))
    manifest = {"action": "none"}
    class MR:
        def __enter__(self): return self
        def __exit__(self,*a): return False
        def read(self): return json.dumps(manifest).encode()
    monkeypatch.setattr("ledit_device.firmware.urllib.request.urlopen", lambda *a,**kw: MR())
    firmware.check_and_update("http://example", "tok", "1.0.0")
    assert not os.path.exists(os.path.join(str(tmp_path), "activate"))
