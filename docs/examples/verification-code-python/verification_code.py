#!/usr/bin/env python3
"""Wait for a verification email in a Phantom Mail mailbox and print the code.

The Python version of the pattern in docs/testing-verification-flows.md. It uses
only the standard library (Python 3.9 or newer).

    python3 docs/examples/verification-code-python/verification_code.py --mailbox signup-123

Exit status 0 means a code was printed; 1 means no email arrived in time or it
contained no code; 2 means a usage or connection error.
"""

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

UNITS = {"ms": 0.001, "s": 1, "m": 60}


class NoCode(Exception):
    """No email arrived, or it held no code (exit status 1)."""


def duration(text):
    m = re.fullmatch(r"(\d+(?:\.\d+)?)(ms|s|m)", text)
    if not m:
        raise argparse.ArgumentTypeError("%r is not a duration such as 30s" % text)
    return float(m.group(1)) * UNITS[m.group(2)]


def get(url, token, timeout):
    """Return (status, parsed JSON) for a GET; 204 gives (204, None)."""
    req = urllib.request.Request(url)
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read()
            if resp.status == 204:
                return 204, None
            return resp.status, json.loads(body)
    except urllib.error.HTTPError as e:
        raise RuntimeError(
            "%s returned %s %s: %s"
            % (url, e.code, e.reason, e.read().decode("utf-8", "replace").strip())
        )


def wait_for_code(api, box, timeout, pattern, token):
    secs = min(max(int(timeout), 1), 60)
    base = (
        api.rstrip("/")
        + "/api/v1/mailboxes/"
        + urllib.parse.quote(box, safe="")
        + "/messages"
    )
    status, listing = get("%s/wait?timeout=%d" % (base, secs), token, secs + 10)
    if status == 204 or not listing.get("messages"):
        raise NoCode("no email arrived in mailbox %r within %ss" % (box, secs))
    _, msg = get(base + "/" + listing["messages"][0]["id"], token, secs + 10)
    for field in (msg.get("subject"), msg.get("text"), msg.get("html")):
        m = pattern.search(field or "")
        if m:
            return m.group(0)
    raise NoCode(
        "no code matching %r in the email %r" % (pattern.pattern, msg.get("subject"))
    )


def main():
    parser = argparse.ArgumentParser(
        description="Print the verification code from a Phantom Mail mailbox."
    )
    parser.add_argument("--mailbox", required=True, help="mailbox name to watch")
    parser.add_argument(
        "--api",
        default="http://localhost:8080",
        help="base URL of the Phantom Mail instance",
    )
    parser.add_argument(
        "--timeout",
        type=duration,
        default=30,
        help="how long to wait for the email, e.g. 30s (default 30s)",
    )
    parser.add_argument(
        "--pattern",
        default=r"\b\d{6}\b",
        help=r"regular expression matching the code (default \b\d{6}\b)",
    )
    try:
        args = parser.parse_args()  # usage errors exit with status 2
        pattern = re.compile(args.pattern)
    except re.error as e:
        print("bad --pattern:", e, file=sys.stderr)
        return 2
    try:
        print(
            wait_for_code(
                args.api,
                args.mailbox,
                args.timeout,
                pattern,
                os.environ.get("PM_API_TOKEN", ""),
            )
        )
    except NoCode as e:
        print(e, file=sys.stderr)
        return 1
    except (RuntimeError, OSError, ValueError) as e:
        print(e, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
