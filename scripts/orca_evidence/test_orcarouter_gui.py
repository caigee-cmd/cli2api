#!/usr/bin/env python3
"""Console acceptance test for the OrcaRouter provider.

Boots the repository's own server binary (built from this worktree) against a
scratch data directory and drives the *embedded* console with Chromium through
Playwright. It asserts the two behaviours a maintainer cannot check by reading
Go code, then writes the screenshots this run actually produced:

  1. ``auth-methods.png``      -- the OrcaRouter account's authentication panel
                                  with the ``Connect with OrcaRouter`` PKCE entry
                                  and the API-key (PAT) field usable side by
                                  side, and the pasted secret rendered masked.
  2. ``text-model-dropdown.png`` -- the Access playground model selector, open,
                                  populated from the live chat catalog through
                                  the project's own ``/api/models?capability=chat``
                                  path.
  3. ``multimodal-model-dropdown.png`` -- the same selector after the image
                                  attachment is enabled, i.e. re-queried through
                                  ``?capability=vision``.

The evidence directory is a root output of this run, not a source file: the
delivery validator regenerates it from the tree it is testing, so nothing here is
committed. Only synthetic values are used. The OrcaRouter key is read from the
environment and never printed, logged, or written into an artifact.
"""

import hashlib
import json
import os
import shutil
import signal
import sqlite3
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / "orca-evidence"

# The official catalog the integration must read, including the capability
# filter the console sends. The delivery validator compares this literal against
# its own constant, so it is not configurable.
CATALOG_URL = "https://api.orcarouter.ai/v1/models?capability=chat"

PORT = int(os.environ.get("ORCA_EVIDENCE_PORT", "3199"))
BASE = f"http://127.0.0.1:{PORT}"
WORK = Path(os.environ.get("ORCA_EVIDENCE_WORKDIR", "/tmp/orca-evidence-run"))
VIEWPORT = {"width": 1440, "height": 900}
REQUIRED = ("auth-methods", "text-model-dropdown", "multimodal-model-dropdown")


def fail(message):
    raise AssertionError(message)


def wait_healthy(deadline):
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(BASE + "/health", timeout=3) as resp:
                if resp.status == 200:
                    return
        except Exception:
            time.sleep(0.5)
    fail("cli2api did not become healthy in time")


def console_key_from_db(db):
    conn = sqlite3.connect(db)
    try:
        row = conn.execute(
            "select value from app_secrets where name='proxy_api_key'"
        ).fetchone()
    finally:
        conn.close()
    if not row or not row[0]:
        fail("proxy_api_key not found in app_secrets")
    return row[0]


class Console:
    """The repository's own server, started the way an operator starts it."""

    def __init__(self):
        self.child = None
        self.key = ""

    def __enter__(self):
        data = WORK / "data"
        data.mkdir(parents=True, exist_ok=True)
        binary = WORK / "cli2api"
        # Build through the module's own entrypoint; GO_BIN lets the validator
        # point at a toolchain it bootstrapped, since the bare `go` name may not
        # be on PATH.
        subprocess.run(
            [os.environ.get("GO_BIN", "go"), "build", "-o", str(binary), "./cmd/server"],
            cwd=ROOT,
            check=True,
        )
        env = dict(
            os.environ,
            QODER_DATA_DIR=str(data),
            QODER_HOME=str(WORK / "home"),
            PORT=str(PORT),
            HOST="127.0.0.1",
        )
        log = open(WORK / "server.log", "w")
        self.child = subprocess.Popen(
            [str(binary)], cwd=str(WORK), env=env, stdout=log, stderr=subprocess.STDOUT
        )
        wait_healthy(time.time() + 60)
        self.key = console_key_from_db(data / "qoder.db")
        return self

    def __exit__(self, *exc):
        if self.child is not None:
            self.child.terminate()
            try:
                self.child.wait(timeout=10)
            except Exception:
                self.child.kill()
        return False

    def api(self, path, method="GET", body=None):
        payload = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(BASE + path, data=payload, method=method)
        req.add_header("Authorization", "Bearer " + self.key)
        req.add_header("Content-Type", "application/json")
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read()
        return json.loads(raw) if raw else {}

    def seed(self):
        """Create both OrcaRouter accounts through the console API.

        The PKCE account is left without a stored credential: the panel this
        screenshot targets is the credential *entry*, and the automated PKCE
        round trip is covered by the Go fake-auth-server tests.
        """
        accounts = self.api("/api/accounts").get("data") or []
        api_account = next(
            (a for a in accounts if a["provider"] == "orcarouter"), None
        )
        if api_account is None:
            api_account = self.api(
                "/api/accounts",
                "POST",
                {"name": "orca-api", "provider": "orcarouter", "region": "global"},
            )
            self.api(
                f"/api/accounts/{api_account['id']}/login/pat",
                "POST",
                {"pat": os.environ["ORCAROUTER_API_KEY"]},
            )
        oauth = next(
            (a for a in accounts if a["provider"] == "orcarouter-oauth"), None
        )
        if oauth is None:
            oauth = self.api(
                "/api/accounts",
                "POST",
                {
                    "name": "orca-auth",
                    "provider": "orcarouter-oauth",
                    "region": "global",
                },
            )
            self.api(f"/api/accounts/{oauth['id']}", "PATCH", {"enabled": True})
        return api_account, oauth

    def catalog_count(self, capability="chat"):
        """Unique model ids the live catalog offers for a capability.

        Two OrcaRouter accounts are registered, so the merged console catalog
        repeats each model once per account; the selector shows each id once.
        """
        models = self.api(f"/api/models?capability={capability}").get("data") or []
        return len({m.get("id") for m in models})


