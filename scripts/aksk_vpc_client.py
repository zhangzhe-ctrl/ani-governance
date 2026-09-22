#!/usr/bin/env python3
"""ANI VPC HMAC client. Only the Python standard library is required."""
import hashlib
import hmac
import json
import os
import re
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def sign_vpc(access_key, secret_key, vpc_id, timestamp=None):
    if not re.fullmatch(r"ak-[A-Za-z0-9_-]+", access_key):
        raise ValueError("invalid access key format")
    if not secret_key or secret_key != secret_key.strip():
        raise ValueError("secret key is empty or has surrounding whitespace")
    if not re.fullmatch(r"vpc_[0-9a-f]{32}", vpc_id):
        raise ValueError("invalid VPC ID")
    ts = str(int(time.time()) if timestamp is None else timestamp)
    if not re.fullmatch(r"[1-9][0-9]*", ts) or int(ts) > 2**63 - 1:
        raise ValueError("timestamp must be positive Unix seconds")
    path = "/api/v1/networks/vpcs/" + vpc_id
    canonical = "\n".join([
        "ANI-HMAC-SHA256",
        "GET",
        path,
        "",  # Empty query string; keep this line.
        access_key,
        ts,
        hashlib.sha256(b"").hexdigest(),
    ]).encode("utf-8")  # No trailing newline.
    signature = hmac.new(
        secret_key.encode("utf-8"), canonical, hashlib.sha256
    ).hexdigest()
    return path, {
        "X-Access-Key": access_key,
        "X-Timestamp": ts,
        "X-Signature": signature,
        "Accept": "application/json",
    }


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def get_vpc(base_url, access_key, secret_key, vpc_id):
    u = urllib.parse.urlsplit(base_url)
    if (u.scheme not in ("https", "http") or not u.hostname
            or u.username is not None or u.password is not None
            or u.path not in ("", "/") or u.query or u.fragment):
        raise ValueError("base URL must be an origin, e.g. https://host:port")
    if u.scheme == "http" and os.getenv("ANI_ALLOW_HTTP_FOR_TEST") != "1":
        raise ValueError("HTTP requires ANI_ALLOW_HTTP_FOR_TEST=1 in an isolated lab")
    path, headers = sign_vpc(access_key, secret_key, vpc_id)
    url = urllib.parse.urlunsplit((u.scheme, u.netloc, path, "", ""))
    req = urllib.request.Request(url, headers=headers, method="GET")
    opener = urllib.request.build_opener(
        NoRedirect(),
        urllib.request.HTTPSHandler(context=ssl.create_default_context()),
    )
    with opener.open(req, timeout=10) as response:
        result = json.load(response)
    vpc = result.get("vpc") if isinstance(result, dict) else None
    if not isinstance(vpc, dict) or vpc.get("id") != vpc_id:
        raise ValueError("response does not contain the requested VPC")
    return result


def self_test():
    _, headers = sign_vpc(
        "ak-doc-example", "sk-doc-example-not-a-real-secret",
        "vpc_0123456789abcdef0123456789abcdef", 1700000000,
    )
    expected = "7df61f06b799ae477b51975097825e2e154ff62dffc46842964a51ed1afaebeb"
    if not hmac.compare_digest(headers["X-Signature"], expected):
        raise RuntimeError("signature vector mismatch")
    print("signature self-test: PASS (offline only)")


if __name__ == "__main__":
    if sys.argv[1:] == ["--self-test"]:
        self_test()
    elif sys.argv[1:]:
        raise SystemExit("usage: python3 aksk_vpc_client.py [--self-test]")
    else:
        try:
            result = get_vpc(
                os.environ["ANI_BASE_URL"], os.environ["ANI_ACCESS_KEY"],
                os.environ["ANI_SECRET_KEY"], os.environ["ANI_VPC_ID"],
            )
            print(json.dumps(result, ensure_ascii=False, indent=2))
        except urllib.error.HTTPError as exc:
            raise SystemExit("Governance returned HTTP " + str(exc.code)) from None
        except (KeyError, ValueError, urllib.error.URLError) as exc:
            raise SystemExit(str(exc)) from None
