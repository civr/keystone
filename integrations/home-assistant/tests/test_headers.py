import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "custom_components" / "keystone_auth"))

from headers import trusted, user_from  # noqa: E402


def test_trusted_ipv4_and_ipv6():
    proxies = ["172.18.0.0/16", "::1/128"]
    assert trusted("172.18.0.5", proxies)
    assert trusted("::1", proxies)
    assert trusted("::ffff:172.18.0.5", proxies)
    assert not trusted("192.168.1.50", proxies)
    assert not trusted("not-an-ip", proxies)
    assert not trusted("172.18.0.5", [])


def test_user_from():
    assert user_from({"X-Keystone-User": " Thijs "}) == "thijs"
    assert user_from({"x-keystone-user": "t.willems@home.lan"}) == "t.willems@home.lan"
    assert user_from({}) is None
    assert user_from({"X-Keystone-User": ""}) is None
    assert user_from({"X-Keystone-User": "a b"}) is None
    assert user_from({"X-Keystone-User": "x" * 65}) is None
    assert user_from({"X-Other": "thijs"}, header="X-Other") == "thijs"