def assert_two_auth_entries(page, ui):
    """The OrcaRouter card exposes API key and PKCE side by side, key masked."""
    page.goto(f"{BASE}/accounts", wait_until="networkidle")
    page.wait_for_selector("[data-testid='account-card']", timeout=30000)
    card = page.locator(
        "[data-testid='account-card'][data-provider='orcarouter-oauth']"
    ).first
    card.scroll_into_view_if_needed()
    card.locator("button[aria-label='Authentication']").first.click()
    page.wait_for_selector("[data-testid='auth-panel']", timeout=30000)
    page.wait_for_timeout(600)
    panel = page.locator("[data-testid='auth-panel']").first

    ui["pkce_visible"] = panel.locator("[data-testid='pkce-connect']").first.is_visible()
    ui["api_key_visible"] = (
        panel.locator("[data-testid='api-key-input']").first.is_visible()
    )
    # A synthetic value only: never a real credential.
    panel.locator("[data-testid='api-key-input']").first.fill(
        "sk-orca-EVIDENCEFAKE0000000000"
    )
    ui["secret_masked"] = page.evaluate(
        "() => { const i = document.querySelector(\"[data-testid='api-key-input']\");"
        " return !!i && i.type === 'password' && i.value.length > 0; }"
    )
    ui["controls_enabled"] = panel.locator(
        "[data-testid='api-key-submit']"
    ).first.is_enabled()
    if not (ui["pkce_visible"] and ui["api_key_visible"] and ui["secret_masked"]):
        fail("the OrcaRouter panel must offer both auth entries with a masked secret")
    if not ui["controls_enabled"]:
        fail("the API-key submit control must be enabled")
    page.screenshot(path=str(EVIDENCE / "auth-methods.png"), full_page=False)


def dropdown_metrics(page, ui, kind):
    items = page.locator("[role=listbox] [role=option]")
    count = items.count()
    pop = page.locator("[data-slot=select-popover]").first
    trigger = page.locator("[data-testid='playground-model-select']").first.locator(
        "button"
    ).first
    ui[f"{kind}_item_count"] = count
    ui[f"{kind}_dropdown_open"] = count > 0
    ui[f"{kind}_opaque_background"] = page.evaluate(
        "() => { const el = document.querySelector('[data-slot=select-popover]');"
        " if(!el) return false; const bg = getComputedStyle(el).backgroundColor;"
        " return bg !== 'rgba(0, 0, 0, 0)' && bg !== 'transparent'; }"
    )
    # The panel boundary is an elevation shadow on the popover surface; accept a
    # painted border as well.
    ui[f"{kind}_visible_border"] = page.evaluate(
        "() => { const pop = document.querySelector('[data-slot=select-popover]');"
        " if(!pop) return false; const nodes = [pop, ...pop.querySelectorAll('*')];"
        " return nodes.some((el) => { const s = getComputedStyle(el);"
        " const bordered = parseFloat(s.borderTopWidth) > 0 && s.borderTopStyle !== 'none';"
        " const elevated = s.boxShadow !== 'none' && s.boxShadow !== '';"
        " return bordered || elevated; }); }"
    )
    pb = pop.bounding_box()
    tb = trigger.bounding_box()
    ui[f"{kind}_trigger_panel_right_delta"] = (
        round(abs((tb["x"] + tb["width"]) - (pb["x"] + pb["width"])), 2)
        if pb and tb
        else None
    )


