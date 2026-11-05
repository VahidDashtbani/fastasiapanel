package web

import (
	"fmt"
	"html/template"
	"net/http"
)

type subStatusPageData struct {
	ClientEmail  string
	SubID        string
	SubURL       string
	StatusLabel  string
	StatusTone   string
	UsedText     string
	TotalText    string
	RemainText   string
	ExpireText   string
	UploadText   string
	DownloadText string
	ProgressPct  int
	RawPayload   string
}

func formatBytesEN(b int64) string {
	if b < 0 {
		b = 0
	}
	const (
		kb = 1024.0
		mb = kb * 1024.0
		gb = mb * 1024.0
		tb = gb * 1024.0
	)
	v := float64(b)
	switch {
	case v >= tb:
		return fmt.Sprintf("%.2f TB", v/tb)
	case v >= gb:
		return fmt.Sprintf("%.2f GB", v/gb)
	case v >= mb:
		return fmt.Sprintf("%.2f MB", v/mb)
	case v >= kb:
		return fmt.Sprintf("%.2f KB", v/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

var subStatusTemplate = template.Must(template.New("subStatus").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Subscription Status - {{.ClientEmail}}</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #0b0f14;
      --card: #121821;
      --card-2: #17202c;
      --border: #253244;
      --text: #e6edf3;
      --muted: #8b9bb0;
      --accent: #22c55e;
      --accent-soft: rgba(34, 197, 94, 0.14);
      --warn: #f59e0b;
      --danger: #ef4444;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      padding: 24px 16px;
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: var(--bg);
      color: var(--text);
      line-height: 1.5;
    }
    .wrap {
      max-width: 880px;
      margin: 0 auto;
    }
    .card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 20px;
      margin-bottom: 16px;
    }
    .header {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
    }
    .title {
      margin: 0;
      font-size: 1.2rem;
      font-weight: 700;
      word-break: break-all;
    }
    .badge {
      display: inline-flex;
      align-items: center;
      padding: 4px 10px;
      border-radius: 999px;
      font-size: 0.8rem;
      font-weight: 600;
      background: var(--accent-soft);
      color: var(--accent);
      border: 1px solid rgba(34, 197, 94, 0.35);
    }
    .badge.warn {
      color: var(--warn);
      background: rgba(245, 158, 11, 0.14);
      border-color: rgba(245, 158, 11, 0.35);
    }
    .badge.danger {
      color: var(--danger);
      background: rgba(239, 68, 68, 0.14);
      border-color: rgba(239, 68, 68, 0.35);
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 12px;
      margin-top: 16px;
    }
    .stat {
      background: var(--card-2);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 12px;
    }
    .stat-label {
      font-size: 0.78rem;
      color: var(--muted);
      margin-bottom: 4px;
    }
    .stat-val {
      font-size: 1rem;
      font-weight: 700;
      word-break: break-word;
    }
    .bar-wrap {
      margin-top: 14px;
      height: 8px;
      border-radius: 999px;
      background: var(--card-2);
      overflow: hidden;
      border: 1px solid var(--border);
    }
    .bar-fill {
      height: 100%;
      width: {{.ProgressPct}}%;
      background: var(--accent);
      transition: width 0.25s ease;
    }
    .url-box {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 14px;
    }
    .url-input {
      flex: 1 1 320px;
      background: var(--card-2);
      color: var(--text);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 9px 12px;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.85rem;
    }
    button {
      background: var(--card-2);
      color: var(--text);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 9px 14px;
      font-size: 0.85rem;
      font-weight: 600;
      cursor: pointer;
    }
    button:hover {
      border-color: var(--accent);
    }
    button.primary {
      background: var(--accent);
      color: #07110a;
      border-color: var(--accent);
    }
    .section-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 10px;
      margin-bottom: 12px;
    }
    .section-title {
      margin: 0;
      font-size: 1rem;
      font-weight: 700;
    }
    .cfg-list {
      display: flex;
      flex-direction: column;
      gap: 10px;
    }
    .cfg-item {
      background: var(--card-2);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 12px;
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    .cfg-top {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
    }
    .cfg-name {
      font-weight: 700;
      font-size: 0.92rem;
      word-break: break-all;
    }
    .cfg-actions {
      display: flex;
      gap: 8px;
      flex-shrink: 0;
    }
    .cfg-uri {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.78rem;
      color: var(--muted);
      word-break: break-all;
      max-height: 64px;
      overflow-y: auto;
    }
    pre.raw-box {
      margin: 0;
      padding: 12px;
      background: var(--card-2);
      border: 1px solid var(--border);
      border-radius: 10px;
      font-size: 0.78rem;
      white-space: pre-wrap;
      word-break: break-all;
      max-height: 240px;
      overflow: auto;
    }
    .modal-backdrop {
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.72);
      display: none;
      align-items: center;
      justify-content: center;
      padding: 16px;
      z-index: 50;
    }
    .modal-backdrop.open {
      display: flex;
    }
    .modal-card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 20px;
      max-width: 360px;
      width: 100%;
      text-align: center;
    }
    .qr-canvas {
      display: block;
      margin: 12px auto;
      background: #ffffff;
      padding: 8px;
      border-radius: 8px;
    }
    .toast {
      position: fixed;
      bottom: 20px;
      right: 20px;
      background: var(--accent);
      color: #07110a;
      font-weight: 700;
      font-size: 0.85rem;
      padding: 10px 14px;
      border-radius: 8px;
      opacity: 0;
      pointer-events: none;
      transform: translateY(8px);
      transition: all 0.2s ease;
      z-index: 60;
    }
    .toast.show {
      opacity: 1;
      transform: translateY(0);
    }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="card">
      <div class="header">
        <h1 class="title">{{.ClientEmail}}</h1>
        <span class="badge {{.StatusTone}}">{{.StatusLabel}}</span>
      </div>

      <div class="bar-wrap" aria-hidden="true">
        <div class="bar-fill"></div>
      </div>

      <div class="grid">
        <div class="stat">
          <div class="stat-label">Used / Total</div>
          <div class="stat-val">{{.UsedText}} / {{.TotalText}}</div>
        </div>
        <div class="stat">
          <div class="stat-label">Remaining</div>
          <div class="stat-val">{{.RemainText}}</div>
        </div>
        <div class="stat">
          <div class="stat-label">Expires</div>
          <div class="stat-val">{{.ExpireText}}</div>
        </div>
        <div class="stat">
          <div class="stat-label">Upload / Download</div>
          <div class="stat-val">{{.UploadText}} / {{.DownloadText}}</div>
        </div>
      </div>

      <div class="url-box">
        <input id="subUrlInput" class="url-input" type="text" readonly value="{{.SubURL}}" />
        <button id="copySubBtn" class="primary" type="button">Copy Subscription</button>
        <button id="copyAllBtn" type="button">Copy All Configs</button>
        <button id="subQrBtn" type="button">Config QR</button>
        <button id="toggleRawBtn" type="button">Raw Output</button>
      </div>
    </div>

    <div class="card">
      <div class="section-head">
        <h2 class="section-title">Decoded Configs <span id="cfgCount">0</span></h2>
      </div>
      <div id="cfgList" class="cfg-list"></div>
    </div>

    <div id="rawSection" class="card" style="display:none;">
      <div class="section-head">
        <h2 class="section-title">Raw Subscription Payload</h2>
      </div>
      <pre id="rawOutput" class="raw-box"></pre>
    </div>
  </div>

  <div id="qrModal" class="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="qrTitle">
    <div class="modal-card">
      <h3 id="qrTitle" style="margin:0 0 6px 0;">QR Code</h3>
      <div id="qrSubtitle" style="font-size:0.82rem;color:var(--muted);word-break:break-all;"></div>
      <img id="qrImg" class="qr-canvas" width="236" height="236" alt="QR Code" />
      <button id="closeQrBtn" type="button">Close</button>
    </div>
  </div>

  <div id="toast" class="toast">Copied</div>

  <script id="rawSubPayload" type="application/json">{{.RawPayload}}</script>
  <script>
    (function () {
      var ALLOWED_SCHEMES = [
        "vmess", "vless", "trojan", "ss", "ssr",
        "hysteria", "hysteria2", "hy2", "tuic",
        "wireguard", "wg", "vpn", "socks", "socks5",
        "anytls", "juicity", "naive+https", "tg"
      ];

      function showToast(msg) {
        var el = document.getElementById("toast");
        if (!el) return;
        el.textContent = msg || "Copied";
        el.classList.add("show");
        clearTimeout(window.__fapToastTimer);
        window.__fapToastTimer = setTimeout(function () {
          el.classList.remove("show");
        }, 1800);
      }

      function copyText(text, label) {
        if (!text) return;
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(function () {
            showToast(label || "Copied");
          }).catch(function () {
            fallbackCopy(text, label);
          });
        } else {
          fallbackCopy(text, label);
        }
      }

      function fallbackCopy(text, label) {
        var ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        try {
          document.execCommand("copy");
          showToast(label || "Copied");
        } catch (_) {}
        document.body.removeChild(ta);
      }

      function readRawPayload() {
        var scriptEl = document.getElementById("rawSubPayload");
        if (!scriptEl) return "";
        var rawText = scriptEl.textContent || "";
        try {
          var parsed = JSON.parse(rawText);
          if (typeof parsed === "string") return parsed;
        } catch (_) {}
        return rawText;
      }

      function tryDecodeBase64Body(input) {
        var cleaned = String(input || "").replace(/[\s\r\n\t]+/g, "");
        if (!cleaned || cleaned.length < 4) return null;
        if (!/^[A-Za-z0-9+/_=-]+$/.test(cleaned)) return null;

        var std = cleaned.replace(/-/g, "+").replace(/_/g, "/");
        var rem = std.length % 4;
        if (rem === 1) return null;
        if (rem > 0) {
          std += "=".repeat(4 - rem);
        }

        try {
          var bin = atob(std);
          var bytes = new Uint8Array(bin.length);
          for (var i = 0; i < bin.length; i++) {
            bytes[i] = bin.charCodeAt(i);
          }
          var decoded = new TextDecoder("utf-8", { fatal: false }).decode(bytes);
          if (!decoded || decoded.indexOf("\uFFFD") !== -1) {
            return null;
          }
          return decoded;
        } catch (_) {
          return null;
        }
      }

      function hasAllowedScheme(line) {
        var idx = line.indexOf("://");
        if (idx <= 0) return false;
        var scheme = line.slice(0, idx).toLowerCase();
        return ALLOWED_SCHEMES.indexOf(scheme) !== -1;
      }

      function decodeSubscriptionBody(rawBody) {
        var text = String(rawBody || "").trim();
        if (!text) return [];

        var decoded = tryDecodeBase64Body(text);
        if (decoded !== null && decoded.trim() !== "") {
          text = decoded.trim();
        }

        var parts = text.split(/[\r\n]+/);
        var links = [];
        for (var i = 0; i < parts.length; i++) {
          var ln = parts[i].trim();
          if (!ln || ln.charAt(0) === "#") continue;
          if (hasAllowedScheme(ln)) {
            links.push(ln);
          }
        }
        return links;
      }

      function extractConfigLabel(uri, index) {
        var hashIdx = uri.indexOf("#");
        if (hashIdx !== -1 && hashIdx < uri.length - 1) {
          var frag = uri.slice(hashIdx + 1);
          try {
            var name = decodeURIComponent(frag).trim();
            if (name) return name;
          } catch (_) {
            if (frag.trim()) return frag.trim();
          }
        }
        var schemeEnd = uri.indexOf("://");
        var prefix = schemeEnd > 0 ? uri.slice(0, schemeEnd).toUpperCase() : "NODE";
        return prefix + " #" + (index + 1);
      }


function subBasePrefix() {
        var p = window.location.pathname || "";
        var idx = p.indexOf("/sub/");
        return idx > 0 ? p.slice(0, idx) : "";
      }

      function renderRealQR(targetImg, text) {
        if (!targetImg) return;
        var val = String(text || "").trim();
        if (!val) {
          targetImg.removeAttribute("src");
          return;
        }
        targetImg.src = subBasePrefix() + "/sub/qr?data=" + encodeURIComponent(val);
      }

      function openQRModal(title, value) {
        var modal = document.getElementById("qrModal");
        var titleEl = document.getElementById("qrTitle");
        var subEl = document.getElementById("qrSubtitle");
        var canvas = document.getElementById("qrImg");
        if (!modal || !canvas) return;
        titleEl.textContent = title || "QR Code";
        subEl.textContent = value || "";
        renderRealQR(canvas, value || "");
        modal.classList.add("open");
      }

      var rawPayload = readRawPayload();
      var links = decodeSubscriptionBody(rawPayload);

      var countEl = document.getElementById("cfgCount");
      var listEl = document.getElementById("cfgList");
      var rawOutEl = document.getElementById("rawOutput");

      if (rawOutEl) {
        rawOutEl.textContent = rawPayload;
      }
      if (countEl) {
        countEl.textContent = String(links.length);
      }

      if (listEl) {
        listEl.innerHTML = "";
        if (links.length === 0) {
          var emptyDiv = document.createElement("div");
          emptyDiv.style.color = "var(--muted)";
          emptyDiv.textContent = "No share links found in subscription payload.";
          listEl.appendChild(emptyDiv);
        } else {
          links.forEach(function (uri, idx) {
            var item = document.createElement("div");
            item.className = "cfg-item";

            var top = document.createElement("div");
            top.className = "cfg-top";

            var nameEl = document.createElement("div");
            nameEl.className = "cfg-name";
            nameEl.textContent = extractConfigLabel(uri, idx);

            var actions = document.createElement("div");
            actions.className = "cfg-actions";

            var copyBtn = document.createElement("button");
            copyBtn.type = "button";
            copyBtn.textContent = "Copy";
            copyBtn.addEventListener("click", function () {
              copyText(uri, "Config copied");
            });

            var qrBtn = document.createElement("button");
            qrBtn.type = "button";
            qrBtn.textContent = "QR";
            qrBtn.addEventListener("click", function () {
              openQRModal(extractConfigLabel(uri, idx), uri);
            });

            actions.appendChild(copyBtn);
            actions.appendChild(qrBtn);

            top.appendChild(nameEl);
            top.appendChild(actions);

            var uriEl = document.createElement("div");
            uriEl.className = "cfg-uri";
            uriEl.textContent = uri;

            item.appendChild(top);
            item.appendChild(uriEl);
            listEl.appendChild(item);
          });
        }
      }

      var copySubBtn = document.getElementById("copySubBtn");
      if (copySubBtn) {
        copySubBtn.addEventListener("click", function () {
          var inp = document.getElementById("subUrlInput");
          copyText(inp ? inp.value : "", "Subscription URL copied");
        });
      }

      var copyAllBtn = document.getElementById("copyAllBtn");
      if (copyAllBtn) {
        copyAllBtn.addEventListener("click", function () {
          if (!links.length) {
            showToast("No configs to copy");
            return;
          }
          copyText(links.join("\n"), "All configs copied");
        });
      }

      var subQrBtn = document.getElementById("subQrBtn");
      if (subQrBtn) {
        subQrBtn.addEventListener("click", function () {
          var inp = document.getElementById("subUrlInput");
          var targetVal = (links && links.length > 0) ? links[0] : (inp ? inp.value : "");
          var targetTitle = (links && links.length > 0) ? extractConfigLabel(links[0], 0) : "Config QR";
          openQRModal(targetTitle, targetVal);
        });
      }

      var toggleRawBtn = document.getElementById("toggleRawBtn");
      var rawSection = document.getElementById("rawSection");
      if (toggleRawBtn && rawSection) {
        toggleRawBtn.addEventListener("click", function () {
          var isHidden = rawSection.style.display === "none" || !rawSection.style.display;
          rawSection.style.display = isHidden ? "block" : "none";
        });
      }

      var closeQrBtn = document.getElementById("closeQrBtn");
      var qrModal = document.getElementById("qrModal");
      if (closeQrBtn && qrModal) {
        closeQrBtn.addEventListener("click", function () {
          qrModal.classList.remove("open");
        });
        qrModal.addEventListener("click", function (e) {
          if (e.target === qrModal) {
            qrModal.classList.remove("open");
          }
        });
      }
    })();
  </script>
</body>
</html>`))

func renderSubStatusPage(w http.ResponseWriter, r *http.Request, data subStatusPageData) {
	_ = r
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := subStatusTemplate.Execute(w, data); err != nil {
		http.Error(w, "failed to render subscription page", http.StatusInternalServerError)
	}
}
