import pytest

from ledit_device import config


def test_env_int_valid(monkeypatch):
    monkeypatch.setenv("LEDIT_TEST_NUM", "42")
    assert config.env_int("LEDIT_TEST_NUM", 0) == 42


def test_env_int_invalid_falls_back(monkeypatch):
    monkeypatch.setenv("LEDIT_TEST_NUM", "notanumber")
    assert config.env_int("LEDIT_TEST_NUM", 7) == 7


def test_env_int_missing_falls_back(monkeypatch):
    monkeypatch.delenv("LEDIT_TEST_NUM", raising=False)
    assert config.env_int("LEDIT_TEST_NUM", 9) == 9


def test_server_url_default(monkeypatch):
    monkeypatch.delenv("LEDIT_SERVER", raising=False)
    assert config.server_url() == "ws://localhost:8080"


def test_server_url_strips_trailing_slash(monkeypatch):
    monkeypatch.setenv("LEDIT_SERVER", "ws://ledit.local:8080/")
    assert config.server_url() == "ws://ledit.local:8080"


def test_server_url_strips_multiple_slashes(monkeypatch):
    monkeypatch.setenv("LEDIT_SERVER", "ws://ledit.local:8080///")
    assert config.server_url() == "ws://ledit.local:8080"


def test_token_required_exits_blank(monkeypatch):
    monkeypatch.delenv("LEDIT_TOKEN", raising=False)
    with pytest.raises(SystemExit) as exc:
        config.token()
    assert exc.value.code == 1


def test_token_required_exits_empty_string(monkeypatch):
    monkeypatch.setenv("LEDIT_TOKEN", "   ")
    with pytest.raises(SystemExit) as exc:
        config.token()
    assert exc.value.code == 1


def test_token_returns_stripped(monkeypatch):
    monkeypatch.setenv("LEDIT_TOKEN", "  abc123  ")
    assert config.token() == "abc123"