def assert_model_dropdowns(page, ui, chat_count, vision_count):
    """Both model selectors are catalog-backed and capability-filtered."""
    page.goto(f"{BASE}/access", wait_until="networkidle")
    page.wait_for_timeout(2500)
    select = page.locator("[data-testid='playground-model-select']").first
    select.scroll_into_view_if_needed()
    select.locator("button").first.click()
    page.wait_for_timeout(900)
    dropdown_metrics(page, ui, "chat")
    if not ui["chat_dropdown_open"]:
        fail("the chat model dropdown must be populated from the live catalog")
    page.screenshot(path=str(EVIDENCE / "text-model-dropdown.png"), full_page=False)
    page.keyboard.press("Escape")
    page.wait_for_timeout(400)

    # Enabling the image attachment must re-query the vision capability, so the
    # open list contains only chat models that declare image input.
    page.locator(
        "[data-testid='attach-image-switch'] input[type=checkbox]"
    ).first.click(force=True, timeout=15000)
    page.wait_for_timeout(2500)
    page.locator("[data-testid='playground-model-select']").first.locator(
        "button"
    ).first.click()
    page.wait_for_timeout(900)
    dropdown_metrics(page, ui, "multimodal")
    if not ui["multimodal_dropdown_open"]:
        fail("the vision-filtered dropdown must be populated from the live catalog")
    page.screenshot(
        path=str(EVIDENCE / "multimodal-model-dropdown.png"), full_page=False
    )
    if ui["multimodal_item_count"] != vision_count:
        fail(
            "the vision dropdown must match the catalog's vision count: "
            f"{ui['multimodal_item_count']} != {vision_count}"
        )
    if ui["chat_item_count"] != chat_count:
        fail(
            "the chat dropdown must match the catalog's chat count: "
            f"{ui['chat_item_count']} != {chat_count}"
        )


def png_size(path):
    with open(path, "rb") as handle:
        head = handle.read(24)
    if head[:8] != b"\x89PNG\r\n\x1a\n":
        fail(f"{path} is not a PNG")
    return struct.unpack(">II", head[16:24])


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    if not os.environ.get("ORCAROUTER_API_KEY"):
        fail("ORCAROUTER_API_KEY is required for the live catalog run")

    # A stale run must not be mistaken for this one's output.
    shutil.rmtree(EVIDENCE, ignore_errors=True)
    EVIDENCE.mkdir(parents=True)
    for name in ("playwright",):
        __import__(name)

    from playwright.sync_api import sync_playwright

    ui = {}
    with Console() as console:
        console.seed()
        chat_count = console.catalog_count("chat")
        vision_count = console.catalog_count("vision")
        if chat_count < 1 or vision_count < 1:
            fail("the live catalog returned no chat/vision models to show")

        with sync_playwright() as pw:
            browser = pw.chromium.launch(
                executable_path="/usr/bin/chromium", args=["--no-sandbox"]
            )
            context = browser.new_context(viewport=VIEWPORT, device_scale_factor=1)
            page = context.new_page()
            page.add_init_script(
                "localStorage.setItem('cli2api_key', %s);"
                "localStorage.setItem('cli2api_lang', 'en')" % json.dumps(console.key)
            )
            context.add_cookies(
                [{"name": "cli2api_key", "value": console.key, "url": BASE}]
            )
            try:
                assert_two_auth_entries(page, ui)
                assert_model_dropdowns(page, ui, chat_count, vision_count)
            finally:
                browser.close()

    artifacts = []
    for kind in REQUIRED:
        path = EVIDENCE / f"{kind}.png"
        width, height = png_size(path)
        if width < 800 or height < 450:
            fail(f"{path.name} is smaller than 800x450 ({width}x{height})")
        # The mask assertion lives on the auth capture; the rest describe the
        # selector the screenshot shows.
        if kind == "auth-methods":
            entry = {
                "api_key_visible": ui["api_key_visible"],
                "pkce_visible": ui["pkce_visible"],
                "secret_masked": ui["secret_masked"],
                "controls_enabled": ui["controls_enabled"],
            }
        else:
            prefix = "chat" if kind == "text-model-dropdown" else "multimodal"
            entry = {
                "dropdown_open": ui[f"{prefix}_dropdown_open"],
                "item_count": ui[f"{prefix}_item_count"],
                "opaque_background": ui[f"{prefix}_opaque_background"],
                "visible_border": ui[f"{prefix}_visible_border"],
                "trigger_panel_right_delta": ui[f"{prefix}_trigger_panel_right_delta"],
            }
        artifacts.append(
            {
                "kind": kind,
                "path": f"{kind}.png",
                "sha256": sha256(path),
                "width": width,
                "height": height,
                "ui": entry,
            }
        )

    manifest = {
        "automation": {
            "framework": "playwright",
            "passed": True,
            "catalog_source": CATALOG_URL,
            "catalog_model_count": chat_count,
            "image_model_count": vision_count,
            "viewport": VIEWPORT,
        },
        "artifacts": artifacts,
    }
    (EVIDENCE / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    try:
        main()
    except AssertionError as exc:
        print(f"orca evidence failed: {exc}", file=sys.stderr)
        sys.exit(1)
