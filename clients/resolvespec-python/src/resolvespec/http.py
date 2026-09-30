"""Shared HTTP helpers for the REST clients."""
from __future__ import annotations

from typing import Any, Dict, Mapping, Optional
from urllib.parse import quote


class ResolveSpecError(Exception):
    """Raised on a non-2xx response or an unsuccessful API result."""

    def __init__(
        self,
        message: str,
        status_code: Optional[int] = None,
        code: Optional[str] = None,
        details: Any = None,
        detail: Optional[str] = None,
    ):
        super().__init__(message)
        self.message = message
        self.status_code = status_code
        self.code = code
        self.details = details
        self.detail = detail  # server-side reason (funcspec / restheadspec errors)


def merge_headers(*sources: Mapping[str, str]) -> Dict[str, str]:
    """Merge HTTP headers case-insensitively; the last source wins and keeps its spelling."""
    result: Dict[str, str] = {}
    for source in sources:
        for name, value in source.items():
            for existing in [k for k in result if k.lower() == name.lower()]:
                del result[existing]
            result[name] = value
    return result


def client_headers(token: Optional[str], headers: Optional[Mapping[str, str]]) -> Dict[str, str]:
    """Content-Type < custom headers < bearer token."""
    return merge_headers(
        {"Content-Type": "application/json"},
        headers or {},
        {"Authorization": f"Bearer {token}"} if token else {},
    )


def build_url(base_url: str, schema: str, entity: str, id: Optional[Any] = None) -> str:
    url = f"{base_url.rstrip('/')}/{quote(schema, safe='')}/{quote(entity, safe='')}"
    if id is not None and id != "":
        url += f"/{quote(str(id), safe='')}"
    return url


def drop_none(d: Mapping[str, Any]) -> Dict[str, Any]:
    return {k: v for k, v in d.items() if v is not None}


def parse_json(response: Any) -> Any:
    try:
        return response.json()
    except ValueError:
        return None


def error_from(response: Any, data: Any) -> ResolveSpecError:
    err = data.get("error") if isinstance(data, dict) else None
    err = err if isinstance(err, dict) else {}
    text = (response.text or "").strip() if data is None else ""
    fallback = text[:200] or f"{response.reason_phrase} ({response.status_code})"
    return ResolveSpecError(
        err.get("message") or fallback,
        status_code=response.status_code,
        code=err.get("code"),
        details=err.get("details"),
        detail=err.get("detail"),
    )
