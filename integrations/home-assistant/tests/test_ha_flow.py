"""Runs against a real Home Assistant core (pytest-homeassistant-custom-component).

uv run --python 3.13 --with pytest-homeassistant-custom-component python -m pytest tests/test_ha_flow.py
"""
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

pytest.importorskip("pytest_homeassistant_custom_component")
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from homeassistant.helpers.http import current_request  # noqa: E402
from homeassistant.setup import async_setup_component  # noqa: E402

from custom_components import keystone_auth  # noqa: E402


class FakeRequest(SimpleNamespace):
    pass


def request(peer, headers):
    return FakeRequest(transport=SimpleNamespace(get_extra_info=lambda k: (peer, 1234)), headers=headers)


@pytest.fixture
async def ha(hass):
    from homeassistant.auth.providers import homeassistant as hass_auth

    hass_provider = hass_auth.HassAuthProvider(hass, hass.auth._store, {"type": "homeassistant", "id": None, "name": None})
    hass.auth._providers[(hass_provider.type, hass_provider.id)] = hass_provider
    user = await hass.auth.async_create_user("Thijs")
    await hass_provider.async_initialize()
    hass_provider.data.add_auth("thijs", "pw")
    cred = await hass_provider.async_get_or_create_credentials({"username": "thijs"})
    await hass.auth.async_link_user(user, cred)
    assert await async_setup_component(hass, "keystone_auth", {"keystone_auth": {"trusted_proxies": ["172.18.0.0/16"]}})
    return hass, user


def provider(hass):
    return next(p for p in hass.auth.auth_providers if p.type == "keystone")


async def test_keystone_header_signs_in(ha):
    hass, user = ha
    p = provider(hass)
    tok = current_request.set(request("172.18.0.5", {"X-Keystone-User": "thijs"}))
    try:
        result = await hass.auth.login_flow.async_init((p.type, p.id))
    finally:
        current_request.reset(tok)
    assert result["type"] == "create_entry"
    found = await hass.auth.async_get_or_create_user(result["result"])
    assert found.id == user.id


async def test_no_header_or_untrusted_aborts(ha):
    hass, _ = ha
    p = provider(hass)
    for req in (request("172.18.0.5", {}), request("192.168.1.50", {"X-Keystone-User": "thijs"})):
        tok = current_request.set(req)
        try:
            result = await hass.auth.login_flow.async_init((p.type, p.id))
        finally:
            current_request.reset(tok)
        assert result["type"] == "abort"
