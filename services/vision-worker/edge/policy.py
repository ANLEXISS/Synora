"""Explicit policy for the exceptional central re-tracking fallback."""

from __future__ import annotations


FALLBACK_STATUSES = frozenset({"unavailable", "invalid", "contradictory"})


def central_retrack_allowed(edge_tracking_status: str, enabled: bool = False) -> bool:
    """Central pixel re-tracking is disabled unless explicitly enabled."""
    return bool(enabled) and str(edge_tracking_status) in FALLBACK_STATUSES
