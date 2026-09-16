from ledit_device import config

def test_update_interval_default(monkeypatch):
    monkeypatch.delenv("LEDIT_UPDATE_INTERVAL", raising=False)
    assert config.update_interval() == 3600

def test_update_interval_env(monkeypatch):
    monkeypatch.setenv("LEDIT_UPDATE_INTERVAL", "0")
    assert config.update_interval() == 0
    monkeypatch.setenv("LEDIT_UPDATE_INTERVAL", "123")
    assert config.update_interval() == 123

def test_update_channel_default(monkeypatch):
    monkeypatch.delenv("LEDIT_UPDATE_CHANNEL", raising=False)
    assert config.update_channel() == ""

def test_update_channel_env(monkeypatch):
    monkeypatch.setenv("LEDIT_UPDATE_CHANNEL", "beta")
    assert config.update_channel() == "beta"

def test_save_and_load_token(tmp_path, monkeypatch):
    monkeypatch.setenv("LEDIT_CONFIG_DIR", str(tmp_path))
    monkeypatch.delenv("LEDIT_TOKEN", raising=False)
    config.save_token("my-token-123")
    assert config.token_optional() == "my-token-123"
    assert config.token() == "my-token-123"
