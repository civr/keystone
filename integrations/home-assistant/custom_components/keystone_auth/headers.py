"""Pure helpers for keystone_auth (no Home Assistant imports, so they can be tested alone)."""

from __future__ import annotations

import ipaddress
import re
from collections.abc import Iterable, Mapping

_USERNAME = re.compile(r"^[a-z0-9._@-]{1,64}$")


def trusted(remote: str, proxies: Iterable[str]) -> bool:
    """True when remote (the peer address HA sees) lies in one of the proxy networks."""
    try:
        ip = ipaddress.ip_address(remote)
    except ValueError:
        return False
    if isinstance(ip, ipaddress.IPv6Address) and ip.ipv4_mapped:
        ip = ip.ipv4_mapped
    for net in proxies:
        try:
            if ip in ipaddress.ip_network(net, strict=False):
                return True
        except ValueError:
            continue
    return False


def user_from(headers: Mapping[str, str], header: str = "X-Keystone-User") -> str | None:
    """The username keystone-gate sent, lowercased, or None when absent or malformed."""
    value = None
    for key, val in headers.items():
        if key.lower() == header.lower():
            value = val
            break
    if value is None:
        return None
    value = value.strip().lower()
    return value if _USERNAME.match(value) else None
