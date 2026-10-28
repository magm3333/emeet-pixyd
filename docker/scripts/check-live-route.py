#!/usr/bin/env python3
"""Smoke-test live tracking suspend/restore through the local HTTP API."""

from __future__ import annotations

import argparse
import json
import time
import urllib.error
import urllib.request


def request(base_url: str, path: str, method: str = "GET") -> tuple[int, str]:
    req = urllib.request.Request(base_url.rstrip("/") + path, method=method)
    with urllib.request.urlopen(req, timeout=10) as response:
        return response.status, response.read().decode(errors="replace")


def camera_state(base_url: str) -> str:
    _, body = request(base_url, "/api/status")
    return str(json.loads(body)["camera"])


def wait_for_state(base_url: str, expected: str) -> None:
    deadline = time.monotonic() + 8
    last = "unknown"
    while time.monotonic() < deadline:
        last = camera_state(base_url)
        if last == expected:
            return
        time.sleep(0.2)
    raise RuntimeError(f"camera mode stayed {last!r}; expected {expected!r}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://127.0.0.1:18090")
    args = parser.parse_args()
    base = args.url.rstrip("/")

    original = camera_state(base)
    if original not in {"tracking", "idle", "privacy"}:
        raise RuntimeError(f"unexpected starting camera mode {original!r}")

    recording = False
    try:
        if original != "tracking":
            request(base, "/api/track", "POST")
            wait_for_state(base, "tracking")

        status, start_patch = request(base, "/api/route/record/start", "POST")
        if status != 200 or 'id="route-record-stop-btn"' not in start_patch:
            raise RuntimeError("record start did not replace the UI control with Stop")
        recording = True
        wait_for_state(base, "idle")

        status, discard_patch = request(base, "/api/route/record/discard", "POST")
        recording = False
        if status != 200 or 'id="route-record-start-btn"' not in discard_patch:
            raise RuntimeError("discard did not restore the UI start control")
        wait_for_state(base, "tracking")
        print("OK: live camera switched Track -> Idle during recording and restored Track on discard")
    finally:
        if recording:
            try:
                request(base, "/api/route/record/discard", "POST")
            except (OSError, urllib.error.URLError):
                pass
        restore_path = {
            "tracking": "/api/track",
            "idle": "/api/idle",
            "privacy": "/api/privacy",
        }[original]
        if camera_state(base) != original:
            request(base, restore_path, "POST")
            wait_for_state(base, original)
        print(f"Camera mode restored to its original value: {original}")


if __name__ == "__main__":
    main()
