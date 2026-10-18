#!/usr/bin/env python3
"""Sends one realistic error event straight to a running Errgotrack server,
bypassing sentry-sdk entirely - just stdlib urllib building the same
envelope format sentry-sdk would send (see internal/sentryprotocol).

Lets you see the issue detail page populated with every optional section
(tags, user, SDK, request, breadcrumbs, extra) without wiring up a real
Django/Flask app first. The request section deliberately includes an
Authorization header, a Cookie, and a password field, so you can confirm
on the issue page that none of those three ever render.

Usage:
    python3 examples/send_test_event.py <public_key> [project_id] [base_url]

Find <public_key> and [project_id] in Settings on the project's page, or:
    sqlite3 data/errgotrack.db "SELECT id, public_key FROM projects;"
"""
import json
import sys
import time
import urllib.request
import uuid


def build_event():
    now = time.time()
    return {
        "event_id": uuid.uuid4().hex,
        "timestamp": now,
        "level": "error",
        "platform": "python",
        "environment": "production",
        "release": "myapp@1.4.0",
        "transaction": "checkout_view",
        "exception": {
            "values": [{
                "type": "ValueError",
                "value": "invalid cart total: -12.50",
                "module": "app.checkout",
                "stacktrace": {
                    "frames": [
                        {
                            "filename": "app/checkout.py", "function": "validate_total",
                            "module": "app.checkout", "lineno": 42, "in_app": True,
                            "pre_context": ["def validate_total(cart):", "    total = cart.total()"],
                            "context_line": "    if total < 0:",
                            "post_context": ["        raise ValueError(f'invalid cart total: {total}')"],
                            "vars": {"total": "-12.50", "cart": {"id": 981, "items": 3}},
                        },
                        {
                            "filename": "app/checkout.py", "function": "checkout_view",
                            "module": "app.checkout", "lineno": 17, "in_app": True,
                        },
                    ]
                },
            }]
        },
        "tags": {"url": "/checkout/", "browser.name": "Chrome"},
        "user": {
            # Flat, not nested under "data" - this is the shape apps get from
            # sentry_sdk.set_user({"id": ..., "plan": "pro"}) in practice,
            # even though Sentry's docs put custom fields under "data".
            "id": "42", "email": "alice@example.com", "ip_address": "{{auto}}",
            "plan": "pro", "api_token": "sk-live-should-never-render",
        },
        "sdk": {
            "name": "sentry.python.django", "version": "2.18.0",
            "integrations": ["django", "logging", "stdlib"],
            "packages": [{"name": "pypi:sentry-sdk", "version": "2.18.0"}],
        },
        "request": {
            "url": "http://example.com/checkout/", "method": "POST",
            "query_string": "ref=email&coupon=SAVE10",
            "headers": {
                "User-Agent": "Mozilla/5.0 (test client)",
                "Content-Type": "application/x-www-form-urlencoded",
                "Authorization": "Bearer should-never-render",
                "Cookie": "sessionid=should-never-render",
            },
            "cookies": {"sessionid": "should-never-render"},
            "env": {"REMOTE_ADDR": "203.0.113.5", "HTTP_COOKIE": "sessionid=should-never-render"},
            "data": {"card_number": "4242", "password": "should-never-render"},
        },
        "breadcrumbs": {
            "values": [
                {"timestamp": now - 30, "category": "navigation", "level": "info", "message": "visited /cart/"},
                {"timestamp": now - 10, "category": "http", "level": "info",
                 "message": "GET /api/cart/", "data": {"status_code": 200}},
            ]
        },
        "contexts": {
            "browser": {"name": "Chrome", "version": "131.0"},
            "trace": {"trace_id": uuid.uuid4().hex, "span_id": uuid.uuid4().hex[:16]},
        },
        "extra": {"cart_total": -12.50, "promo_applied": "SAVE10"},
    }


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)
    public_key = sys.argv[1]
    project_id = sys.argv[2] if len(sys.argv) > 2 else "1"
    base_url = sys.argv[3] if len(sys.argv) > 3 else "http://localhost:8080"

    event = build_event()
    envelope = "\n".join([
        json.dumps({"event_id": event["event_id"]}),
        json.dumps({"type": "event"}),
        json.dumps(event),
    ]) + "\n"

    url = f"{base_url}/api/{project_id}/envelope/"
    req = urllib.request.Request(url, data=envelope.encode(), method="POST")
    req.add_header("X-Sentry-Auth", f"Sentry sentry_version=7, sentry_key={public_key}")
    req.add_header("Content-Type", "application/x-sentry-envelope")

    with urllib.request.urlopen(req) as resp:
        print(f"{resp.status} {resp.read().decode()}")
    print(f"Sent event_id={event['event_id']} to project {project_id} - check {base_url}/issues")


if __name__ == "__main__":
    main()
