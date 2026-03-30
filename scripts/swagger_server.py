#!/usr/bin/env python3
"""Lokalni Swagger UI server za Banka Backend API."""

import http.server
import json
import os
import threading
import webbrowser
from pathlib import Path

ROOT = Path(__file__).parent.parent
BANK_JSON = ROOT / "docs/swagger/proto/banka/banka.swagger.json"
USER_JSON = ROOT / "docs/swagger/proto/user/user.swagger.json"
PORT = 8099

SWAGGER_UI_HTML = """<!DOCTYPE html>
<html>
<head>
  <title>Banka Backend — API Docs</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; background: #1a1a2e; }
    .topbar { display:none !important; }
    #selector {
      position: fixed; top: 0; left: 0; right: 0; z-index: 999;
      background: #16213e;
      padding: 10px 20px;
      display: flex; align-items: center; gap: 16px;
      border-bottom: 2px solid #0f3460;
      box-shadow: 0 2px 8px rgba(0,0,0,.5);
    }
    #selector span { color: #e2e8f0; font-family: sans-serif; font-weight: 600; font-size: 15px; }
    #selector select {
      padding: 6px 12px; border-radius: 8px;
      background: #0f3460; color: #e2e8f0;
      border: 1px solid #334155; font-size: 14px; cursor: pointer;
    }
    #swagger-ui { margin-top: 52px; }
  </style>
</head>
<body>
  <div id="selector">
    <span>🏦 Banka API</span>
    <select id="spec-select" onchange="loadSpec(this.value)">
      <option value="/bank.json">Bank Service</option>
      <option value="/user.json">User Service</option>
    </select>
  </div>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    var ui;
    function loadSpec(url) {
      if (ui) {
        ui.specActions.updateUrl(url);
        ui.specActions.download(url);
      } else {
        ui = SwaggerUIBundle({
          url: url,
          dom_id: '#swagger-ui',
          presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
          layout: "BaseLayout",
          deepLinking: true,
          defaultModelsExpandDepth: 1,
          defaultModelExpandDepth: 1,
        });
      }
    }
    window.onload = function() { loadSpec('/bank.json'); };
  </script>
</body>
</html>"""


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass  # bez verbose logova

    def do_GET(self):
        if self.path == "/" or self.path == "/index.html":
            self._respond(200, "text/html", SWAGGER_UI_HTML.encode())
        elif self.path == "/bank.json":
            self._respond(200, "application/json", BANK_JSON.read_bytes())
        elif self.path == "/user.json":
            self._respond(200, "application/json", USER_JSON.read_bytes())
        else:
            self._respond(404, "text/plain", b"Not found")

    def _respond(self, code, ctype, body):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    server = http.server.HTTPServer(("localhost", PORT), Handler)
    url = f"http://localhost:{PORT}"
    print(f"  Swagger UI → {url}")
    print(f"  Bank Service  → {url}/bank.json")
    print(f"  User Service  → {url}/user.json")
    print(f"  Ctrl+C da zaustaviš")
    threading.Timer(0.5, lambda: webbrowser.open(url)).start()
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n  Swagger server zaustavljen.")
