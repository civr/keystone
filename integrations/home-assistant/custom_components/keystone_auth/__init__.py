"""Keystone sign-in for Home Assistant.

keystone-gate (behind your reverse proxy) adds X-Keystone-User to every
request of a signed-in Keystone user. This component adds a login provider
that, when the login page is opened through a trusted proxy with that header,
signs in the Home Assistant user with the same username at once. Without the
header (or from an untrusted address) it steps aside and the normal login
form is shown.

configuration.yaml:

    keystone_auth:
      trusted_proxies:
        - 172.18.0.0/16      # the reverse proxy as Home Assistant sees it
      header: X-Keystone-User  # optional
"""

from __future__ import annotations

import logging
from collections import OrderedDict
from typing import Any

import voluptuous as vol

from homeassistant.auth.models import Credentials, UserMeta
from homeassistant.auth.providers import AUTH_PROVIDERS, AuthProvider, LoginFlow
from homeassistant.core import HomeAssistant
from homeassistant.data_entry_flow import FlowResult
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.http import current_request

from .headers import trusted, user_from

_LOGGER = logging.getLogger(__name__)

DOMAIN = "keystone_auth"
PROVIDER_TYPE = "keystone"

CONFIG_SCHEMA = vol.Schema(
    {
        DOMAIN: vol.Schema(
            {
                vol.Required("trusted_proxies"): vol.All(cv.ensure_list, [cv.string]),
                vol.Optional("header", default="X-Keystone-User"): cv.string,
            }
        )
    },
    extra=vol.ALLOW_EXTRA,
)


async def async_setup(hass: HomeAssistant, config: dict[str, Any]) -> bool:
    conf = config[DOMAIN]
    provider = KeystoneAuthProvider(
        hass,
        hass.auth._store,  # noqa: SLF001 - no public API to add a provider at runtime
        {"type": PROVIDER_TYPE, "id": None, "name": "Keystone"},
    )
    provider.trusted_proxies = conf["trusted_proxies"]
    provider.header = conf["header"]
    # First in the list: the login page starts with this provider.
    providers = OrderedDict()
    providers[(provider.type, provider.id)] = provider
    providers.update(hass.auth._providers)  # noqa: SLF001
    hass.auth._providers = providers  # noqa: SLF001
    _LOGGER.info("Keystone sign-in active for proxies %s", conf["trusted_proxies"])
    return True


class KeystoneAuthProvider(AuthProvider):
    """Signs in the HA user named in keystone-gate's header."""

    DEFAULT_TITLE = "Keystone"
    trusted_proxies: list[str] = []
    header = "X-Keystone-User"

    @property
    def type(self) -> str:
        return PROVIDER_TYPE

    @property
    def support_mfa(self) -> bool:
        return False  # Keystone already required a passkey

    async def async_login_flow(self, context: dict[str, Any] | None) -> LoginFlow:
        request = current_request.get()
        username = None
        if request is not None and request.transport is not None:
            peer = request.transport.get_extra_info("peername")
            if peer and trusted(peer[0], self.trusted_proxies):
                username = user_from(request.headers, self.header)
        if username and await self._ha_user(username) is None:
            username = None
        # Without a Keystone visit the flow stops; the login page then offers
        # the other providers (the normal form). Handing over another
        # provider's flow is not possible: Home Assistant would finish it
        # with this provider, skipping that provider's MFA.
        return KeystoneLoginFlow(self, username)

    async def async_get_or_create_credentials(self, flow_result: dict[str, str]) -> Credentials:
        username = flow_result["username"]
        for cred in await self.async_credentials():
            if cred.data.get("username") == username:
                return cred
        user = await self._ha_user(username)
        if user is None:
            raise ValueError(f"no Home Assistant user named {username}")
        cred = self.async_create_credentials({"username": username})
        await self.store.async_link_user(user, cred)
        return cred

    async def _ha_user(self, username: str):
        """The active HA user whose Home Assistant login name is username.

        Only the login name counts: it is unique, display names are not.
        """
        for user in await self.store.async_get_users():
            if user.system_generated or not user.is_active:
                continue
            for cred in user.credentials:
                if cred.auth_provider_type == "homeassistant" and cred.data.get("username", "").lower() == username:
                    return user
        return None

    async def async_user_meta_for_credentials(self, credentials: Credentials) -> UserMeta:
        return UserMeta(name=credentials.data["username"], is_active=True)


class KeystoneLoginFlow(LoginFlow):
    """Finishes at once for the Keystone user."""

    def __init__(self, provider: KeystoneAuthProvider, username: str | None) -> None:
        super().__init__(provider)
        self._username = username

    async def async_step_init(self, user_input: dict[str, str] | None = None) -> FlowResult:
        if self._username:
            return await self.async_finish({"username": self._username})
        return self.async_abort(reason="not_keystone")


AUTH_PROVIDERS.setdefault(PROVIDER_TYPE, KeystoneAuthProvider)
