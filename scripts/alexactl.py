#!/usr/bin/env python3
"""Tiny CLI client for the Alexa API — used for end-to-end testing.

Usage:
  alexactl.py [--url URL] [--user U --password P] send "message" [--timeout S] [--answer TEXT] [--handback] [--attach FILE]
  alexactl.py ... feed [-n N] | report ID | stop | compact | memories | schedules
"""
import argparse
import http.cookiejar
import json
import os
import time
import urllib.error
import urllib.request


class Client:
    def __init__(self, base, user, password):
        self.base = base.rstrip("/")
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
        self.login(user, password)

    def req(self, method, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        r = urllib.request.Request(self.base + path, data=data, method=method)
        r.add_header("Content-Type", "application/json")
        r.add_header("X-Alexa", "1")
        try:
            with self.opener.open(r, timeout=120) as resp:
                raw = resp.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as e:
            raise SystemExit(f"{method} {path} -> {e.code}: {e.read().decode()}")

    def upload(self, path):
        boundary = "----alexactl" + str(int(time.time() * 1000))
        name = os.path.basename(path)
        with open(path, "rb") as f:
            content = f.read()
        body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{name}\"\r\n"
                f"Content-Type: application/octet-stream\r\n\r\n").encode() + content + f"\r\n--{boundary}--\r\n".encode()
        r = urllib.request.Request(self.base + "/api/uploads", data=body, method="POST")
        r.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
        r.add_header("X-Alexa", "1")
        try:
            with self.opener.open(r, timeout=60) as resp:
                return json.loads(resp.read())
        except urllib.error.HTTPError as e:
            raise SystemExit(f"upload {path} -> {e.code}: {e.read().decode()}")

    def login(self, user, password):
        setup = self.req("GET", "/api/setup")
        if setup.get("needsSetup"):
            self.req("POST", "/api/auth/signup", {"username": user, "password": password, "displayName": user, "timezone": "America/Los_Angeles"})
        else:
            self.req("POST", "/api/auth/login", {"username": user, "password": password})


def line(c, e, t0, show_reports=True):
    """Prints one feed entry the way the user would see it."""
    at = f"[{(e['updatedAt'] - t0) / 1000:6.1f}s]" if t0 else ""
    k = e["kind"]
    if k == "step":
        label = e.get("doneText") if e.get("status") == "done" and e.get("doneText") else e["text"]
        print(f"  {at} {e.get('status', ''):6} {label}", flush=True)
    elif k == "user":
        print(f"  {at} YOU:   {e['text']}", flush=True)
    elif k == "say":
        print(f"  {at} ALEXA: {e['text']}", flush=True)
    elif k == "ask":
        print(f"  {at} ASK:   {e['text']} {(e.get('data') or {}).get('options') or ''} ({e.get('status')})", flush=True)
    elif k == "report":
        rid = (e.get("data") or {}).get("id")
        print(f"  {at} REPORT {rid}: {e['text']}", flush=True)
        if show_reports and rid:
            print_report(c.req("GET", f"/api/reports/{rid}"))
    else:
        print(f"  {at} {k.upper()}: {e['text']}", flush=True)


def print_report(r):
    print(f"\n    # {r['title']}  ({r['format']})")
    for ln in (r.get("content") or "").splitlines():
        print("    " + ln)
    d = r.get("data") or {}
    for it in d.get("items") or []:
        print(f"    - {it.get('label')} | {it.get('detail', '')}")
    for e in d.get("events") or []:
        print("    EVENT:", e)
    for f in d.get("files") or []:
        print("    FILE:", f)
    for l in d.get("sources") or []:
        print("    SOURCE:", l.get("title"), l.get("url"))
    print(flush=True)


def follow(c, after, timeout, answer=None, handback=False):
    """Prints new and changed entries until Alexa is done or needs the user."""
    printed = {}
    t0 = time.time() * 1000
    start = time.time()
    began = False
    while time.time() - start < timeout:
        s = c.req("GET", "/api/session")
        st = s["state"]
        for e in s["feed"]:
            if e["id"] <= after or e["kind"] == "user":
                continue
            ver = (e.get("status"), e["updatedAt"])
            if printed.get(e["id"]) == ver or (e["kind"] == "step" and e.get("status") == "running"):
                continue
            printed[e["id"]] = ver
            line(c, e, t0)
        if st["status"] != "idle" or printed:
            began = True
        if st["status"] == "needs":
            needs = st.get("needs") or {}
            if needs.get("kind") == "takeover" and handback:
                c.req("POST", "/api/handback", {"note": "I can't do that right now; carry on without it if you can."})
                handback = False
            elif needs.get("kind") == "question" and answer is not None:
                print(f"  answering: {answer}", flush=True)
                c.req("POST", "/api/messages", {"text": answer})
                answer = None
            else:
                print(f"  >>> NEEDS ({needs.get('kind')}): {needs.get('message')}", flush=True)
                return st
        elif st["status"] == "idle" and began:
            return st
        time.sleep(1.5)
    print("  !!! timed out", flush=True)
    return st


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", default=os.environ.get("ALEXA_URL", "http://127.0.0.1:8090"))
    ap.add_argument("--user", default=os.environ.get("ALEXA_USER", "alex"))
    ap.add_argument("--password", default=os.environ.get("ALEXA_PASSWORD"), help="or set ALEXA_PASSWORD")
    sub = ap.add_subparsers(dest="cmd", required=True)
    sd = sub.add_parser("send")
    sd.add_argument("text")
    sd.add_argument("--timeout", type=int, default=900)
    sd.add_argument("--answer", help="reply to Alexa's first question with this")
    sd.add_argument("--handback", action="store_true", help="hand the browser straight back if Alexa asks you to take over")
    sd.add_argument("--attach", action="append", default=[], help="file to upload and attach (repeatable)")
    fd = sub.add_parser("feed")
    fd.add_argument("-n", type=int, default=30)
    rp = sub.add_parser("report")
    rp.add_argument("id")
    for name in ("stop", "compact", "memories", "schedules"):
        sub.add_parser(name)
    a = ap.parse_args()
    if not a.password:
        ap.error("pass --password or set ALEXA_PASSWORD")
    c = Client(a.url, a.user, a.password)
    if a.cmd == "send":
        feed = c.req("GET", "/api/session")["feed"]
        after = max((e["id"] for e in feed), default=0)
        atts = [c.upload(p) for p in a.attach]
        c.req("POST", "/api/messages", {"text": a.text, "attachments": atts})
        st = follow(c, after, a.timeout, a.answer, a.handback)
        print(f"\nSTATUS: {st['status']}  context: {st['contextTokens']} tokens  compactions: {st['compactions']}")
    elif a.cmd == "feed":
        for e in c.req("GET", "/api/session")["feed"][-a.n:]:
            line(c, e, 0, show_reports=False)
    elif a.cmd == "report":
        print_report(c.req("GET", f"/api/reports/{a.id}"))
    elif a.cmd == "stop":
        c.req("POST", "/api/stop")
    elif a.cmd == "compact":
        c.req("POST", "/api/session/compact")
        st = c.req("GET", "/api/session")["state"]
        print(f"context: {st['contextTokens']} tokens  compactions: {st['compactions']}")
    elif a.cmd == "memories":
        print(json.dumps(c.req("GET", "/api/memories"), indent=2))
    elif a.cmd == "schedules":
        print(json.dumps(c.req("GET", "/api/schedules"), indent=2))


if __name__ == "__main__":
    main()
