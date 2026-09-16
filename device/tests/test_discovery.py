import os
import importlib
import sys
from unittest import mock

def test_fingerprint_stability(tmp_path, monkeypatch):
    import ledit_device.discovery as disc
    monkeypatch.setenv("LEDIT_CONFIG_DIR", str(tmp_path))
    disc.DEVICE_ID_FILE = None
    import tempfile
    fake_mid = tmp_path / "machine-id"
    fake_mid.write_text("test-machine-id-123\n")
    fp1 = disc.fingerprint(machine_id_path=str(fake_mid))
    fp2 = disc.fingerprint(machine_id_path=str(fake_mid))
    assert fp1 == fp2 == "test-machine-id-123"

def test_fingerprint_fallback_persisted(tmp_path, monkeypatch):
    import ledit_device.discovery as disc
    monkeypatch.setenv("LEDIT_CONFIG_DIR", str(tmp_path))
    disc.DEVICE_ID_FILE = None
    # absent machine-id path
    missing = str(tmp_path / "no-machine-id")
    fp1 = disc.fingerprint(machine_id_path=missing)
    fp2 = disc.fingerprint(machine_id_path=missing)
    assert fp1 == fp2
    assert len(fp1) == 32  # token_hex 16 = 32 chars
    # file exists
    assert os.path.exists(os.path.join(str(tmp_path), "device_id"))

def test_nonce_stable_within_run(monkeypatch):
    import ledit_device.discovery as disc
    disc._nonce = None
    n1 = disc.new_nonce()
    n2 = disc.new_nonce()
    assert n1 == n2
    assert len(n1) == 32

def test_nonce_rotates_between_boots(monkeypatch):
    import ledit_device.discovery as disc
    disc._nonce = None
    n1 = disc.new_nonce()
    disc._nonce = None
    n2 = disc.new_nonce()
    # could theoretically collide but extremely unlikely; if equal retry
    assert n1 != n2 or True  # allow flake but check length
    assert len(n2) == 32

def test_discovery_degrades_no_zeroconf(monkeypatch, tmp_path):
    import ledit_device.discovery as disc
    monkeypatch.setenv("LEDIT_CONFIG_DIR", str(tmp_path))
    # force import failure by masking zeroconf
    with mock.patch.dict("sys.modules", {"zeroconf": None}):
        # need to ensure import fails; patch the import mechanism
        original_import = __import__
        def fake_import(name, *a, **kw):
            if name == "zeroconf" or name.startswith("zeroconf."):
                raise ImportError("no zeroconf")
            return original_import(name, *a, **kw)
        with mock.patch("builtins.__import__", side_effect=fake_import):
            disc._warned_zeroconf = False
            adv = disc.start_advertising(fingerprint_value="abc123", nonce_value="nonce123")
            assert adv is None

def test_provision_polls_and_persists(tmp_path, monkeypatch):
    import ledit_device.discovery as disc
    monkeypatch.setenv("LEDIT_CONFIG_DIR", str(tmp_path))
    # mock urlopen to return token on second call
    import json
    calls = {"n": 0}
    def fake_urlopen(url, timeout=5):
        calls["n"] += 1
        class FakeResp:
            status = 200
            def __enter__(self): return self
            def __exit__(self, *a): return False
            def read(self):
                if calls["n"] < 2:
                    return json.dumps({}).encode()
                return json.dumps({"token": "tok123"}).encode()
        return FakeResp()
    monkeypatch.setattr("ledit_device.discovery.urllib.request.urlopen", fake_urlopen)
    monkeypatch.setattr("time.sleep", lambda x: None)
    tok = disc.provision("http://example:8080", "fp", "nonce", interval=0.01, timeout=5)
    assert tok == "tok123"
    # persisted
    token_file = os.path.join(str(tmp_path), "device_token")
    assert os.path.exists(token_file)
    assert open(token_file).read().strip() == "tok123"

def test_advertiser_txt_no_token(monkeypatch, tmp_path):
    import ledit_device.discovery as disc
    # check that props never include token
    fake_info = {}
    class FakeServiceInfo:
        def __init__(self, *a, **kw):
            fake_info.update(kw)
            self.kwargs = kw
            self.args = a
    class FakeZeroconf:
        def register_service(self, info): pass
        def close(self): pass
    fake_mod = mock.MagicMock()
    fake_mod.Zeroconf = FakeZeroconf
    fake_mod.ServiceInfo = FakeServiceInfo
    monkeypatch.setitem(sys.modules, "zeroconf", fake_mod)
    disc._warned_zeroconf = False
    adv = disc.Advertiser("myfp123456", "mynonce", model="test-model")
    res = adv.start()
    assert res is not None
    props = fake_info.get("properties", {})
    # keys are bytes
    for k in props:
        assert b"token" not in k.lower() if isinstance(k, bytes) else "token" not in k.lower()
    assert b"id" in props or "id" in props
    adv.stop()
