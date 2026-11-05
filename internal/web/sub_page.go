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
        <button id="subQrBtn" type="button">Subscription QR</button>
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
      <canvas id="qrCanvas" class="qr-canvas" width="220" height="220"></canvas>
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


function fapMakeScannableQR(text, sizePx) {
  var px = sizePx || 196;
  var raw = String(text || "");
  var utf8 = unescape(encodeURIComponent(raw));
  var bytes = [];
  for (var i = 0; i < utf8.length; i++) {
    bytes.push(utf8.charCodeAt(i) & 0xff);
  }

  // Versions 1..10 (ECC Level M) byte capacity
  var caps = [0, 14, 26, 42, 62, 84, 106, 122, 152, 180, 213];
  var totalCodewords = [0, 26, 44, 70, 100, 134, 172, 196, 242, 292, 346];
  var ecPerBlock = [0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26];
  var blocksCount = [0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5];

  var ver = 0;
  for (var v = 1; v <= 10; v++) {
    if (bytes.length <= caps[v]) {
      ver = v;
      break;
    }
  }
  if (ver === 0) {
    throw new Error("Payload too long for offline QR v1-v10");
  }

  var n = ver * 4 + 17;
  var dataCapacity = totalCodewords[ver] - ecPerBlock[ver] * blocksCount[ver];

  // Bit stream: mode 0100 (byte) + char count (8 or 16 bits) + payload + terminator + pad
  var bits = [];
  function pushBits(val, len) {
    for (var b = len - 1; b >= 0; b--) {
      bits.push((val >>> b) & 1);
    }
  }
  pushBits(4, 4);
  pushBits(bytes.length, ver >= 10 ? 16 : 8);
  for (var i = 0; i < bytes.length; i++) {
    pushBits(bytes[i], 8);
  }
  var maxBits = dataCapacity * 8;
  var termLen = Math.min(4, Math.max(0, maxBits - bits.length));
  pushBits(0, termLen);
  while (bits.length % 8 !== 0) bits.push(0);
  var padBytes = [0xec, 0x11];
  var padIdx = 0;
  while (bits.length < maxBits) {
    pushBits(padBytes[padIdx % 2], 8);
    padIdx++;
  }

  var dataCodewords = [];
  for (var i = 0; i < dataCapacity; i++) {
    var cw = 0;
    for (var b = 0; b < 8; b++) {
      cw = (cw << 1) | (bits[i * 8 + b] || 0);
    }
    dataCodewords.push(cw);
  }

  // GF(256) Reed-Solomon generator
  var gfExp = new Array(512);
  var gfLog = new Array(256);
  var x = 1;
  for (var i = 0; i < 255; i++) {
    gfExp[i] = x;
    gfLog[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= 0x11d;
  }
  for (var i = 255; i < 512; i++) gfExp[i] = gfExp[i - 255];

  function gfMul(a, b) {
    if (a === 0 || b === 0) return 0;
    return gfExp[(gfLog[a] + gfLog[b]) % 255];
  }

  function rsGenerator(ecLen) {
    var poly = [1];
    for (var i = 0; i < ecLen; i++) {
      var next = new Array(poly.length + 1).fill(0);
      var root = gfExp[i];
      for (var j = 0; j < poly.length; j++) {
        next[j] ^= poly[j];
        next[j + 1] ^= gfMul(poly[j], root);
      }
      poly = next;
    }
    return poly;
  }

  function rsEncode(msg, ecLen) {
    var gen = rsGenerator(ecLen);
    var rem = new Array(ecLen).fill(0);
    for (var i = 0; i < msg.length; i++) {
      var factor = msg[i] ^ rem[0];
      rem.shift();
      rem.push(0);
      for (var j = 0; j < ecLen; j++) {
        rem[j] ^= gfMul(gen[j + 1], factor);
      }
    }
    return rem;
  }

  var bc = blocksCount[ver];
  var ecLen = ecPerBlock[ver];
  var shortLen = Math.floor(dataCapacity / bc);
  var longCount = dataCapacity % bc;
  var dataBlocks = [];
  var ecBlocks = [];
  var offset = 0;
  for (var b = 0; b < bc; b++) {
    var len = shortLen + (b >= bc - longCount ? 1 : 0);
    var blk = dataCodewords.slice(offset, offset + len);
    offset += len;
    dataBlocks.push(blk);
    ecBlocks.push(rsEncode(blk, ecLen));
  }

  var interleaved = [];
  var maxDataLen = shortLen + (longCount > 0 ? 1 : 0);
  for (var i = 0; i < maxDataLen; i++) {
    for (var b = 0; b < bc; b++) {
      if (i < dataBlocks[b].length) interleaved.push(dataBlocks[b][i]);
    }
  }
  for (var i = 0; i < ecLen; i++) {
    for (var b = 0; b < bc; b++) {
      interleaved.push(ecBlocks[b][i]);
    }
  }

  var finalBits = [];
  for (var i = 0; i < interleaved.length; i++) {
    for (var b = 7; b >= 0; b--) {
      finalBits.push((interleaved[i] >>> b) & 1);
    }
  }
  var remBits = totalCodewords[ver] * 8 - finalBits.length;
  for (var i = 0; i < remBits; i++) finalBits.push(0);

  // Matrix setup
  var grid = Array.from({ length: n }, function () { return new Array(n).fill(0); });
  var reserved = Array.from({ length: n }, function () { return new Array(n).fill(false); });

  function setModule(r, c, dark, isReserved) {
    if (r < 0 || r >= n || c < 0 || c >= n) return;
    grid[r][c] = dark ? 1 : 0;
    if (isReserved) reserved[r][c] = true;
  }

  function placeFinder(r0, c0) {
    for (var r = -1; r <= 7; r++) {
      for (var c = -1; c <= 7; c++) {
        var rr = r0 + r, cc = c0 + c;
        if (rr < 0 || rr >= n || cc < 0 || cc >= n) continue;
        var inBox = r >= 0 && r <= 6 && c >= 0 && c <= 6;
        var border = r === 0 || r === 6 || c === 0 || c === 6;
        var center = r >= 2 && r <= 4 && c >= 2 && c <= 4;
        setModule(rr, cc, inBox && (border || center), true);
      }
    }
  }

  placeFinder(0, 0);
  placeFinder(0, n - 7);
  placeFinder(n - 7, 0);

  // Timing patterns
  for (var i = 8; i < n - 8; i++) {
    var dark = (i % 2 === 0);
    setModule(6, i, dark, true);
    setModule(i, 6, dark, true);
  }

  // Alignment pattern for v >= 2
  if (ver >= 2) {
    var alignPos = [0, 0, 18, 22, 26, 30, 34, 22, 24, 26, 28][ver];
    if (alignPos > 0) {
      for (var r = -2; r <= 2; r++) {
        for (var c = -2; c <= 2; c++) {
          var rr = alignPos + r, cc = alignPos + c;
          if (!reserved[rr][cc]) {
            var isDark = Math.abs(r) === 2 || Math.abs(c) === 2 || (r === 0 && c === 0);
            setModule(rr, cc, isDark, true);
          }
        }
      }
    }
  }

  // Dark module
  setModule(n - 8, 8, true, true);

  // Reserve format info cells
  for (var i = 0; i < 9; i++) {
    if (i < n) {
      reserved[8][i] = true;
      reserved[i][8] = true;
    }
  }
  for (var i = 0; i < 8; i++) {
    reserved[8][n - 1 - i] = true;
    reserved[n - 1 - i][8] = true;
  }

  // Zigzag data placement
  var bitCursor = 0;
  var upward = true;
  for (var col = n - 1; col >= 1; col -= 2) {
    if (col === 6) col = 5;
    for (var step = 0; step < n; step++) {
      var r = upward ? (n - 1 - step) : step;
      for (var dc = 0; dc >= -1; dc--) {
        var c = col + dc;
        if (c < 0 || reserved[r][c]) continue;
        var bit = bitCursor < finalBits.length ? finalBits[bitCursor++] : 0;
        grid[r][c] = bit;
      }
    }
    upward = !upward;
  }

  // Mask 0: (r + c) % 2 === 0
  for (var r = 0; r < n; r++) {
    for (var c = 0; c < n; c++) {
      if (!reserved[r][c] && ((r + c) % 2 === 0)) {
        grid[r][c] ^= 1;
      }
    }
  }

  // Format bits for ECC M (00) + Mask 0 (000) => data 00000
  var fmtData = 0;
  var rem = fmtData << 10;
  var genPoly = 0x537;
  for (var b = 14; b >= 10; b--) {
    if ((rem >>> b) & 1) {
      rem ^= genPoly << (b - 10);
    }
  }
  var fmtBits = (((fmtData << 10) | rem) ^ 0x5412) & 0x7fff;

  function getFmtBit(idx) {
    return (fmtBits >>> (14 - idx)) & 1;
  }

  for (var i = 0; i <= 5; i++) grid[8][i] = getFmtBit(i);
  grid[8][7] = getFmtBit(6);
  grid[8][8] = getFmtBit(7);
  grid[7][8] = getFmtBit(8);
  for (var i = 9; i < 15; i++) grid[14 - i][8] = getFmtBit(i);

  for (var i = 0; i < 8; i++) grid[n - 1 - i][8] = getFmtBit(i);
  for (var i = 8; i < 15; i++) grid[8][n - 15 + i] = getFmtBit(i);

  var canvas = document.createElement("canvas");
  canvas.width = px;
  canvas.height = px;
  var ctx = canvas.getContext("2d");
  var quiet = 4;
  var total = n + quiet * 2;
  var scale = Math.max(1, Math.floor(px / total));
  var drawW = scale * total;
  var padOffset = Math.floor((px - drawW) / 2);

  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, px, px);
  ctx.fillStyle = "#0b0f14";
  for (var r = 0; r < n; r++) {
    for (var c = 0; c < n; c++) {
      if (grid[r][c]) {
        ctx.fillRect(
          padOffset + (c + quiet) * scale,
          padOffset + (r + quiet) * scale,
          scale,
          scale
        );
      }
    }
  }
  return canvas;
}

      function renderRealQR(targetCanvas, text) {
        if (!targetCanvas) return;
        try {
          var built = fapMakeScannableQR(text || "", targetCanvas.width || 220);
          var ctx = targetCanvas.getContext("2d");
          targetCanvas.width = built.width;
          targetCanvas.height = built.height;
          ctx.drawImage(built, 0, 0);
        } catch (err) {
          var ctxErr = targetCanvas.getContext("2d");
          ctxErr.fillStyle = "#ffffff";
          ctxErr.fillRect(0, 0, targetCanvas.width, targetCanvas.height);
          ctxErr.fillStyle = "#0b0f14";
          ctxErr.font = "12px sans-serif";
          ctxErr.fillText("QR unavailable", 16, 28);
        }
      }

      function openQRModal(title, value) {
        var modal = document.getElementById("qrModal");
        var titleEl = document.getElementById("qrTitle");
        var subEl = document.getElementById("qrSubtitle");
        var canvas = document.getElementById("qrCanvas");
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
          openQRModal("Subscription QR", inp ? inp.value : "");
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
