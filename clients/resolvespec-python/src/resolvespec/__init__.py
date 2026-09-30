"""ResolveSpec Python client: REST (ResolveSpec), HeaderSpec and WebSocketSpec."""
from typing import Mapping, Optional

from .headerspec import (
    AsyncHeaderSpecClient,
    HeaderSpecClient,
    build_headers,
    decode_header_value,
    encode_header_value,
)
from .funcspec import AsyncFuncSpecClient, FuncSpecClient
from .http import ResolveSpecError, merge_headers
from .resolvespec import AsyncResolveSpecClient, ResolveSpecClient
from .types import *  # noqa: F401,F403
from .websocket import Subscription, WebSocketClient


def _cache_key(base_url: str, token: Optional[str], headers: Optional[Mapping[str, str]]):
    return (
        base_url,
        token,
        tuple(sorted((k.lower(), v) for k, v in (headers or {}).items())),
    )


_resolvespec: dict = {}
_headerspec: dict = {}


def get_resolvespec_client(base_url: str, token: Optional[str] = None, headers: Optional[Mapping[str, str]] = None) -> ResolveSpecClient:
    """Cached sync client, keyed by base_url + token + headers (case-insensitive names)."""
    key = _cache_key(base_url, token, headers)
    if key not in _resolvespec:
        _resolvespec[key] = ResolveSpecClient(base_url, token, headers)
    return _resolvespec[key]


def get_headerspec_client(base_url: str, token: Optional[str] = None, headers: Optional[Mapping[str, str]] = None) -> HeaderSpecClient:
    """Cached sync client, keyed by base_url + token + headers (case-insensitive names)."""
    key = _cache_key(base_url, token, headers)
    if key not in _headerspec:
        _headerspec[key] = HeaderSpecClient(base_url, token, headers)
    return _headerspec[key]
