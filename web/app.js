(function () {
  'use strict';

  var root = document.documentElement;
  var view = document.getElementById('view');
  var titleEl = document.getElementById('title');
  var topActions = document.getElementById('topActions');
  var modalRoot = document.getElementById('modalRoot');
  var toasts = document.getElementById('toasts');
  var titles = {
    dashboard: 'Dashboard',
    inbounds: 'Inbounds',
    clients: 'Clients',
    outbounds: 'Outbounds',
    routing: 'Routing Rules',
    quotas: 'Quota Groups',
    nodes: 'Nodes & API',
    logs: 'Logs & Xray Config',
    settings: 'Settings & System'
  };
  var state = {
    page: 'dashboard',
    inbounds: [],
    outbounds: [],
    rules: [],
    groups: [],
    nodes: [],
    tokens: [],
    ibQuery: '',
    clQuery: '',
    clInboundFilter: '',
    logType: 'error',
    timer: null
  };

  var theme = 'light';
  try {
    theme = localStorage.getItem('fap-theme') || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  } catch (e) {}
  root.dataset.theme = theme;

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  function toast(msg, isErr) {
    var el = document.createElement('div');
    el.className = 'toast' + (isErr ? ' err' : '');
    el.textContent = msg;
    toasts.appendChild(el);
    setTimeout(function () { el.remove(); }, 3400);
  }

  function api(method, path, body) {
    var opts = { method: method, credentials: 'same-origin', headers: {} };
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    return fetch(basePrefix() + '/api/' + String(path).replace(/^\/+/, ''), opts).then(function (res) {
      if (res.status === 401) {
        location.href = basePrefix() + '/login';
        throw new Error('unauthorized');
      }
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) throw new Error(data.error || 'Unknown error');
        return data;
      });
    });
  }

  function fail(e) {
    if (e && e.message !== 'unauthorized') toast(e.message, true);
  }

  function fmtBytes(n) {
    if (!n || n <= 0) return '0 B';
    var u = ['B', 'KB', 'MB', 'GB', 'TB'];
    var i = 0;
    while (n >= 1024 && i < 4) { n /= 1024; i++; }
    return n.toFixed(i === 0 || n >= 100 ? 0 : 1) + ' ' + u[i];
  }

  function fmtUptime(s) {
    var d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    return (d ? d + 'd ' : '') + (h ? h + 'h ' : '') + m + 'm';
  }

  function expiryInfo(c) {
    var ms = c.expiry_at || 0;
    if (!ms) return { text: 'Unlimited', cls: 'soft' };
    if (ms < 0) {
      var daysTotal = Math.round(Math.abs(ms) / 864e5);
      if (!c.first_seen_at) return { text: daysTotal + 'd (after first use)', cls: 'soft' };
      ms = c.first_seen_at + Math.abs(ms);
    }
    var left = ms - Date.now();
    if (left <= 0) return { text: 'Expired', cls: 'off' };
    var days = Math.ceil(left / 864e5);
    return { text: days + 'd left', cls: days <= 3 ? 'warn' : 'soft' };
  }

  function copy(text) {
    var done = function () { toast('Copied to clipboard'); };
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(done, function () { toast('Failed to copy', true); });
      return;
    }
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); done(); } catch (e) { toast('Failed to copy', true); }
    ta.remove();
  }

  function applyBars() {
    view.querySelectorAll('[data-pct]').forEach(function (el) {
      el.style.width = el.dataset.pct + '%';
    });
    view.querySelectorAll('.ring[data-ring]').forEach(function (el) {
      var pct = Math.max(0, Math.min(100, parseFloat(el.dataset.ring) || 0));
      var color = pct >= 85 ? 'var(--danger)' : pct >= 65 ? 'var(--warn)' : 'var(--accent)';
      el.style.setProperty('--ring-p', pct + '%');
      el.style.setProperty('--ring-c', color);
    });
  }

  function stopTimer() {
    if (state.timer) clearInterval(state.timer);
    state.timer = null;
  }

  var validPages = ['dashboard', 'inbounds', 'clients', 'outbounds', 'routing', 'quotas', 'nodes', 'logs', 'settings'];

  function basePrefix() {
    var p = window.location.pathname || '/';
    var idx = p.indexOf('/static/');
    if (idx !== -1) p = p.slice(0, idx);
    for (var i = 0; i < validPages.length; i++) {
      var seg = '/' + validPages[i];
      if (p === seg || p.endsWith(seg) || p.endsWith(seg + '/')) {
        p = p.slice(0, p.lastIndexOf(seg));
        break;
      }
    }
    p = p.replace(/\/+$/, '');
    return p || '';
  }

  function pageFromLocation() {
    var parts = (window.location.pathname || '/').split('/').filter(Boolean);
    if (parts.length > 0) {
      var last = parts[parts.length - 1].toLowerCase();
      if (validPages.indexOf(last) !== -1) return last;
    }
    return 'dashboard';
  }

  function syncUrlForPage(page) {
    try {
      var base = basePrefix();
      var target = page === 'dashboard' ? (base + '/') : (base + '/' + page);
      if (window.location.pathname !== target) {
        window.history.pushState({ fapPage: page }, '', target);
      }
    } catch (_) {}
  }

  function go(page) {
    if (validPages.indexOf(page) === -1) page = 'dashboard';
    stopTimer();
    state.page = page;
    syncUrlForPage(page);
    titleEl.textContent = titles[page] || page;
    topActions.innerHTML = '';
    document.querySelectorAll('#nav button').forEach(function (b) {
      b.classList.toggle('on', b.dataset.page === page);
    });
    if (page === 'dashboard') renderDashboard();
    if (page === 'inbounds') renderInbounds();
    if (page === 'clients') renderClients();
    if (page === 'outbounds') renderOutbounds();
    if (page === 'routing') renderRouting();
    if (page === 'quotas') renderQuotas();
    if (page === 'nodes') renderNodes();
    if (page === 'logs') renderLogs();
    if (page === 'settings') renderSettings();
  }

  function statCard(label, value, sub) {
    return '<div class="card stat"><small>' + esc(label) + '</small><b>' + value + '</b>' +
      (sub ? '<div class="sub">' + sub + '</div>' : '') + '</div>';
  }

  function gaugeCard(title, pct, mainText, subText) {
    var p = Math.round(pct || 0);
    return '<div class="card gauge-card">' +
      '<div class="ring" data-ring="' + p + '"><span>' + p + '%</span></div>' +
      '<div class="gauge-info"><small>' + esc(title) + '</small><b class="num">' + mainText + '</b><span>' + subText + '</span></div></div>';
  }

  
  var trafficHistory = [];
  function pushTrafficSample(upBps, downBps) {
    trafficHistory.push({
      up: Math.max(0, Number(upBps) || 0),
      down: Math.max(0, Number(downBps) || 0),
      t: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    });
    if (trafficHistory.length > 30) trafficHistory.shift();
  }

  function renderTrafficChartSvg() {
    if (!trafficHistory.length) {
      return '<div class="muted" style="padding:24px 0;text-align:center">Collecting live traffic samples...</div>';
    }
    var w = 640, h = 150, padL = 8, padR = 8, padT = 12, padB = 22;
    var plotW = w - padL - padR;
    var plotH = h - padT - padB;
    var maxVal = 1;
    trafficHistory.forEach(function (pt) {
      if (pt.up > maxVal) maxVal = pt.up;
      if (pt.down > maxVal) maxVal = pt.down;
    });
    var n = trafficHistory.length;
    var stepX = n > 1 ? plotW / (n - 1) : 0;

    function ptsFor(key) {
      return trafficHistory.map(function (pt, idx) {
        var x = padL + idx * stepX;
        var y = padT + plotH - (pt[key] / maxVal) * plotH;
        return x.toFixed(1) + ',' + y.toFixed(1);
      });
    }

    var upLine = ptsFor('up').join(' ');
    var downLine = ptsFor('down').join(' ');
    var downArea = downLine + ' ' + (padL + (n - 1) * stepX).toFixed(1) + ',' + (padT + plotH) + ' ' + padL + ',' + (padT + plotH);
    var upArea = upLine + ' ' + (padL + (n - 1) * stepX).toFixed(1) + ',' + (padT + plotH) + ' ' + padL + ',' + (padT + plotH);
    var firstLabel = trafficHistory[0].t;
    var lastLabel = trafficHistory[n - 1].t;

    return '<svg viewBox="0 0 ' + w + ' ' + h + '" class="traffic-svg" preserveAspectRatio="none">' +
      '<defs>' +
      '<linearGradient id="fapDownGrad" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#22c55e" stop-opacity="0.32"/><stop offset="100%" stop-color="#22c55e" stop-opacity="0.01"/></linearGradient>' +
      '<linearGradient id="fapUpGrad" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#38bdf8" stop-opacity="0.26"/><stop offset="100%" stop-color="#38bdf8" stop-opacity="0.01"/></linearGradient>' +
      '</defs>' +
      '<line x1="' + padL + '" y1="' + (padT + plotH * 0.5).toFixed(1) + '" x2="' + (w - padR) + '" y2="' + (padT + plotH * 0.5).toFixed(1) + '" class="traffic-grid"/>' +
      '<line x1="' + padL + '" y1="' + (padT + plotH).toFixed(1) + '" x2="' + (w - padR) + '" y2="' + (padT + plotH).toFixed(1) + '" class="traffic-grid"/>' +
      '<polygon points="' + downArea + '" fill="url(#fapDownGrad)"/>' +
      '<polygon points="' + upArea + '" fill="url(#fapUpGrad)"/>' +
      '<polyline fill="none" stroke="#22c55e" stroke-width="2.2" points="' + downLine + '"/>' +
      '<polyline fill="none" stroke="#38bdf8" stroke-width="2" points="' + upLine + '"/>' +
      '<text x="' + padL + '" y="' + (h - 5) + '" class="traffic-axis">' + esc(firstLabel) + '</text>' +
      '<text x="' + (w / 2).toFixed(0) + '" y="' + (h - 5) + '" text-anchor="middle" class="traffic-axis">Peak: ' + esc(fmtBytes(maxVal)) + '/s</text>' +
      '<text x="' + (w - padR) + '" y="' + (h - 5) + '" text-anchor="end" class="traffic-axis">' + esc(lastLabel) + '</text>' +
      '</svg>';
  }

  function renderDashboard() {
    topActions.innerHTML = '<div class="top-tools">' +
      '<button type="button" class="btn sec sm" id="dashRefreshBtn">Refresh Now</button>' +
      '<button type="button" class="btn sm" id="dashRestartXrayBtn">Restart Xray</button>' +
      '</div>';

    var rBtn = document.getElementById('dashRestartXrayBtn');
    if (rBtn) {
      rBtn.onclick = function () {
        rBtn.disabled = true;
        api('POST', 'xray/restart').then(function () {
          toast('Xray restarted');
          load();
        }).catch(fail).finally(function () {
          rBtn.disabled = false;
        });
      };
    }

    function load() {
      api('GET', 'status').then(function (s) {
        var sys = s.sys || {};
        var memPct = sys.mem_total ? (sys.mem_used * 100 / sys.mem_total) : 0;
        var diskPct = sys.disk_total ? (sys.disk_used * 100 / sys.disk_total) : 0;
        var netSpd = '↑ ' + fmtBytes(sys.net_up_speed || 0) + '/s · ↓ ' + fmtBytes(sys.net_down_speed || 0) + '/s';
        var netTot = 'Total: ↑ ' + fmtBytes(sys.net_up || 0) + ' · ↓ ' + fmtBytes(sys.net_down || 0);
        pushTrafficSample(sys.net_up_speed || 0, sys.net_down_speed || 0);
        var coreBadge = s.xray_running
          ? '<span class="tag ok">Live · Running</span>'
          : '<span class="tag danger">Stopped</span>';

        view.innerHTML =
          '<div class="dash-banner">' +
          '<div class="dash-banner-left">' +
          '<div>' +
          '<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">' +
          '<div class="card traffic-chart-card" style="margin-top:14px">' +
          '<div class="traffic-head">' +
          '<div><strong>Live Bandwidth Throughput Chart</strong><div class="muted" style="font-size:12px">Samples: ' + trafficHistory.length + ' · Download & Upload</div></div>' +
          '<div class="traffic-legend num">' +
          '<span><i class="lg-dot down"></i>Download: ' + esc(fmtBytes(sys.net_down_speed || 0)) + '/s</span>' +
          '<span><i class="lg-dot up"></i>Upload: ' + esc(fmtBytes(sys.net_up_speed || 0)) + '/s</span>' +
          '</div>' +
          '</div>' +
          renderTrafficChartSvg() +
          '<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">' +
          '<span class="dot ' + (s.xray_running ? 'ok' : '') + '"></span>' +
          '<strong style="font-size:15.5px">Xray Core Command Center</strong>' +
          coreBadge +
          '</div>' +
          '<div class="dash-banner-meta" style="margin-top:6px">' +
          '<span class="dash-pill num">Core: ' + esc(s.xray_version || '-') + '</span>' +
          '<span class="dash-pill num">CPU: ' + ((sys.cpu || 0).toFixed(1)) + '%</span>' +
          '<span class="dash-pill num">RAM: ' + memPct.toFixed(1) + '%</span>' +
          '<span class="dash-pill num">Uptime: ' + fmtUptime(s.uptime) + '</span>' +
          '</div>' +
          '</div>' +
          '</div>' +
          '<div class="dash-quick">' +
          '<button type="button" class="btn sec sm" data-go="clients">Clients (' + s.clients + ')</button>' +
          '<button type="button" class="btn sec sm" data-go="inbounds">Inbounds (' + s.inbounds + ')</button>' +
          '<button type="button" class="btn sec sm" data-go="logs">Live Logs</button>' +
          '</div>' +
          '</div>' +
          '<div class="gauges">' +
          gaugeCard('CPU Usage', sys.cpu || 0, (sys.cpu || 0).toFixed(1) + '%', (sys.cpu_cores || 1) + ' Cores · Load: ' + esc(sys.loads || '-')) +
          gaugeCard('RAM Usage', memPct, fmtBytes(sys.mem_used || 0) + ' / ' + fmtBytes(sys.mem_total || 0), 'Panel RSS: ' + fmtBytes(s.panel_mem)) +
          gaugeCard('Disk Usage', diskPct, fmtBytes(sys.disk_used || 0) + ' / ' + fmtBytes(sys.disk_total || 0), esc((sys.os || '') + ' / ' + (sys.arch || ''))) +
          statCard('Server Network', '<span class="num" style="font-size:15px">' + netSpd + '</span>', '<span class="num">' + netTot + '</span>') +
          '</div>' +
          '<div class="cards">' +
          statCard('Xray Core Status', '<span class="dot ' + (s.xray_running ? 'ok' : '') + '"></span>' + (s.xray_running ? 'Running' : 'Stopped'), esc(s.xray_version || '') + (s.xray_error ? '<br><span style="color:#e5484d;font-size:12px;word-break:break-word">' + esc(String(s.xray_error).slice(0, 300)) + '</span>' : '')) +
          statCard('Inbounds / Outbounds', '<span class="num">' + s.inbounds + ' / ' + (s.outbounds || 0) + '</span>', 'Routing Rules: <span class="num">' + (s.routing_rules || 0) + '</span>') +
          statCard('Clients (Online / Active)', '<span class="num">' + (s.online || 0) + ' / ' + s.active + '</span>', 'Total Clients: <span class="num">' + s.clients + '</span>') +
          statCard('Total Client Traffic', '<span class="num">' + fmtBytes(s.up + s.down) + '</span>', '↑ ' + fmtBytes(s.up) + ' · ↓ ' + fmtBytes(s.down)) +
          statCard('Panel Uptime', fmtUptime(s.uptime), 'DB: ' + esc(s.db_type) + ' · v' + esc(s.version)) +
          '</div>';
        view.querySelectorAll('[data-go]').forEach(function (btn) {
          btn.addEventListener('click', function () {
            if (typeof go === 'function') go(btn.dataset.go);
          });
        });
        applyBars();
      }).catch(fail);
    }

    var refBtn = document.getElementById('dashRefreshBtn');
    if (refBtn) refBtn.onclick = load;

    load();
    state.timer = setInterval(load, 3000);
  }

  function groupName(gid) {
    if (!gid) return '';
    for (var i = 0; i < state.groups.length; i++) {
      if (state.groups[i].id === gid) return state.groups[i].name;
    }
    return '';
  }

  function renderInboundsTable(keepScroll) {
    var y = window.scrollY;
    var q = state.ibQuery.toLowerCase();
    var list = state.inbounds.filter(function (ib) {
      if (!q) return true;
      return (ib.remark || '').toLowerCase().indexOf(q) !== -1 ||
             (ib.tag || '').toLowerCase().indexOf(q) !== -1 ||
             (ib.protocol || '').toLowerCase().indexOf(q) !== -1 ||
             String(ib.port).indexOf(q) !== -1;
    });

    if (!list.length) {
      view.innerHTML = '<div class="card empty">No inbounds found. Click "+ New Inbound" to create one.</div>';
      return;
    }

    var html = '<div class="card" style="overflow-x:auto"><table><thead><tr>' +
      '<th>Status</th><th>Remark / Tag</th><th>Protocol</th><th>Transport / Security</th><th>Listen : Port</th><th>Clients</th><th>Traffic</th><th>Actions</th>' +
      '</tr></thead><tbody>';

    list.forEach(function (ib) {
      var o = ib.options || {};
      var clients = ib.clients || [];
      var activeCount = clients.filter(function (c) { return c.enable; }).length;
      var fbCount = (o.fallbacks || []).length;
      html += '<tr>' +
        '<td><button class="switch sm ' + (ib.enable ? 'on' : '') + '" data-act="toggle" data-id="' + esc(ib.id) + '" aria-label="Toggle"></button></td>' +
        '<td><b>' + esc(ib.remark || ib.tag) + '</b><span class="comment-sub">' + esc(ib.tag) + '</span></td>' +
        '<td><span class="tag">' + esc(ib.protocol.toUpperCase()) + '</span></td>' +
        '<td><span class="tag soft">' + esc((o.network || 'tcp').toUpperCase()) + '</span> ' +
        (o.security && o.security !== 'none' ? '<span class="tag ok">' + esc(o.security.toUpperCase()) + '</span> ' : '') +
        (fbCount ? '<span class="tag warn">' + fbCount + ' FB</span>' : '') + '</td>' +
        '<td class="num">' + esc(ib.listen || '0.0.0.0') + ':' + ib.port + '</td>' +
        '<td class="num">' + activeCount + ' / ' + clients.length + '</td>' +
        '<td class="num">' + fmtBytes((ib.up || 0) + (ib.down || 0)) + '</td>' +
        '<td>'+ '</td>' +
        '<td><div class="row-actions">' +
        '<button class="icon-btn" data-act="add-client" data-id="' + esc(ib.id) + '">+ Client</button>' +
        '<button class="icon-btn" data-act="view-clients" data-id="' + esc(ib.id) + '">Clients</button>' +
        '<button class="icon-btn" data-act="edit-inbound" data-id="' + esc(ib.id) + '">Edit</button>' +
        '<button class="icon-btn" data-act="reset-inbound" data-id="' + esc(ib.id) + '">Reset</button>' +
        '<button class="icon-btn danger" data-act="del-inbound" data-id="' + esc(ib.id) + '">Delete</button>' +
        '</div></td></tr>';
    });

    html += '</tbody></table></div>';
    view.innerHTML = html;
    if (keepScroll) window.scrollTo(0, y);
  }

  function renderInbounds(keepScroll) {
    topActions.innerHTML = '<div class="top-tools">' +
      '<input class="search-box" id="ibSearch" type="search" placeholder="Search inbound or port..." value="' + esc(state.ibQuery) + '">' +
      '<button class="btn sec" data-act="reset-all">Reset All Traffic</button>' +
      '<button class="btn" data-act="new-inbound">+ New Inbound</button></div>';
    document.getElementById('ibSearch').addEventListener('input', function (e) {
      state.ibQuery = e.target.value.trim();
      renderInboundsTable(false);
    });
    api('GET', 'inbounds').then(function (list) {
      state.inbounds = list;
      renderInboundsTable(keepScroll);
    }).catch(fail);
  }

  function renderClientsTable(keepScroll) {
    var y = window.scrollY;
    var q = state.clQuery.toLowerCase();
    var ibFilter = state.clInboundFilter;
    var rows = [];

    state.inbounds.forEach(function (ib) {
      if (ibFilter && ib.id !== ibFilter) return;
      (ib.clients || []).forEach(function (c) {
        if (q) {
          var match = (c.email || '').toLowerCase().indexOf(q) !== -1 ||
                      (c.comment || '').toLowerCase().indexOf(q) !== -1 ||
                      (c.sub_id || '').toLowerCase().indexOf(q) !== -1 ||
                      (ib.remark || ib.tag || '').toLowerCase().indexOf(q) !== -1;
          if (!match) return;
        }
        rows.push({ ib: ib, c: c });
      });
    });

    if (!rows.length) {
      view.innerHTML = '<div class="card empty">No clients found. Click "+ New Client" to create one.</div>';
      return;
    }

    var html = '<div class="card" style="overflow-x:auto"><table><thead><tr>' +
      '<th>Status</th><th>Email / Name</th><th>Inbound</th><th>Traffic Usage</th><th>Speed</th><th>Expiry</th><th>Limits / Quota</th><th>Actions</th>' +'</tr></thead><tbody>';

    rows.forEach(function (item) {
      var ib = item.ib;
      var c = item.c;
      var upVal = c.up || 0;
      var downVal = c.down || 0;
      var used = upVal + downVal;
      var pct = c.total_bytes > 0 ? Math.min(100, Math.round(used * 100 / c.total_bytes)) : 0;
      var barCls = pct >= 100 ? 'full' : pct >= 85 ? 'high' : '';
      var liveSub = '<div class="sub num" data-cl-ud="' + esc(c.id || c.email || '') + '">&uarr; ' + fmtBytes(c.live_up || 0) + '/s &middot; &darr; ' + fmtBytes(c.live_down || 0) + '/s</div>';
      var usage = (c.total_bytes > 0
        ? '<span class="bar ' + barCls + '"><i data-pct="' + pct + '"></i></span><span class="num" data-cl-total="' + esc(c.id || c.email || '') + '">' + fmtBytes(used) + ' / ' + fmtBytes(c.total_bytes) + '</span>'
        : '<span class="num" data-cl-total="' + esc(c.id || c.email || '') + '">' + fmtBytes(used) + '</span> <span class="tag soft">Unlimited</span>') ;
      var ex = expiryInfo(c);
      var limits = [];
      if (c.limit_ip > 0) limits.push('IP: ' + c.limit_ip);
      if (c.reset > 0) limits.push('Reset: ' + c.reset + 'd');
      var gn = groupName(c.quota_group);
      if (gn) limits.push('Group: ' + gn);

      html += '<tr>' +
        '<td><button class="switch sm ' + (c.enable ? 'on' : '') + '" data-act="toggle-client" data-cid="' + esc(c.id) + '"></button></td>' +
        '<td><span class="dot ' + (c.online ? 'ok' : '') + '" title="' + (c.online ? 'Online' : 'Offline') + '"></span><b>' + esc(c.email) + '</b>' + (c.comment ? '<span class="comment-sub">' + esc(c.comment) + '</span>' : '') + '</td>' +
        '<td><span class="tag soft">' + esc(ib.remark || ib.tag) + ' (' + esc(ib.protocol.toUpperCase()) + ')</span></td>' +
        '<td data-label=\"Traffic Usage\">' + usage + '</td>' +
        renderClientSpeedCell(c) +
        '<td><span class="tag ' + ex.cls + '">' + ex.text + '</span></td>' +
        '<td>' + (limits.length ? '<span class="tag soft">' + esc(limits.join(' · ')) + '</span>' : '<span class="tag soft">-</span>') + '</td>' +
        '<td><div class="row-actions">' +
        '<button class="icon-btn" data-act="qr" data-cid="' + esc(c.id) + '" data-id="' + esc(ib.id) + '">QR</button>' +
        '<button class="icon-btn" data-act="copy" data-cid="' + esc(c.id) + '" data-id="' + esc(ib.id) + '">Link</button>' +
        (c.sub_url ? (resolveClientSubUrl(c) ? '<button class="icon-btn" data-act="copy-sub" data-cid="' + esc(c.id) + '" data-id="' + esc(ib.id) + '">Sub</button>' : '') : '') +
        '<button class="icon-btn" data-act="edit-client" data-cid="' + esc(c.id) + '" data-id="' + esc(ib.id) + '">Edit</button>' +
        '<button class="icon-btn" data-act="reset" data-cid="' + esc(c.id) + '">Reset</button>' +
        '<button class="icon-btn danger" data-act="del-client" data-cid="' + esc(c.id) + '">Delete</button>' +
        '</div></td></tr>';
    });

    html += '</tbody></table></div>';
    view.innerHTML = html;
    applyBars();
    if (keepScroll) window.scrollTo(0, y);
  }

  
    function renderClientSpeedCell(c) {
    c = c || {};
    var up = Number(c.live_up ?? c.up_speed ?? c.tx_speed ?? c.upload_speed ?? 0) || 0;
    var down = Number(c.live_down ?? c.down_speed ?? c.rx_speed ?? c.download_speed ?? 0) || 0;
    var upStr = (typeof fmtBytes === 'function' ? fmtBytes(up) : up + ' B') + '/s';
    var downStr = (typeof fmtBytes === 'function' ? fmtBytes(down) : down + ' B') + '/s';
    return '<td class="num" data-label="Speed" data-cl-ud="' + esc(c.id || c.email || '') + '">' +
      '<span class="client-speed"><span class="sp-up">&uarr; ' + esc(upStr) + '</span> &middot; <span class="sp-down">&darr; ' + esc(downStr) + '</span></span>' +
      '</td>';
  }

  function formatClientSpeed(c) {
    c = c || {};
    var up = Number(
      c.up_speed ?? c.tx_speed ?? c.upload_speed ?? c.net_up_speed ?? c.speed_up ?? 0
    ) || 0;
    var down = Number(
      c.down_speed ?? c.rx_speed ?? c.download_speed ?? c.net_down_speed ?? c.speed_down ?? 0
    ) || 0;
    var upStr = (typeof fmtBytes === 'function' ? fmtBytes(up) : up + ' B') + '/s';
    var downStr = (typeof fmtBytes === 'function' ? fmtBytes(down) : down + ' B') + '/s';
    return '<span class="client-speed num"><span class="sp-up">↑ ' + esc(upStr) + '</span> · <span class="sp-down">↓ ' + esc(downStr) + '</span></span>';
  }

  function renderClients(keepScroll) {
    Promise.all([api('GET', 'inbounds'), api('GET', 'quota-groups')]).then(function (res) {
      state.inbounds = res[0];
      state.groups = res[1];

      var ibOpts = '<option value="">All Inbounds</option>';
      state.inbounds.forEach(function (ib) {
        ibOpts += '<option value="' + esc(ib.id) + '"' + (state.clInboundFilter === ib.id ? ' selected' : '') + '>' + esc(ib.remark || ib.tag) + ' (' + ib.port + ')</option>';
      });

      topActions.innerHTML = '<div class="top-tools">' +
        '<select class="filter-select" id="clIbFilter">' + ibOpts + '</select>' +
        '<input class="search-box" id="clSearch" type="search" placeholder="Search email, comment, sub..." value="' + esc(state.clQuery) + '">' +
        '<button class="btn" data-act="new-client-global">+ New Client</button></div>';

      document.getElementById('clIbFilter').addEventListener('change', function (e) {
        state.clInboundFilter = e.target.value;
        renderClientsTable(false);
      });
      document.getElementById('clSearch').addEventListener('input', function (e) {
        state.clQuery = e.target.value.trim();
        renderClientsTable(false);
      });

      renderClientsTable(keepScroll);
      if (window.__fapClientLiveTimer) clearInterval(window.__fapClientLiveTimer);
      window.__fapClientLiveTimer = setInterval(function () {
        if (document.hidden || state.page !== 'clients') return;
        api('GET', 'inbounds').then(function (latest) {
          if (!Array.isArray(latest) || state.page !== 'clients') return;
          state.inbounds = latest;
          var map = {};
          latest.forEach(function (ib) {
            (ib.clients || []).forEach(function (cl) {
              if (cl.id) map[String(cl.id)] = cl;
              if (cl.email) map[String(cl.email)] = cl;
            });
          });
          document.querySelectorAll('[data-cl-ud]').forEach(function (el) {
            var key = el.getAttribute('data-cl-ud');
            var cl = map[key];
            if (!cl) return;
            var u = cl.up || 0;
            var d = cl.down || 0;
            el.innerHTML = '&uarr; ' + fmtBytes(cl.live_up || 0) + '/s &middot; &darr; ' + fmtBytes(cl.live_down || 0) + '/s';
            var totEl = document.querySelector('[data-cl-total="' + CSS.escape(key) + '"]');
            if (totEl) {
              var sum = u + d;
              totEl.textContent = cl.total_bytes > 0 ? (fmtBytes(sum) + ' / ' + fmtBytes(cl.total_bytes)) : fmtBytes(sum);
            }
          });
        }).catch(function () {});
      }, 1000);
    }).catch(fail);
  }

  function findInbound(id) {
    return state.inbounds.filter(function (i) { return i.id === id; })[0];
  }

  function findClient(ib, cid) {
    return ib.clients.filter(function (c) { return c.id === cid; })[0];
  }

  function closeModal() {
    modalRoot.innerHTML = '';
  }

  function openModal(title, bodyHTML, onSubmit, hideSave) {
    modalRoot.innerHTML = '<div class="modal-back"><form class="modal"><h2>' + esc(title) + '</h2>' + bodyHTML +
      '<div class="form-error" id="mErr" role="alert"></div>' +
      '<div class="modal-actions">' +
      (hideSave ? '' : '<button class="btn" type="submit">Save</button>') +
      '<button class="btn sec" type="button" data-act="close">' + (hideSave ? 'Close' : 'Cancel') + '</button></div></form></div>';
    var form = modalRoot.querySelector('form');
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      if (onSubmit) onSubmit(form, function (msg) { document.getElementById('mErr').textContent = msg; });
    });
    var first = form.querySelector('input,select');
    if (first) first.focus();
    return form;
  }

  function opt(value, label, cur) {
    return '<option value="' + value + '"' + (value === cur ? ' selected' : '') + '>' + label + '</option>';
  }

  function inboundForm(ib) {
    var o = ib ? ib.options : {
      network: 'tcp',
      security: 'none',
      reality: { fingerprint: 'chrome' },
      ss_method: 'chacha20-ietf-poly1305',
      sniffing: { enabled: true },
      sockopt: {}
    };
    var r = o.reality || {};
    var sn = o.sniffing || { enabled: true };
    var so = o.sockopt || {};
    var proto = ib ? ib.protocol : 'vless';
    var fbText = (o.fallbacks || []).map(function (f) {
      return [f.dest, f.path || '', f.alpn || '', f.name || ''].join('|');
    }).join('\n');

    return '<div class="grid2">' +
      '<div class="sec-sep">Basic & Protocol Settings</div>' +
      '<label>Remark<input name="remark" value="' + esc(ib ? ib.remark : '') + '" required></label>' +
      '<label>Protocol<select name="protocol">' +
      opt('vless', 'VLESS', proto) +
      opt('vmess', 'VMess', proto) +
      opt('trojan', 'Trojan', proto) +
      opt('shadowsocks', 'Shadowsocks', proto) +
      opt('wireguard', 'WireGuard', proto) +
      opt('mtproto', 'MTProto', proto) +
      opt('hysteria2', 'Hysteria2', proto) +
      opt('socks', 'SOCKS5', proto) +
      opt('http', 'HTTP Proxy', proto) +
      opt('dokodemo-door', 'Dokodemo-door', proto) +
      '</select></label>' +
      '<label>Port<input class="ltr" name="port" type="number" min="1" max="65535" value="' + (ib ? ib.port : '') + '" required></label>' +
      '<label>Listen IP<input class="ltr" name="listen" placeholder="0.0.0.0" value="' + esc(ib ? ib.listen : '') + '"></label>' +
      '<label data-when="protocol=shadowsocks">Encryption Method<select name="ss_method">' +
      opt('chacha20-ietf-poly1305', 'chacha20-ietf-poly1305', o.ss_method) +
      opt('aes-128-gcm', 'aes-128-gcm', o.ss_method) +
      opt('aes-256-gcm', 'aes-256-gcm', o.ss_method) +
      opt('2022-blake3-aes-128-gcm', '2022-blake3-aes-128-gcm', o.ss_method) +
      opt('2022-blake3-aes-256-gcm', '2022-blake3-aes-256-gcm', o.ss_method) +
      '</select></label>' +
      '<label data-when="protocol=shadowsocks">Server Key (SS-2022)<input class="ltr" name="ss_password" value="' + esc(o.ss_password || '') + '"></label>' +
      '<label class="span2" data-when="protocol=wireguard">Server Private Key<input class="ltr" name="wg_priv" value="' + esc(o.wg_private_key || '') + '"></label>' +
      '<label class="span2" data-when="protocol=hysteria2">Auth Password<input class="ltr" name="hy_auth" value="' + esc(o.hysteria_auth || '') + '"></label>' +
      '<label class="span2" data-when="protocol=mtproto">Proxy Secret<input class="ltr" name="mt_secret" value="' + esc(o.mtproto_secret || '') + '"></label>' +
      '<label data-when="protocol=dokodemo-door">Target Address<input class="ltr" name="doko_addr" placeholder="127.0.0.1 or 1.1.1.1" value="' + esc(o.doko_address || '') + '"></label>' +
      '<label data-when="protocol=dokodemo-door">Target Port<input class="ltr" name="doko_port" type="number" placeholder="53" value="' + esc(o.doko_port || '') + '"></label>' +

      '<div class="sec-sep">Transport & Security</div>' +
      '<label>Network<select name="network">' +
      opt('tcp', 'TCP', o.network) +
      opt('ws', 'WebSocket', o.network) +
      opt('httpupgrade', 'HTTPUpgrade', o.network) +
      opt('xhttp', 'XHTTP', o.network) +
      opt('grpc', 'gRPC', o.network) +
      '</select></label>' +
      '<label>Security<select name="security">' + opt('none', 'None', o.security) + opt('tls', 'TLS', o.security) + opt('reality', 'Reality', o.security) + '</select></label>' +
      '<label data-when="network=ws,httpupgrade,xhttp">Host Header<input class="ltr" name="host" placeholder="cdn.example.com" value="' + esc(o.host || '') + '"></label>' +
      '<label data-when="network=ws,httpupgrade,xhttp,grpc">Path / Service Name<input class="ltr" name="path" placeholder="/" value="' + esc(o.path || '') + '"></label>' +
      '<label class="span2" data-when="network=xhttp">XHTTP Mode<select name="xhttp_mode">' +
      opt('auto', 'auto', o.xhttp_mode || 'auto') +
      opt('packet-up', 'packet-up', o.xhttp_mode) +
      opt('stream-up', 'stream-up', o.xhttp_mode) +
      '</select></label>' +

      '<label class="span2" data-when="security=tls">Certificate File Path<input class="ltr" name="cert_file" value="' + esc(o.cert_file || '') + '"></label>' +
      '<label class="span2" data-when="security=tls">Private Key File Path<input class="ltr" name="key_file" value="' + esc(o.key_file || '') + '"></label>' +

      '<label data-when="security=reality">Dest (Target)<input class="ltr" name="r_dest" placeholder="www.microsoft.com:443" value="' + esc(r.dest || '') + '"></label>' +
      '<label data-when="security=reality">Server Names (comma-separated)<input class="ltr" name="r_sni" placeholder="www.microsoft.com,yahoo.com" value="' + esc(r.server_name || '') + '"></label>' +
      '<label class="span2" data-when="security=reality">Private Key<input class="ltr" name="r_priv" value="' + esc(r.private_key || '') + '"></label>' +
      '<label class="span2" data-when="security=reality">Public Key<input class="ltr" name="r_pub" value="' + esc(r.public_key || '') + '"></label>' +
      '<label data-when="security=reality">Short IDs (comma-separated)<input class="ltr" name="r_sid" value="' + esc(r.short_id || '') + '"></label>' +
      '<label data-when="security=reality">Fingerprint<select name="r_fp">' +
      opt('chrome', 'chrome', r.fingerprint || 'chrome') +
      opt('firefox', 'firefox', r.fingerprint) +
      opt('safari', 'safari', r.fingerprint) +
      opt('ios', 'ios', r.fingerprint) +
      opt('android', 'android', r.fingerprint) +
      opt('random', 'random', r.fingerprint) +
      '</select></label>' +
      '<div class="span2" data-when="security=reality"><button class="btn sec sm" type="button" data-act="gen-keys">Generate Reality Keys</button></div>' +

      '<div class="sec-sep">Sniffing & Sockopt</div>' +
      '<label><input name="sniff_en" type="checkbox"' + (sn.enabled !== false ? ' checked' : '') + '> Enable Sniffing</label>' +
      '<label><input name="sniff_ro" type="checkbox"' + (sn.route_only ? ' checked' : '') + '> Route Only</label>' +
      '<label><input name="so_tfo" type="checkbox"' + (so.tcp_fast_open ? ' checked' : '') + '> TCP Fast Open</label>' +
      '<label><input name="so_pp" type="checkbox"' + (so.accept_proxy_protocol ? ' checked' : '') + '> Accept Proxy Protocol</label>' +
      '<label>TCP Congestion<select name="so_cong">' +
      opt('', 'Default', so.tcp_congestion || '') +
      opt('bbr', 'BBR', so.tcp_congestion) +
      opt('cubic', 'CUBIC', so.tcp_congestion) +
      '</select></label>' +
      '<label>External Proxy Host<input class="ltr" name="ext_proxy" placeholder="Optional" value="' + esc(o.external_proxy || '') + '"></label>' +

      '<div class="sec-sep" data-when="protocol=vless,trojan">Fallbacks</div>' +
      '<label class="span2" data-when="protocol=vless,trojan">One rule per line: dest|path|alpn|sni (e.g. 8080|/ws|h2|)<textarea class="ltr" name="fallbacks" rows="2" placeholder="8080|/ws||">' + esc(fbText) + '</textarea></label>' +
      '<label class="span2"><input name="enable" type="checkbox"' + (!ib || ib.enable ? ' checked' : '') + '> Enable Inbound</label>' +
      '</div>';
  }

  function syncInboundForm(form) {
    var f = form.elements;
    var p = f.protocol.value;
    if (p === 'wireguard' || p === 'mtproto' || p === 'socks' || p === 'http' || p === 'dokodemo-door') {
      f.security.value = 'none';
    }
    if (f.security.value === 'reality') {
      if (f.network.value !== 'tcp' && f.network.value !== 'grpc' && f.network.value !== 'xhttp') f.network.value = 'tcp';
      if (f.protocol.value !== 'vless' && f.protocol.value !== 'trojan') f.security.value = 'none';
    }
    if (f.protocol.value === 'shadowsocks') {
      f.security.value = 'none';
    }
    form.querySelectorAll('[data-when]').forEach(function (el) {
      var parts = el.dataset.when.split('=');
      el.hidden = parts[1].split(',').indexOf(f[parts[0]].value) === -1;
    });
  }

  function collectInbound(f) {
    var fallbacks = [];
    if (f.fallbacks && f.fallbacks.value.trim()) {
      f.fallbacks.value.trim().split('\n').forEach(function (line) {
        var p = line.trim().split('|');
        if (p[0]) {
          fallbacks.push({ dest: p[0].trim(), path: (p[1] || '').trim(), alpn: (p[2] || '').trim(), name: (p[3] || '').trim() });
        }
      });
    }
    return {
      remark: f.remark.value.trim(),
      protocol: f.protocol.value,
      doko_address: f.doko_addr ? f.doko_addr.value.trim() : '',
      doko_port: f.doko_port ? (parseInt(f.doko_port.value, 10) || 0) : 0,
      port: parseInt(f.port.value, 10),
      listen: f.listen.value.trim(),
      enable: f.enable.checked,
      options: {
        network: f.network.value,
        security: f.security.value,
        path: f.path.value.trim(),
        host: f.host.value.trim(),
        xhttp_mode: f.xhttp_mode ? f.xhttp_mode.value : '',
        cert_file: f.cert_file.value.trim(),
        key_file: f.key_file.value.trim(),
        ss_method: f.ss_method.value,
        ss_password: f.ss_password ? f.ss_password.value.trim() : '',
        wg_private_key: f.wg_priv ? f.wg_priv.value.trim() : '',
        hysteria_auth: f.hy_auth ? f.hy_auth.value.trim() : '',
        mtproto_secret: f.mt_secret ? f.mt_secret.value.trim() : '',
        external_proxy: f.ext_proxy ? f.ext_proxy.value.trim() : '',
        reality: {
          dest: f.r_dest.value.trim(),
          server_name: f.r_sni.value.trim(),
          private_key: f.r_priv.value.trim(),
          public_key: f.r_pub.value.trim(),
          short_id: f.r_sid.value.trim(),
          fingerprint: f.r_fp ? f.r_fp.value : 'chrome'
        },
        sniffing: {
          enabled: f.sniff_en ? f.sniff_en.checked : true,
          route_only: f.sniff_ro ? f.sniff_ro.checked : false,
          dest_override: ['http', 'tls', 'quic', 'fakedns']
        },
        sockopt: {
          tcp_fast_open: f.so_tfo ? f.so_tfo.checked : false,
          accept_proxy_protocol: f.so_pp ? f.so_pp.checked : false,
          tcp_congestion: f.so_cong ? f.so_cong.value : ''
        },
        fallbacks: fallbacks
      }
    };
  }

  function openInboundModal(ib) {
    var form = openModal(ib ? 'Edit Inbound' : 'New Inbound', inboundForm(ib), function (f, setErr) {
      var body = collectInbound(f.elements);
      var req = ib ? api('PUT', 'inbounds/' + ib.id, body) : api('POST', 'inbounds', body);
      req.then(function () {
        closeModal();
        toast('Inbound saved');
        renderInbounds(true);
      }).catch(function (e) { setErr(e.message); });
    });
    syncInboundForm(form);
    form.addEventListener('change', function () { syncInboundForm(form); });
  }

  function flowAllowed(ib) {
    if (!ib) return false;
    var o = ib.options || {};
    return ib.protocol === 'vless' && o.network === 'tcp' && o.security !== 'none';
  }

  function openClientModal(ib, c) {
    if (!ib && !state.inbounds.length) {
      toast('Please create an inbound first', true);
      return;
    }
    var targetIb = ib || state.inbounds[0];
    var gb = c && c.total_bytes ? (c.total_bytes / Math.pow(1024, 3)).toFixed(2).replace(/\.?0+$/, '') : '';
    var expDays = '';
    var startAfterFirst = false;
    if (c && c.expiry_at) {
      if (c.expiry_at < 0) {
        startAfterFirst = true;
        expDays = Math.round(Math.abs(c.expiry_at) / 864e5);
      } else {
        var rem = c.expiry_at - Date.now();
        expDays = rem > 0 ? Math.ceil(rem / 864e5) : 0;
      }
    }

    var ibSelectHTML = '';
    if (!c && !ib) {
      var ibOpts = '';
      state.inbounds.forEach(function (item) {
        ibOpts += '<option value="' + esc(item.id) + '">' + esc(item.remark || item.tag) + ' (' + esc(item.protocol.toUpperCase()) + ' :' + item.port + ')</option>';
      });
      ibSelectHTML = '<label class="span2">Target Inbound<select name="inbound_id">' + ibOpts + '</select></label>';
    }

    var groupOpts = '<option value="">None</option>';
    state.groups.forEach(function (g) {
      groupOpts += '<option value="' + esc(g.id) + '"' + (c && c.quota_group === g.id ? ' selected' : '') + '>' + esc(g.name) + ' (' + fmtBytes(g.total_bytes) + ')</option>';
    });

    var body = '<div class="grid2">' +
      ibSelectHTML +
      '<label>Email / Client Name<input class="ltr" name="email" value="' + esc(c ? c.email : '') + '" required></label>' +
      (!c ? '<label>Bulk Count<input class="ltr" name="bulk_count" type="number" min="1" max="200" value="1"></label>' :
        '<label>Subscription ID<input class="ltr" name="sub_id" value="' + esc(c.sub_id || '') + '"></label>') +
      '<label>Traffic Limit (GB)<input class="ltr" name="gb" type="number" min="0" step="0.1" placeholder="0 = Unlimited" value="' + gb + '"></label>' +
      '<label>Expiry Duration (Days)<input class="ltr" name="days" type="number" min="0" placeholder="0 = Unlimited" value="' + expDays + '"></label>' +
      '<label>IP Limit<input class="ltr" name="limit_ip" type="number" min="0" value="' + (c ? c.limit_ip : 0) + '"></label>' +
      '<label>Auto Reset Traffic (Days)<input class="ltr" name="reset" type="number" min="0" value="' + (c ? c.reset : 0) + '"></label>' +
      '<label>Shared Quota Group<select name="quota_group">' + groupOpts + '</select></label>' +
      '<label>Device Lock (HWID)<input class="ltr" name="hwid" value="' + esc(c ? c.hwid || '' : '') + '"></label>' +
      '<label class="span2" id="flowField" ' + (flowAllowed(targetIb) ? '' : 'hidden') + '>Flow<select name="flow">' + opt('', 'None', c ? c.flow : '') + opt('xtls-rprx-vision', 'xtls-rprx-vision', c ? c.flow : '') + '</select></label>' +
      '<label class="span2">Comment<input name="comment" value="' + esc(c ? c.comment : '') + '"></label>' +
      '<label class="span2"><input name="start_first" type="checkbox"' + (startAfterFirst ? ' checked' : '') + '> Start expiry timer on first connection</label>' +
      '<label class="span2"><input name="enable" type="checkbox"' + (!c || c.enable ? ' checked' : '') + '> Enable Client</label>' +
      '</div>';

    var form = openModal(c ? 'Edit Client' : 'New Client', body, function (f, setErr) {
      var el = f.elements;
      var chosenIbId = el.inbound_id ? el.inbound_id.value : targetIb.id;
      var total = el.gb.value ? Math.round(parseFloat(el.gb.value) * Math.pow(1024, 3)) : 0;
      var days = parseInt(el.days.value, 10) || 0;
      var exp = 0;
      if (days > 0) {
        var durMs = days * 86400 * 1000;
        exp = el.start_first.checked ? -durMs : (Date.now() + durMs);
      }
      var payload = {
        inbound_id: chosenIbId,
        email: el.email.value.trim(),
        sub_id: el.sub_id ? el.sub_id.value.trim() : '',
        bulk_count: el.bulk_count ? (parseInt(el.bulk_count.value, 10) || 1) : 1,
        total_bytes: total,
        expiry_at: exp,
        limit_ip: parseInt(el.limit_ip.value, 10) || 0,
        reset: parseInt(el.reset.value, 10) || 0,
        quota_group: el.quota_group.value,
        hwid: el.hwid.value.trim(),
        comment: el.comment.value.trim(),
        flow: el.flow && !document.getElementById('flowField').hidden ? el.flow.value : '',
        enable: el.enable.checked
      };
      var req = c ? api('PUT', 'clients/' + c.id, payload) : api('POST', 'clients', payload);
      req.then(function () {
        closeModal();
        toast('Client saved');
        if (state.page === 'clients') renderClients(true);
        else renderInbounds(true);
      }).catch(function (e) { setErr(e.message); });
    });

    if (form.elements.inbound_id) {
      form.elements.inbound_id.addEventListener('change', function (e) {
        var selIb = findInbound(e.target.value);
        document.getElementById('flowField').hidden = !flowAllowed(selIb);
      });
    }
  }

  state.outboundHealth = state.outboundHealth || {};

  function formatOutboundHealthBadge(oid) {
    var h = (state.outboundHealth || {})[oid];
    if (!h) return '<span class="ob-health-pill idle">Not tested</span>';
    if (h.testing) return '<span class="ob-health-pill testing">Testing...</span>';
    if (h.ok) {
      var cls = h.latency_ms <= 120 ? 'good' : (h.latency_ms <= 350 ? 'warn' : 'slow');
      var msText = h.latency_ms > 0 ? (h.latency_ms + ' ms') : (h.status || 'OK');
      return '<span class="ob-health-pill ' + cls + '" title="' + esc(h.status || 'Healthy') + '">' + esc(msText) + '</span>';
    }
    return '<span class="ob-health-pill bad" title="' + esc(h.error || h.status || 'Unreachable') + '">' + esc(h.status || 'Failed') + '</span>';
  }

  function runOutboundTest(oid) {
    if (!oid) return Promise.resolve();
    state.outboundHealth[oid] = { testing: true };
    var cell = document.querySelector('[data-ob-health="' + oid + '"]');
    if (cell) cell.innerHTML = formatOutboundHealthBadge(oid);

    return api('POST', 'outbounds/' + encodeURIComponent(oid) + '/test').then(function (res) {
      state.outboundHealth[oid] = res || { ok: false, status: 'Error' };
      var c = document.querySelector('[data-ob-health="' + oid + '"]');
      if (c) c.innerHTML = formatOutboundHealthBadge(oid);
      if (res && res.ok) {
        toast((res.tag || 'Outbound') + ': ' + (res.latency_ms > 0 ? res.latency_ms + ' ms' : res.status));
      } else {
        toast((res && res.tag ? res.tag + ': ' : '') + ((res && (res.error || res.status)) || 'Unreachable'), true);
      }
    }).catch(function (err) {
      state.outboundHealth[oid] = { ok: false, status: 'Error', error: err && err.message ? err.message : String(err) };
      var c = document.querySelector('[data-ob-health="' + oid + '"]');
      if (c) c.innerHTML = formatOutboundHealthBadge(oid);
      toast('Test failed: ' + (err && err.message ? err.message : err), true);
    });
  }

  function runAllOutboundsTest() {
    var list = state.outbounds || [];
    if (!list.length) {
      toast('No outbounds to test');
      return;
    }
    list.forEach(function (o) {
      var oid = o.id || o.tag || '';
      if (oid) {
        state.outboundHealth[oid] = { testing: true };
        var cell = document.querySelector('[data-ob-health="' + oid + '"]');
        if (cell) cell.innerHTML = formatOutboundHealthBadge(oid);
      }
    });
    api('POST', 'outbounds/test-all').then(function (results) {
      var okCount = 0;
      (results || []).forEach(function (r) {
        var key = r.id || r.tag || '';
        if (key) {
          state.outboundHealth[key] = r;
          var cell = document.querySelector('[data-ob-health="' + key + '"]');
          if (cell) cell.innerHTML = formatOutboundHealthBadge(key);
        }
        if (r.ok) okCount++;
      });
      toast('Tested ' + (results ? results.length : 0) + ' outbounds (' + okCount + ' healthy)');
    }).catch(function (err) {
      toast('Bulk test failed: ' + (err && err.message ? err.message : err), true);
    });
  }

  function renderOutbounds() {
    topActions.innerHTML =
      '<div class="top-tools">' +
        '<button class="btn" id="btn-test-all-ob">Test All</button>' +
        '<button class="btn primary" id="btn-new-ob">+ Add Outbound</button>' +
      '</div>';
    var btnNew = document.getElementById('btn-new-ob');
    if (btnNew) btnNew.onclick = function () { openOutboundModal(); };
    var btnTestAll = document.getElementById('btn-test-all-ob');
    if (btnTestAll) btnTestAll.onclick = function () { runAllOutboundsTest(); };

    view.innerHTML =
      '<div class="card">' +
        '<div class="card-head">' +
          '<div><h3>Outbounds</h3><div class="hint">Manage external proxy gateways (WARP, SOCKS5, HTTP, Freedom, Blackhole) and probe live latency.</div></div>' +
        '</div>' +
        '<div id="outbounds-table-box"><div class="hint">Loading outbounds...</div></div>' +
      '</div>';

    api('GET', 'outbounds').then(function (list) {
      state.outbounds = list || [];
      var box = document.getElementById('outbounds-table-box');
      if (!box) return;
      if (!state.outbounds.length) {
        box.innerHTML = '<div class="empty" style="padding:20px">No custom outbounds configured. Built-in <b>direct</b> and <b>blocked</b> tags are always available in Routing Rules.</div>';
        return;
      }
      var rows = state.outbounds.map(function (o) {
        var rawId = o.id || o.tag || '';
        var oid = esc(rawId);
        var tag = esc(o.tag || '-');
        var remark = o.remark ? '<div class="hint" style="font-size:12px">' + esc(o.remark) + '</div>' : '';
        var proto = esc((o.protocol || o.type || '-').toUpperCase());
        var endpoint = esc((o.address || o.server || '') + (o.port ? ':' + o.port : ''));
        return (
          '<tr>' +
            '<td><button class="switch sm ' + (o.enable !== false ? 'on' : '') + '" data-act="toggle-outbound" data-oid="' + oid + '"></button></td>' +
            '<td><span class="tag">' + tag + '</span>' + remark + '</td>' +
            '<td>' + proto + '</td>' +
            '<td class="ltr">' + (endpoint || '-') + '</td>' +
            '<td data-ob-health="' + oid + '">' + formatOutboundHealthBadge(rawId) + '</td>' +
            '<td style="text-align:right;white-space:nowrap">' +
              '<button class="icon-btn" data-act="test-outbound" data-oid="' + oid + '">Test</button> ' +
              '<button class="icon-btn" data-act="edit-outbound" data-oid="' + oid + '">Edit</button> ' +
              '<button class="icon-btn danger" data-act="del-outbound" data-oid="' + oid + '">Delete</button>' +
            '</td>' +
          '</tr>'
        );
      }).join('');

      box.innerHTML =
        '<div class="table-wrap"><table><thead><tr>' +
          '<th>Status</th><th>Tag</th><th>Protocol</th><th>Endpoint</th><th>Health / Latency</th><th style="text-align:right">Actions</th>' +
        '</tr></thead><tbody>' + rows + '</tbody></table></div>';

      box.onclick = function (ev) {
        var btn = ev.target && ev.target.closest ? ev.target.closest('[data-act]') : null;
        if (!btn) return;
        var act = btn.getAttribute('data-act');
        var oid = btn.getAttribute('data-oid') || '';
        if (act === 'test-outbound') {
          ev.preventDefault();
          ev.stopPropagation();
          runOutboundTest(oid);
        } else if (act === 'edit-outbound') {
          ev.preventDefault();
          ev.stopPropagation();
          var targetOb = findOutbound(oid);
          if (targetOb) openOutboundModal(targetOb);
        }
      };
    }).catch(function (err) {
      var box = document.getElementById('outbounds-table-box');
      if (box) box.innerHTML = '<div class="empty">Failed to load outbounds: ' + esc(err && err.message ? err.message : err) + '</div>';
    });
  }

function renderRouting() {
    topActions.innerHTML = '<div class="top-tools">' +
      '<button class="btn sec" data-act="update-geo">Update GeoIP / GeoSite</button>' +
            '<button class="btn" data-act="new-rule">+ New Rule</button></div>';

    Promise.all([api('GET', 'outbounds'), api('GET', 'routing'), api('GET', 'inbounds')]).then(function (res) {
      state.outbounds = res[0] || [];
      state.rules = res[1] || [];
      state.inbounds = res[2] || [];

      var ibFilterOpts = opt('', 'Any Inbound', '');
      state.inbounds.forEach(function (ib) {
        ibFilterOpts += opt(ib.tag, ib.tag + (ib.remark ? ' (' + ib.remark + ')' : ''), '');
      });

      var html = '<div class="card pad"><h3>One-Click Routing Presets</h3>' +
        '<div class="top-tools">' +
        '<button class="btn sec sm" data-act="preset" data-preset="block-ads">+ Block Ads & Trackers</button>' +
        '<button class="btn sec sm" data-act="preset" data-preset="block-porn">+ Block Adult Content</button>' +
        '<button class="btn sec sm" data-act="preset" data-preset="ir-direct">+ Direct IR Traffic</button>' +
        '<button class="btn sec sm" data-act="preset" data-preset="ai-outbound">+ Route AI Services (OpenAI / Claude)</button>' +
        '</div></div>';

      html += '<div class="card pad"><h3>Routing Rules</h3>';
      if (!state.rules.length) {
        html += '<div class="empty" style="padding:18px">No custom routing rules configured.</div>';
      } else {
        html += '<div style="overflow-x:auto"><table><thead><tr><th>Status</th><th>Rule Name</th><th>Inbound</th><th>Domains / IPs</th><th>Outbound</th><th>Priority</th><th>Actions</th></tr></thead><tbody>';
        state.rules.forEach(function (r, idx) {
          var conds = [].concat(r.domains || [], r.ips || []).join(', ') || '*';
          var ibs = (r.inbound_tags && r.inbound_tags.length) ? r.inbound_tags.join(', ') : 'All';
          html += '<tr>' +
            '<td><button class="switch sm ' + (r.enable ? 'on' : '') + '" data-act="toggle-rule" data-rid="' + esc(r.id) + '"></button></td>' +
            '<td><b>' + esc(r.remark || '-') + '</b></td>' +
            '<td class="ltr"><span class="tag soft">' + esc(ibs) + '</span></td>' +
            '<td class="ltr"><code>' + esc(conds) + '</code></td>' +
            '<td><span class="tag">' + esc(r.outbound_tag) + '</span></td>' +
            '<td class="num">' + r.priority + '</td>' +
            '<td style="white-space:nowrap">' +
            (idx > 0 ? '<button class="icon-btn" data-act="move-rule" data-dir="up" data-rid="' + esc(r.id) + '" title="Move Up">↑</button> ' : '') +
            (idx < state.rules.length - 1 ? '<button class="icon-btn" data-act="move-rule" data-dir="down" data-rid="' + esc(r.id) + '" title="Move Down">↓</button> ' : '') +
            '<button class="icon-btn" data-act="edit-rule" data-rid="' + esc(r.id) + '">Edit</button> ' +
            '<button class="icon-btn danger" data-act="del-rule" data-rid="' + esc(r.id) + '">Delete</button>' +
            '</td></tr>';
        });
        html += '</tbody></table></div>';
      }
      html += '</div>';

      html += '<div class="card pad"><h3>Routing Rule Simulator</h3>' +
        '<div class="grid2" style="margin-bottom:10px">' +
        '<label>Domain / IP / Host:Port<input class="ltr" id="rt-sim-target" placeholder="openai.com or 1.1.1.1 or google.com:443"></label>' +
        '<label>Destination Port (Optional)<input class="ltr" id="rt-sim-port" placeholder="443"></label>' +
        '<label>Inbound Tag (Optional)<select id="rt-sim-inbound">' + ibFilterOpts + '</select></label>' +
        '<div style="display:flex;align-items:flex-end"><button type="button" class="btn sec" data-act="test-rule">Test Routing Match</button></div>' +
        '</div>' +
        '<div id="rt-sim-result" class="hint">Enter a domain, IP, port, or inbound to preview which outbound will handle it.</div>' +
        '</div>';

      html += '<div class="card pad"><h3>Outbounds</h3>' +
        '<div style="overflow-x:auto"><table><thead><tr><th>Status</th><th>Tag</th><th>Remark</th><th>Protocol</th><th>Target</th><th>Actions</th></tr></thead><tbody>' +
        '<tr><td><span class="tag ok">Built-in</span></td><td class="ltr"><b>direct</b></td><td>Direct Connection</td><td>FREEDOM</td><td>-</td><td>-</td></tr>' +
        '<tr><td><span class="tag off">Built-in</span></td><td class="ltr"><b>blocked</b></td><td>Blackhole Block</td><td>BLACKHOLE</td><td>-</td><td>-</td></tr>';
      state.outbounds.forEach(function (o) {
        html += '<tr>' +
          '<td><button class="switch sm ' + (o.enable ? 'on' : '') + '" data-act="toggle-ob" data-oid="' + esc(o.id) + '"></button></td>' +
          '<td class="ltr"><b>' + esc(o.tag) + '</b></td>' +
          '<td>' + esc(o.remark || '-') + '</td>' +
          '<td><span class="tag soft">' + esc(o.protocol.toUpperCase()) + '</span></td>' +
          '<td class="ltr">' + (o.address ? esc(o.address + ':' + o.port) : '-') + '</td>' +
          '<td><button class="icon-btn danger" data-act="del-ob" data-oid="' + esc(o.id) + '">Delete</button></td></tr>';
      });
      html += '</tbody></table></div></div>';

      view.innerHTML = html;
    }).catch(fail);
  }

  function findOutbound(id) {
    return (state.outbounds || []).filter(function (o) { return o.id === id || o.tag === id; })[0];
  }

  function openOutboundModal(ob) {
    var o = ob || {};
    var isEdit = Boolean(o.id);
    var curProto = o.protocol || 'socks';
    var ext = {};
    if (o.settings) {
      if (typeof o.settings === 'string') {
        try { ext = JSON.parse(o.settings) || {}; } catch (_) { ext = {}; }
      } else if (typeof o.settings === 'object') {
        ext = o.settings;
      }
    }
    var body = '<div class="grid2">' +
      '<label>Unique Tag<input class="ltr" name="tag" placeholder="warp-out" value="' + esc(o.tag || '') + '" required></label>' +
      '<label>Remark<input name="remark" placeholder="Cloudflare WARP" value="' + esc(o.remark || '') + '"></label>' +
      '<label>Protocol<select name="protocol">' +
      opt('socks', 'SOCKS5', curProto) +
      opt('http', 'HTTP Proxy', curProto) +
      opt('vless', 'VLESS', curProto) +
      opt('vmess', 'VMess', curProto) +
      opt('trojan', 'Trojan', curProto) +
      opt('shadowsocks', 'Shadowsocks', curProto) +
      opt('wireguard', 'WireGuard', curProto) +
      opt('freedom', 'Freedom (Direct)', curProto) +
      opt('blackhole', 'Blackhole (Block)', curProto) +
      '</select></label>' +
      '<label>Server Address / Endpoint<input class="ltr" name="address" placeholder="127.0.0.1 or example.com" value="' + esc(o.address || '') + '"></label>' +
      '<label>Port<input class="ltr" name="port" type="number" placeholder="40000" value="' + esc(o.port || '') + '"></label>' +
      '<label>Username / UUID / Key<input class="ltr" name="username" placeholder="Leave empty for no-auth SOCKS, or paste exact UUID" value="' + esc(o.username || '') + '"></label>' +
      '<label>Password / Method / PeerKey<input class="ltr" name="password" placeholder="Optional / Password / PublicKey" value="' + esc(o.password || '') + '"></label>' +
      '<label>Security / TLS<select name="ob_sec">' +
      opt('none', 'None', ext.security || 'none') +
      opt('tls', 'TLS', ext.security || '') +
      opt('reality', 'Reality', ext.security || '') +
      '</select></label>' +
      '<label>SNI / ServerName / Flow<input class="ltr" name="ob_sni" placeholder="example.com or xtls-rprx-vision" value="' + esc(ext.sni || ext.flow || '') + '"></label>' +
    '</div>';
    openModal(isEdit ? 'Edit Outbound' : 'New Outbound', body, function (form, setErr) {
      var f = form.elements;
      var protoVal = f.protocol.value;
      var secVal = f.ob_sec ? f.ob_sec.value : 'none';
      var sniOrFlow = f.ob_sni ? f.ob_sni.value.trim() : '';
      var settingsObj = { security: secVal };
      if (sniOrFlow) {
        if (sniOrFlow.indexOf('xtls-') === 0) settingsObj.flow = sniOrFlow;
        else settingsObj.sni = sniOrFlow;
      }
      var payload = {
        tag: f.tag.value.trim(),
        remark: f.remark.value.trim(),
        protocol: protoVal,
        address: f.address.value.trim(),
        port: parseInt(f.port.value, 10) || 0,
        username: f.username.value.trim(),
        password: f.password.value.trim(),
        settings: settingsObj
      };
      var req = isEdit
        ? api('PUT', 'outbounds/' + encodeURIComponent(o.id), payload)
        : api('POST', 'outbounds', payload);
      req.then(function () {
        closeModal();
        toast(isEdit ? 'Outbound updated' : 'Outbound created');
        if (state.page === 'outbounds' && typeof renderOutbounds === 'function') renderOutbounds();
        else if (typeof renderRouting === 'function') renderRouting();
      }).catch(function (e) { setErr(e.message); });
    });
  }

  function openRuleModal(existingRule) {
    var r = existingRule || {};
    var isEdit = Boolean(r.id);
    var obOpts = '';
    var seenOb = {};
    (state.outbounds || []).forEach(function (o) {
      seenOb[o.tag] = true;
      obOpts += opt(o.tag, o.tag + (o.remark ? ' (' + o.remark + ')' : ''), r.outbound_tag || 'direct');
    });
    if (!seenOb['direct']) obOpts = opt('direct', 'direct', r.outbound_tag || 'direct') + obOpts;
    if (!seenOb['blocked']) obOpts += opt('blocked', 'blocked', r.outbound_tag || '');

    var currentFirstIb = (r.inbound_tags && r.inbound_tags.length) ? r.inbound_tags[0] : '';
    var ibOpts = opt('', 'All Inbounds (Any)', currentFirstIb);
    (state.inbounds || []).forEach(function (ib) {
      var label = ib.tag + (ib.remark ? ' (' + ib.remark + ')' : '') + (ib.port ? ' :' + ib.port : '');
      ibOpts += opt(ib.tag, label, currentFirstIb);
    });

    var extraIbsInitial = (r.inbound_tags && r.inbound_tags.length > 1) ? r.inbound_tags.slice(1).join(', ') : '';

    var body = '<div class="grid2">' +
      '<label>Rule Name<input name="remark" placeholder="AI Services" value="' + esc(r.remark || '') + '" required></label>' +
      '<label>Target Outbound<select name="outbound_tag">' + obOpts + '</select></label>' +
      '<label>Target Inbound<select name="target_inbound">' + ibOpts + '</select></label>' +
      '<label>Extra Inbound Tags (optional)<input class="ltr" name="inbound_tags" value="' + esc(extraIbsInitial) + '" placeholder="vless-in,vmess-in"></label>' +
      '<label class="span2">Domains (comma-separated)<input class="ltr" name="domains" value="' + esc((r.domains || []).join(', ')) + '" placeholder="geosite:openai,domain:google.com"></label>' +
      '<label class="span2">IPs / CIDRs (comma-separated)<input class="ltr" name="ips" value="' + esc((r.ips || []).join(', ')) + '" placeholder="geoip:ir,1.1.1.1/32"></label>' +
      '<label>Destination Port (Optional)<input class="ltr" name="port" value="' + esc(r.port || '') + '" placeholder="80,443"></label>' +
      '<label>Priority (lower = higher priority)<input class="ltr" name="priority" type="number" step="1" value="' + (typeof r.priority === 'number' ? r.priority : 50) + '"></label>' +
      '<label>Network<select name="network">' +
        opt('', 'Any (TCP + UDP)', r.network || '') +
        opt('tcp', 'TCP', r.network || '') +
        opt('udp', 'UDP', r.network || '') +
      '</select></label>' +
      '<label>Protocol (optional)<input class="ltr" name="protocol" value="' + esc((r.protocol || []).join(', ')) + '" placeholder="bittorrent,http,tls"></label>' +
      '</div>';

    openModal(isEdit ? 'Edit Routing Rule' : 'New Routing Rule', body, function (form, setErr) {
      var f = form.elements;
      var splitList = function (el) {
        if (!el || !el.value) return [];
        return el.value.split(',').map(function (s) { return s.trim(); }).filter(Boolean);
      };
      var remark = (f.remark.value || '').trim();
      var outboundTag = (f.outbound_tag.value || '').trim();
      var selectedIb = (f.target_inbound.value || '').trim();
      var extraIbs = splitList(f.inbound_tags);
      var inboundTags = [];
      if (selectedIb) inboundTags.push(selectedIb);
      extraIbs.forEach(function (t) {
        if (inboundTags.indexOf(t) === -1) inboundTags.push(t);
      });
      var domains = splitList(f.domains);
      var ips = splitList(f.ips);
      var port = (f.port.value || '').trim();
      var rawPrio = parseInt(f.priority.value, 10);
      var priority = isNaN(rawPrio) ? 50 : rawPrio;
      var network = (f.network.value || '').trim();
      var protocol = splitList(f.protocol);

      if (!remark) {
        setErr('Rule Name is required');
        return;
      }
      if (!outboundTag) {
        setErr('Target Outbound is required');
        return;
      }

      var payload = {
        remark: remark,
        outbound_tag: outboundTag,
        inbound_tags: inboundTags,
        domains: domains,
        ips: ips,
        port: port,
        network: network,
        protocol: protocol,
        priority: priority
      };

      var reqPromise = isEdit
        ? api('PUT', 'routing/' + encodeURIComponent(r.id), payload)
        : api('POST', 'routing', payload);

      reqPromise.then(function () {
        closeModal();
        toast(isEdit ? 'Routing rule updated' : 'Routing rule created');
        renderRouting();
      }).catch(function (e) { setErr(e.message); });
    });
  }

  function renderQuotas() {
    topActions.innerHTML = '<button class="btn" data-act="new-group">+ New Quota Group</button>';
    api('GET', 'quota-groups').then(function (list) {
      state.groups = list || [];
      if (!state.groups.length) {
        view.innerHTML = '<div class="card empty">No shared quota groups configured.</div>';
        return;
      }
      var html = '<div class="card" style="overflow-x:auto"><table><thead><tr><th>Group Name</th><th>Used Traffic</th><th>Total Limit</th><th>Members</th><th>Status</th><th>Actions</th></tr></thead><tbody>';
      state.groups.forEach(function (g) {
        var pct = g.total_bytes > 0 ? Math.min(100, Math.round(g.used_bytes * 100 / g.total_bytes)) : 0;
        var barCls = pct >= 100 ? 'full' : pct >= 85 ? 'high' : '';
        html += '<tr>' +
          '<td><b>' + esc(g.name) + '</b></td>' +
          '<td><span class="bar ' + barCls + '"><i data-pct="' + pct + '"></i></span><span class="num">' + fmtBytes(g.used_bytes) + '</span></td>' +
          '<td class="num">' + (g.total_bytes > 0 ? fmtBytes(g.total_bytes) : 'Unlimited') + '</td>' +
          '<td class="num">' + g.members + '</td>' +
          '<td><span class="tag ' + (g.enable ? 'ok' : 'off') + '">' + (g.enable ? 'Active' : 'Disabled') + '</span></td>' +
          '<td><div class="row-actions">' +
          '<button class="icon-btn" data-act="edit-group" data-gid="' + esc(g.id) + '">Edit</button>' +
          '<button class="icon-btn danger" data-act="del-group" data-gid="' + esc(g.id) + '">Delete</button>' +
          '</div></td></tr>';
      });
      html += '</tbody></table></div>';
      view.innerHTML = html;
      applyBars();
    }).catch(fail);
  }

  function openGroupModal(grp) {
    var isEdit = !!grp;
    var curGB = isEdit && grp.total_bytes > 0
      ? (grp.total_bytes / Math.pow(1024, 3)).toFixed(2).replace(/\.?0+$/, '')
      : '50';
    var body = '<div class="grid2">' +
      '<label>Group Name<input name="name" value="' + esc(isEdit ? grp.name : '') + '" required></label>' +
      '<label>Total Quota (GB)<input class="ltr" name="gb" type="number" min="0" step="0.5" value="' + esc(curGB) + '" required></label>' +
      (isEdit ? '<label class="span2"><input name="enable" type="checkbox"' + (grp.enable ? ' checked' : '') + '> Active Group</label>' : '') +
      '</div>';
    openModal(isEdit ? 'Edit Quota Group' : 'New Shared Quota Group', body, function (form, setErr) {
      var f = form.elements;
      var gbVal = parseFloat(f.gb.value) || 0;
      var total = Math.round(gbVal * Math.pow(1024, 3));
      var payload = {
        name: f.name.value.trim(),
        total_bytes: total
      };
      if (isEdit) payload.enable = !!(f.enable && f.enable.checked);
      var req = isEdit
        ? api('PUT', 'quota-groups/' + grp.id, payload)
        : api('POST', 'quota-groups', payload);
      req.then(function () {
        closeModal();
        toast(isEdit ? 'Quota group updated' : 'Quota group created');
        renderQuotas();
      }).catch(function (e) { setErr(e.message); });
    });
  }

  function renderNodes() {
    topActions.innerHTML = '<div class="top-tools">' +
      '<button class="btn sec" data-act="new-token">+ New API Token</button>' +
      '<button class="btn" data-act="new-node">+ Add Node</button></div>';

    Promise.all([api('GET', 'nodes'), api('GET', 'tokens')]).then(function (res) {
      state.nodes = res[0];
      state.tokens = res[1];

      var html = '<div class="card pad"><h3>Connected Nodes (Multi-Node)</h3>';
      if (!state.nodes.length) {
        html += '<div class="empty" style="padding:18px">No remote nodes registered.</div>';
      } else {
        html += '<div style="overflow-x:auto"><table><thead><tr><th>Node Name</th><th>Endpoint URL</th><th>Status</th><th>Actions</th></tr></thead><tbody>';
        state.nodes.forEach(function (n) {
          var cls = n.status === 'online' ? 'ok' : n.status === 'offline' ? 'off' : 'soft';
          html += '<tr><td><b>' + esc(n.name) + '</b></td><td class="ltr">' + esc(n.address) + '</td>' +
            '<td><span class="tag ' + cls + '">' + esc(n.status) + '</span></td>' +
            '<td><div class="row-actions">' +
            '<button class="icon-btn" data-act="ping-node" data-nid="' + esc(n.id) + '">Ping</button>' +
            '<button class="icon-btn danger" data-act="del-node" data-nid="' + esc(n.id) + '">Delete</button>' +
            '</div></td></tr>';
        });
        html += '</tbody></table></div>';
      }
      html += '</div>';

      html += '<div class="card pad"><h3>REST API Tokens (Telegram Bots & Nodes)</h3>';
      if (!state.tokens.length) {
        html += '<div class="empty" style="padding:18px">No API tokens generated.</div>';
      } else {
        html += '<div style="overflow-x:auto"><table><thead><tr><th>Name</th><th>Scope</th><th>Token</th><th>Actions</th></tr></thead><tbody>';
        state.tokens.forEach(function (t) {
          html += '<tr><td><b>' + esc(t.name) + '</b></td><td><span class="tag soft">' + esc(t.scope) + '</span></td>' +
            '<td class="ltr"><code>' + esc(t.token.slice(0, 14)) + '...</code></td>' +
            '<td><div class="row-actions">' +
            '<button class="icon-btn" data-act="copy-tok" data-tok="' + esc(t.token) + '">Copy Token</button>' +
            '<button class="icon-btn danger" data-act="del-tok" data-tid="' + esc(t.id) + '">Revoke</button>' +
            '</div></td></tr>';
        });
        html += '</tbody></table></div>';
      }
      html += '</div>';

      view.innerHTML = html;
    }).catch(fail);
  }

  function openNodeModal() {
    var body = '<div class="grid2">' +
      '<label>Node Name<input name="name" placeholder="Germany-1" required></label>' +
      '<label>Panel API URL<input class="ltr" name="address" placeholder="https://de1.example.com:2053" required></label>' +
      '<label class="span2">Node API Token<input class="ltr" name="token" placeholder="fap_..." required></label>' +
      '</div>';
    openModal('Add Remote Node', body, function (form, setErr) {
      var f = form.elements;
      api('POST', 'nodes', {
        name: f.name.value.trim(),
        address: f.address.value.trim(),
        token: f.token.value.trim()
      }).then(function () {
        closeModal();
        toast('Node added');
        renderNodes();
      }).catch(function (e) { setErr(e.message); });
    });
  }

  function openTokenModal() {
    var body = '<div class="grid2">' +
      '<label>Token Name<input name="name" placeholder="Telegram Bot" required></label>' +
      '<label>Permission Scope<select name="scope">' +
      opt('*', 'Full Access (*)', '*') +
      opt('clients:write', 'Manage Clients (clients:write)', '') +
      opt('inbounds:read', 'Read Inbounds (inbounds:read)', '') +
      opt('system:read', 'Read System Status (system:read)', '') +
      '</select></label>' +
      '</div>';
    openModal('Generate API Token', body, function (form, setErr) {
      var f = form.elements;
      api('POST', 'tokens', { name: f.name.value.trim(), scope: f.scope.value }).then(function (res) {
        closeModal();
        copy(res.token);
        toast('Token created and copied');
        renderNodes();
      }).catch(function (e) { setErr(e.message); });
    });
  }

  function renderLogs() {
    if (!state.logLevel) state.logLevel = 'ALL';
    if (typeof state.logQuery !== 'string') state.logQuery = '';

    topActions.innerHTML = '<div class="top-tools">' +
      '<button class="btn sec" data-act="log-err">Error Log</button>' +
      '<button class="btn sec" data-act="log-acc">Access Log</button>' +
      '<button class="btn" data-act="show-xcfg">Inspect Xray JSON</button></div>';

    view.innerHTML =
      '<div class="card pad">' +
      '<div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:10px;margin-bottom:12px">' +
      '<h3 style="margin:0">Live Xray Log (<span id="logTypeLabel">' + esc((state.logType || 'error').toUpperCase()) + '</span>)</h3>' +
      '<div id="logStats" class="muted" style="font-size:12px">Loading...</div>' +
      '</div>' +
      '<div style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px;align-items:center">' +
      '<input type="search" id="logSearchInput" class="ltr" placeholder="Search IP, email, inbound, error..." value="' + esc(state.logQuery) + '" style="flex:1;min-width:220px">' +
      '<button type="button" class="btn sm ' + (state.logLevel === 'ALL' ? '' : 'sec') + '" data-lvl="ALL">All</button>' +
      '<button type="button" class="btn sm ' + (state.logLevel === 'ERROR' ? '' : 'sec') + '" data-lvl="ERROR">Error</button>' +
      '<button type="button" class="btn sm ' + (state.logLevel === 'WARNING' ? '' : 'sec') + '" data-lvl="WARNING">Warning</button>' +
      '<button type="button" class="btn sm ' + (state.logLevel === 'INFO' ? '' : 'sec') + '" data-lvl="INFO">Info</button>' +
      '<button type="button" class="btn sec sm" id="logClearBtn">Clear</button>' +
      '<button type="button" class="btn sec sm" id="logCopyBtn">Copy</button>' +
      '</div>' +
      '<pre class="link-box" id="logOutputPre" style="max-height:460px;white-space:pre-wrap;font-size:12px"></pre>' +
      '</div>';

    var searchEl = document.getElementById('logSearchInput');
    var preEl = document.getElementById('logOutputPre');
    var statsEl = document.getElementById('logStats');
    var typeLbl = document.getElementById('logTypeLabel');

    function syncLevelButtons() {
      view.querySelectorAll('[data-lvl]').forEach(function (b) {
        b.className = 'btn sm ' + (b.dataset.lvl === state.logLevel ? '' : 'sec');
      });
    }

    function loadLog() {
      var q = encodeURIComponent(state.logQuery || '');
      var lvl = encodeURIComponent(state.logLevel || 'ALL');
      var t = encodeURIComponent(state.logType || 'error');
      api('GET', 'xray/logs?type=' + t + '&level=' + lvl + '&q=' + q).then(function (res) {
        if (typeLbl) typeLbl.textContent = (state.logType || 'error').toUpperCase();
        if (preEl) preEl.textContent = res.lines || 'No matching log lines.';
        if (statsEl) {
          var matched = (typeof res.matched === 'number') ? res.matched : 0;
          var total = (typeof res.total === 'number') ? res.total : 0;
          statsEl.textContent = 'Showing ' + matched + ' of ' + total + ' lines · Auto-refresh 4s';
        }
      }).catch(fail);
    }

    if (searchEl) {
      searchEl.addEventListener('input', function () {
        state.logQuery = searchEl.value || '';
        loadLog();
      });
    }

    view.querySelectorAll('[data-lvl]').forEach(function (b) {
      b.addEventListener('click', function () {
        state.logLevel = b.dataset.lvl || 'ALL';
        syncLevelButtons();
        loadLog();
      });
    });

    var clearBtn = document.getElementById('logClearBtn');
    if (clearBtn) {
      clearBtn.onclick = function () {
        state.logQuery = '';
        state.logLevel = 'ALL';
        if (searchEl) searchEl.value = '';
        syncLevelButtons();
        loadLog();
      };
    }

    var copyBtn = document.getElementById('logCopyBtn');
    if (copyBtn) {
      copyBtn.onclick = function () {
        copy(preEl ? preEl.textContent : '');
      };
    }

    loadLog();
    state.timer = setInterval(loadLog, 4000);
  }

  function openXrayConfigModal() {
    api('GET', 'xray/config').then(function (res) {
      var body = '<pre class="link-box" style="max-height:460px;white-space:pre;font-size:11.5px">' + esc(res.config || '') + '</pre>' +
        '<div style="margin-top:10px"><button type="button" class="btn sm" id="copyXcfgBtn">Copy Full JSON</button></div>';
      openModal('Generated Xray Core Configuration', body, null, true);
      document.getElementById('copyXcfgBtn').onclick = function () { copy(res.config || ''); };
    }).catch(fail);
  }

  function drawQR(canvas, text) {
    if (!canvas) return;
    var ctx = canvas.getContext('2d');
    var payload = String(text || '');
    if (!payload) {
      canvas.width = 220;
      canvas.height = 60;
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.fillStyle = '#111827';
      ctx.font = '13px sans-serif';
      ctx.fillText('Empty QR content', 16, 34);
      return;
    }

    // UTF-8 byte encode
    var dataBytes = [];
    for (var i = 0; i < payload.length; i++) {
      var c = payload.charCodeAt(i);
      if (c < 0x80) dataBytes.push(c);
      else if (c < 0x800) dataBytes.push(0xc0 | (c >> 6), 0x80 | (c & 0x3f));
      else dataBytes.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 0x3f), 0x80 | (c & 0x3f));
    }

    // GF(256) with primitive polynomial 0x11d
    var exp = new Array(256), log = new Array(256);
    var x = 1;
    for (var i = 0; i < 255; i++) {
      exp[i] = x;
      log[x] = i;
      x <<= 1;
      if (x & 0x100) x ^= 0x11d;
    }
    exp[255] = exp[0];

    function gfMul(a, b) {
      if (a === 0 || b === 0) return 0;
      return exp[(log[a] + log[b]) % 255];
    }
    function rsPoly(ecLen) {
      var poly = [1];
      for (var i = 0; i < ecLen; i++) {
        var next = new Array(poly.length + 1).fill(0);
        for (var j = 0; j < poly.length; j++) {
          next[j] ^= poly[j];
          next[j + 1] ^= gfMul(poly[j], exp[i]);
        }
        poly = next;
      }
      return poly;
    }
    function rsEncode(data, ecLen) {
      var gen = rsPoly(ecLen);
      var rem = new Array(ecLen).fill(0);
      for (var i = 0; i < data.length; i++) {
        var factor = data[i] ^ rem[0];
        rem.shift();
        rem.push(0);
        for (var j = 0; j < ecLen; j++) {
          rem[j] ^= gfMul(gen[j + 1], factor);
        }
      }
      return data.concat(rem);
    }

    // QR capacities for ECC Level M (bytes) across versions 1..10
    var capM = [0, 14, 26, 42, 62, 84, 106, 122, 152, 180, 213];
    var ecPerBlockM = [0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26];
    var blocksM = [0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5];

    var ver = 0;
    for (var v = 1; v <= 10; v++) {
      if (dataBytes.length <= capM[v]) { ver = v; break; }
    }
    if (ver === 0) ver = 10;

    var countBits = ver <= 9 ? 8 : 16;
    var totalDataCodewords = capM[ver];
    var ecLen = ecPerBlockM[ver];
    var numBlocks = blocksM[ver];

    // Bit stream: mode 0100 (byte) + length + payload + terminator + pad
    var bits = [];
    function pushBits(val, len) {
      for (var b = len - 1; b >= 0; b--) bits.push((val >> b) & 1);
    }
    pushBits(0x4, 4);
    pushBits(dataBytes.length, countBits);
    for (var i = 0; i < dataBytes.length && bits.length < totalDataCodewords * 8; i++) {
      pushBits(dataBytes[i], 8);
    }
    var termLen = Math.min(4, totalDataCodewords * 8 - bits.length);
    if (termLen > 0) pushBits(0, termLen);
    while (bits.length % 8 !== 0) bits.push(0);

    var cw = [];
    for (var i = 0; i < bits.length; i += 8) {
      var byteVal = 0;
      for (var k = 0; k < 8; k++) byteVal = (byteVal << 1) | bits[i + k];
      cw.push(byteVal);
    }
    var padBytes = [0xec, 0x11];
    var pIdx = 0;
    while (cw.length < totalDataCodewords) {
      cw.push(padBytes[pIdx % 2]);
      pIdx++;
    }

    // Split into blocks & interleave
    var shortLen = Math.floor(totalDataCodewords / numBlocks);
    var longCount = totalDataCodewords % numBlocks;
    var dBlocks = [], eBlocks = [], offset = 0;
    for (var b = 0; b < numBlocks; b++) {
      var len = shortLen + (b >= numBlocks - longCount ? 1 : 0);
      var slice = cw.slice(offset, offset + len);
      offset += len;
      dBlocks.push(slice);
      eBlocks.push(rsEncode(slice, ecLen));
    }
    var finalCodewords = [];
    var maxDataLen = shortLen + (longCount > 0 ? 1 : 0);
    for (var i = 0; i < maxDataLen; i++) {
      for (var b = 0; b < numBlocks; b++) {
        if (i < dBlocks[b].length) finalCodewords.push(dBlocks[b][i]);
      }
    }
    for (var i = 0; i < ecLen; i++) {
      for (var b = 0; b < numBlocks; b++) {
        finalCodewords.push(eBlocks[b][i]);
      }
    }

    var n = ver * 4 + 17;
    var mat = Array.from({ length: n }, function () { return new Array(n).fill(null); });
    var fixed = Array.from({ length: n }, function () { return new Array(n).fill(false); });

    function setCell(r, c, dark, isFixed) {
      if (r < 0 || c < 0 || r >= n || c >= n) return;
      mat[r][c] = dark;
      if (isFixed) fixed[r][c] = true;
    }

    function addFinder(r0, c0) {
      for (var r = -1; r <= 7; r++) {
        for (var c = -1; c <= 7; c++) {
          var rr = r0 + r, cc = c0 + c;
          if (rr < 0 || cc < 0 || rr >= n || cc >= n) continue;
          var dark = (r >= 0 && r <= 6 && (c === 0 || c === 6)) ||
                     (c >= 0 && c <= 6 && (r === 0 || r === 6)) ||
                     (r >= 2 && r <= 4 && c >= 2 && c <= 4);
          setCell(rr, cc, !!dark, true);
        }
      }
    }
    addFinder(0, 0);
    addFinder(0, n - 7);
    addFinder(n - 7, 0);

    // Timing patterns
    for (var i = 8; i < n - 8; i++) {
      setCell(6, i, i % 2 === 0, true);
      setCell(i, 6, i % 2 === 0, true);
    }

    // Alignment pattern for v >= 2
    if (ver >= 2) {
      var alignPos = ver === 2 ? [6, 18] : ver === 3 ? [6, 22] : ver === 4 ? [6, 26] : ver === 5 ? [6, 30] : ver === 6 ? [6, 34] : [6, 22, 38];
      for (var ri = 0; ri < alignPos.length; ri++) {
        for (var ci = 0; ci < alignPos.length; ci++) {
          var ar = alignPos[ri], ac = alignPos[ci];
          if (fixed[ar][ac]) continue;
          for (var dr = -2; dr <= 2; dr++) {
            for (var dc = -2; dc <= 2; dc++) {
              var mDist = Math.max(Math.abs(dr), Math.abs(dc));
              setCell(ar + dr, ac + dc, mDist !== 1, true);
            }
          }
        }
      }
    }

    // Reserve format info cells
    for (var i = 0; i < 9; i++) {
      if (i < n) { fixed[8][i] = true; fixed[i][8] = true; }
    }
    for (var i = 0; i < 8; i++) {
      fixed[8][n - 1 - i] = true;
      fixed[n - 1 - i][8] = true;
    }
    setCell(n - 8, 8, true, true);

    // Place data bits in zig-zag
    var bitStream = [];
    for (var i = 0; i < finalCodewords.length; i++) {
      for (var b = 7; b >= 0; b--) bitStream.push((finalCodewords[i] >> b) & 1);
    }
    var bIdx = 0;
    var upward = true;
    for (var col = n - 1; col >= 1; col -= 2) {
      if (col === 6) col = 5;
      for (var step = 0; step < n; step++) {
        var row = upward ? (n - 1 - step) : step;
        for (var dc = 0; dc < 2; dc++) {
          var c = col - dc;
          if (!fixed[row][c]) {
            var dBit = bIdx < bitStream.length ? bitStream[bIdx++] : 0;
            mat[row][c] = dBit === 1;
          }
        }
      }
      upward = !upward;
    }

    // Mask 0: (r + c) % 2 === 0
    for (var r = 0; r < n; r++) {
      for (var c = 0; c < n; c++) {
        if (!fixed[r][c] && ((r + c) % 2 === 0)) {
          mat[r][c] = !mat[r][c];
        }
      }
    }

    // Format info for ECC M (00) + Mask 0 (000) -> BCH(15,5) = 0x5412
    var fmt = 0x5412;
    for (var i = 0; i < 15; i++) {
      var dark = ((fmt >> (14 - i)) & 1) === 1;
      if (i < 6) setCell(i, 8, dark, true);
      else if (i < 8) setCell(i + 1, 8, dark, true);
      else setCell(n - 15 + i, 8, dark, true);

      if (i < 8) setCell(8, n - 1 - i, dark, true);
      else if (i === 8) setCell(8, 7, dark, true);
      else setCell(8, 14 - i, dark, true);
    }

    var quiet = 4;
    var cellPx = 6;
    canvas.width = (n + quiet * 2) * cellPx;
    canvas.height = canvas.width;
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#000000';
    for (var r = 0; r < n; r++) {
      for (var c = 0; c < n; c++) {
        if (mat[r][c]) {
          ctx.fillRect((c + quiet) * cellPx, (r + quiet) * cellPx, cellPx, cellPx);
        }
      }
    }
  }


  function resolveClientDirectLink(ib, cl) {
    if (!cl) return "";
    if (cl.link && String(cl.link).trim()) return String(cl.link).trim();
    if (!ib) return "";
    const host = (window.location && window.location.hostname) ? window.location.hostname : "127.0.0.1";
    const port = ib.port || 443;
    const proto = String(ib.protocol || "vless").toLowerCase();
    const id = cl.uuid || cl.id || "";
    const label = encodeURIComponent(cl.email || ib.remark || ib.tag || proto);
    if (!id) return "";
    if (proto === "trojan") {
      return "trojan://" + encodeURIComponent(id) + "@" + host + ":" + port + "?security=tls#" + label;
    }
    if (proto === "vmess") {
      const payload = { v: "2", ps: cl.email || ib.tag || "vmess", add: host, port: String(port), id: id, aid: "0", net: "tcp", type: "none", tls: "" };
      return "vmess://" + btoa(unescape(encodeURIComponent(JSON.stringify(payload))));
    }
    return "vless://" + encodeURIComponent(id) + "@" + host + ":" + port + "?encryption=none&security=none#" + label;
  }

  function resolveClientSubUrl(cl) {
    if (!cl) return "";
    if (cl.sub_url && String(cl.sub_url).trim()) return String(cl.sub_url).trim();
    const subId = String(cl.sub_id || cl.subId || "").trim();
    if (!subId) return "";
    const origin = window.location ? window.location.origin : "";
    const prefix = (typeof basePrefix === "function") ? basePrefix() : "";
    return origin + prefix + "/sub/" + encodeURIComponent(subId);
  }

  function openQRModal(c) {
    var currentLink = c.sub_url || c.link;
    var body = '<div class="qr-tabs">' +
      (c.sub_url ? '<button type="button" class="btn sm" id="tabSub">Subscription URL</button>' : '') +
      '<button type="button" class="btn sec sm" id="tabDirect">Direct Config Link</button>' +
      '</div>' +
      '<div class="qr-wrap"><canvas id="qrCanvas"></canvas></div>' +
      '<div class="link-box" id="qrLinkText">' + esc(currentLink) + '</div>' +
      '<div style="margin-top:10px"><button type="button" class="btn sm" id="qrCopyBtn">Copy Link</button></div>';

    openModal('Share: ' + c.email, body, null, true);
    var canvas = document.getElementById('qrCanvas');
    var linkBox = document.getElementById('qrLinkText');
    var setMode = function (url, isSub) {
      currentLink = url;
      linkBox.textContent = url;
      drawQR(canvas, url);
      var tSub = document.getElementById('tabSub');
      var tDir = document.getElementById('tabDirect');
      if (tSub) tSub.className = isSub ? 'btn sm' : 'btn sec sm';
      if (tDir) tDir.className = !isSub ? 'btn sm' : 'btn sec sm';
    };
    setMode(currentLink, !!c.sub_url);
    if (document.getElementById('tabSub')) {
      document.getElementById('tabSub').onclick = function () { setMode(c.sub_url, true); };
    }
    document.getElementById('tabDirect').onclick = function () { setMode(c.link, false); };
    document.getElementById('qrCopyBtn').onclick = function () { copy(currentLink); };
  }

  function renderSettings() {
    api('GET', 'settings').then(function (cfg) {
      view.innerHTML =
        '<div class="card pad"><h3>Domain, Xray Core & Security Settings</h3><form id="cfgForm" class="grid2">' +
        '<label>Public Domain or Server IP<input class="ltr" name="public_host" placeholder="sub.example.com" value="' + esc(cfg.public_host || '') + '"></label>' +
        '<label>Custom Subscription Base URL<input class="ltr" name="sub_base_url" placeholder="https://sub.example.com/sub" value="' + esc(cfg.sub_base_url || '') + '"></label>' +
        '<label>Domain Routing Strategy<select name="routing_strategy">' +
        opt('IPIfNonMatch', 'IPIfNonMatch (Recommended)', cfg.routing_strategy || 'IPIfNonMatch') +
        opt('AsIs', 'AsIs', cfg.routing_strategy) +
        opt('IPOnDemand', 'IPOnDemand', cfg.routing_strategy) +
        '</select></label>' +
        '<label>Xray Log Level<select name="xray_log_level">' +
        opt('warning', 'Warning', cfg.xray_log_level || 'warning') +
        opt('info', 'Info', cfg.xray_log_level) +
        opt('error', 'Error', cfg.xray_log_level) +
        opt('none', 'None', cfg.xray_log_level) +
        '</select></label>' +
        '<label class="span2"><input name="xray_access_log" type="checkbox"' + (cfg.xray_access_log === 'true' ? ' checked' : '') + '> Write Xray log files to disk (enables Live Log viewer)</label>' +
        '<label class="span2"><input name="fail2ban" type="checkbox"' + (cfg.fail2ban === 'true' ? ' checked' : '') + '> Enable Fail2ban brute-force IP protection</label>' +
        '<div class="span2"><button class="btn" type="submit">Save Settings</button></div></form></div>' +

        '<div class="card pad"><h3>Change Admin Password</h3><form id="pwForm" class="grid2">' +
        '<label class="span2">Current Password<input name="old" type="password" autocomplete="current-password" required></label>' +
        '<label>New Password (min 8 chars)<input name="n1" type="password" autocomplete="new-password" minlength="8" required></label>' +
        '<label>Confirm New Password<input name="n2" type="password" autocomplete="new-password" minlength="8" required></label>' +
        '<div class="form-error span2" id="pwErr" role="alert"></div>' +
        '<div class="span2"><button class="btn" type="submit">Update Password</button></div></form></div>' +

        '<div class="card pad"><h3>System & Backup</h3><div class="modal-actions" style="margin:0">' +
        '<button class="btn sec" data-act="restart-xray">Restart Xray Core</button>' +
        '<button class="btn sec" data-act="backup">Download Backup</button>' +
        '<button class="btn sec" data-act="restore-btn">Restore Backup</button>' +
        '<input type="file" id="restoreFile" accept=".json" hidden>' +
        '</div><p class="hint">Restoring a backup replaces all inbounds, clients, and routing rules while keeping your current admin login password.</p></div>';

      document.getElementById('cfgForm').addEventListener('submit', function (e) {
        e.preventDefault();
        var f = e.target.elements;
        api('POST', 'settings', {
          public_host: f.public_host.value.trim(),
          sub_base_url: f.sub_base_url.value.trim(),
          routing_strategy: f.routing_strategy.value,
          xray_log_level: f.xray_log_level.value,
          xray_access_log: f.xray_access_log.checked ? 'true' : 'false',
          fail2ban: f.fail2ban.checked ? 'true' : 'false'
        }).then(function () { toast('Settings saved'); }).catch(fail);
      });

      document.getElementById('pwForm').addEventListener('submit', function (e) {
        e.preventDefault();
        var f = e.target.elements;
        var err = document.getElementById('pwErr');
        if (f.n1.value !== f.n2.value) {
          err.textContent = 'Passwords do not match';
          return;
        }
        api('POST', 'password', { old: f.old.value, new: f.n1.value }).then(function () {
          toast('Password updated; please sign in again');
          setTimeout(function () { location.href = basePrefix() + '/login'; }, 900);
        }).catch(function (x) { err.textContent = x.message; });
      });

      document.getElementById('restoreFile').addEventListener('change', function (e) {
        var file = e.target.files[0];
        if (!file) return;
        var reader = new FileReader();
        reader.onload = function () {
          try {
            var data = JSON.parse(reader.result);
            api('POST', 'restore', data).then(function () {
              toast('Backup restored successfully');
            }).catch(fail);
          } catch (err) {
            toast('Invalid JSON file', true);
          }
        };
        reader.readAsText(file);
      });
    }).catch(fail);
  }

  document.addEventListener('click', function (e) {
    var el = e.target.closest('[data-act]');
    if (!el) return;
    var act = el.dataset.act;
    var ib = el.dataset.id ? findInbound(el.dataset.id) : null;
    var cl = ib && el.dataset.cid ? findClient(ib, el.dataset.cid) : null;

    if (act === 'close') return closeModal();
    if (act === 'new-inbound') return openInboundModal(null);
    if (act === 'new-client-global') return openClientModal(null, null);
    if (act === 'new-outbound') return openOutboundModal();
    if (act === 'new-rule') return openRuleModal();
    if (act === 'new-group') return openGroupModal();
    if (act === 'edit-rule') {
      var found = (state.rules || []).find(function (item) { return item.id === el.dataset.rid; });
      return openRuleModal(found || null);
    }
    if (act === 'edit-group') {
      var grp = (state.groups || []).filter(function (x) { return x.id === el.dataset.gid; })[0];
      if (grp) return openGroupModal(grp);
    }
    if (act === 'move-rule') {
      return api('POST', 'routing/' + encodeURIComponent(el.dataset.rid) + '/move', {
        direction: el.dataset.dir
      }).then(function () { renderRouting(); }).catch(fail);
    }
    if (act === 'test-rule') {
      var targetVal = (document.getElementById('rt-sim-target') || {}).value || '';
      var portVal = (document.getElementById('rt-sim-port') || {}).value || '';
      var ibVal = (document.getElementById('rt-sim-inbound') || {}).value || '';
      var resBox = document.getElementById('rt-sim-result');
      return api('POST', 'routing/test', {
        target: targetVal.trim(),
        port: portVal.trim(),
        inbound_tag: ibVal.trim()
      }).then(function (out) {
        if (!resBox) return;
        if (out.matched) {
          resBox.innerHTML = 'Matched Rule: <b>' + esc(out.rule_name || out.rule_id) + '</b> → Outbound: <span class="tag">' + esc(out.outbound_tag) + '</span> (' + esc(out.reason) + ')';
        } else {
          resBox.innerHTML = 'No rule matched → Fallback Outbound: <span class="tag soft">' + esc(out.outbound_tag) + '</span> (' + esc(out.reason) + ')';
        }
      }).catch(function (e) {
        if (resBox) resBox.textContent = 'Error: ' + e.message;
      });
    }
    if (act === 'new-node') return openNodeModal();
    if (act === 'new-token') return openTokenModal();
    if (act === 'show-xcfg') return openXrayConfigModal();
    if (act === 'log-err') { state.logType = 'error'; return renderLogs(); }
    if (act === 'log-acc') { state.logType = 'access'; return renderLogs(); }
    if (act === 'update-geo') {
      toast('Downloading GeoIP & GeoSite databases...');
      return api('POST', 'xray/geodata').then(function (r) {
        toast('GeoData updated in ' + r.dir);
      }).catch(fail);
    }
    if (act === 'preset') {
      return api('POST', 'routing/preset', { preset: el.dataset.preset }).then(function () {
        toast('Routing rule added');
        renderRouting();
      }).catch(fail);
    }
    if (act === 'toggle-rule') {
      return api('POST', 'routing/' + el.dataset.rid + '/toggle').then(function () { renderRouting(); }).catch(fail);
    }
    if (act === 'del-rule') {
      return api('DELETE', 'routing/' + el.dataset.rid).then(function () { toast('Rule deleted'); renderRouting(); }).catch(fail);
    }
    if (act === 'toggle-ob') {
      return api('POST', 'outbounds/' + el.dataset.oid + '/toggle').then(function () { if (typeof renderOutbounds === 'function' && document.getElementById('outbounds-table-box')) renderOutbounds(); else renderRouting(); }).catch(fail);
    }
    if (act === 'del-ob') {
      return api('DELETE', 'outbounds/' + el.dataset.oid).then(function () { toast('Outbound deleted'); if (typeof renderOutbounds === 'function' && document.getElementById('outbounds-table-box')) renderOutbounds(); else renderRouting(); }).catch(fail);
    }
    if (act === 'view-clients' && ib) {
      state.clInboundFilter = ib.id;
      return go('clients');
    }
    if (act === 'toggle' && ib) {
      return api('POST', 'inbounds/' + ib.id + '/toggle').then(function () { renderInbounds(true); }).catch(fail);
    }
    if (act === 'toggle-client') {
      return api('POST', 'clients/' + el.dataset.cid + '/toggle').then(function () { renderClients(true); }).catch(fail);
    }
    if (act === 'edit-inbound' && ib) return openInboundModal(ib);
    if (act === 'add-client' && ib) return openClientModal(ib, null);
    if (act === 'edit-client' && ib && cl) return openClientModal(ib, cl);
    if (act === 'qr' && cl) return openQRModal(cl);
    if (act === 'copy' && cl) return copy(resolveClientDirectLink(ib, cl));
    if (act === 'copy-sub' && cl) { const sUrl = resolveClientSubUrl(cl); if (sUrl) return copy(sUrl); }
    if (act === 'copy-tok') return copy(el.dataset.tok);
    if (act === 'reset-inbound' && ib) {
      if (!confirm('Reset traffic for all clients in this inbound?')) return;
      return api('POST', 'inbounds/' + ib.id + '/reset').then(function () { toast('Inbound traffic reset'); renderInbounds(true); }).catch(fail);
    }
    if (act === 'reset-all') {
      if (!confirm('Reset traffic for all inbounds and clients?')) return;
      return api('POST', 'inbounds/reset-all').then(function () { toast('All traffic reset'); renderInbounds(true); }).catch(fail);
    }
    if (act === 'del-inbound' && ib) {
      if (!confirm('Delete inbound "' + (ib.remark || ib.tag) + '" and all its clients?')) return;
      return api('DELETE', 'inbounds/' + ib.id).then(function () { toast('Inbound deleted'); renderInbounds(true); }).catch(fail);
    }
    if (act === 'del-client') {
      if (!confirm('Delete this client?')) return;
      return api('DELETE', 'clients/' + el.dataset.cid).then(function () { toast('Client deleted'); renderClients(true); }).catch(fail);
    }
    if (act === 'del-group') {
      if (!confirm('Delete this quota group?')) return;
      return api('DELETE', 'quota-groups/' + el.dataset.gid).then(function () { toast('Quota group deleted'); renderQuotas(); }).catch(fail);
    }
    if (act === 'ping-node') {
      return api('POST', 'nodes/' + el.dataset.nid + '/ping').then(function (r) {
        toast(r.status === 'online' ? 'Node is online' : 'Node unreachable', r.status !== 'online');
        renderNodes();
      }).catch(fail);
    }
    if (act === 'del-node') {
      if (!confirm('Delete this node?')) return;
      return api('DELETE', 'nodes/' + el.dataset.nid).then(function () { toast('Node deleted'); renderNodes(); }).catch(fail);
    }
    if (act === 'del-tok') {
      if (!confirm('Revoke this API token?')) return;
      return api('DELETE', 'tokens/' + el.dataset.tid).then(function () { toast('Token revoked'); renderNodes(); }).catch(fail);
    }
    if (act === 'reset') {
      return api('POST', 'clients/' + el.dataset.cid + '/reset').then(function () { toast('Client traffic reset'); renderClients(true); }).catch(fail);
    }
    if (act === 'gen-keys') {
      var form = el.closest('form');
      return api('GET', 'reality/keys').then(function (k) {
        form.elements.r_priv.value = k.private_key;
        form.elements.r_pub.value = k.public_key;
        form.elements.r_sid.value = k.short_id;
      }).catch(fail);
    }
    if (act === 'restart-xray') {
      return api('POST', 'xray/restart').then(function () { toast('Xray Core restarted'); }).catch(fail);
    }
    if (act === 'backup') {
      location.href = 'api/backup';
    }
    if (act === 'restore-btn') {
      document.getElementById('restoreFile').click();
    }
  });

  document.getElementById('nav').addEventListener('click', function (e) {
    var b = e.target.closest('button[data-page]');
    if (b) {
      if (b.dataset.page === 'clients') state.clInboundFilter = '';
      go(b.dataset.page);
    }
  });

  document.getElementById('themeBtn').addEventListener('click', function () {
    var next = root.dataset.theme === 'dark' ? 'light' : 'dark';
    root.dataset.theme = next;
    try { localStorage.setItem('fap-theme', next); } catch (e) {}
  });

  document.getElementById('logoutBtn').addEventListener('click', function () {
    api('POST', 'logout').then(function () { location.href = basePrefix() + '/login'; }).catch(fail);
  });

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeModal();
  });

  go('dashboard');
})();

  function renderOutboundsSection(targetEl) {
    if (!targetEl) return;
    var list = (state && state.outbounds) || [];
    var rows = list.map(function (o) {
      return '<tr>' +
        '<td><b>' + esc(o.tag || '') + '</b></td>' +
        '<td><span class="tag soft">' + esc((o.protocol || '').toUpperCase()) + '</span></td>' +
        '<td>' + esc((o.address || '') + (o.port ? ':' + o.port : '')) + '</td>' +
        '<td>' + esc(o.relay_tag || '-') + '</td>' +
        '<td>' + esc(o.remark || '') + '</td>' +
        '</tr>';
    }).join('');
    var section = document.createElement('div');
    section.className = 'card separated-outbounds-card';
    section.style.marginTop = '18px';
    section.innerHTML =
      '<div class="card-head" style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">' +
        '<div><h3 style="margin:0">Outbounds (Dedicated Section)</h3><small style="opacity:0.75">Manage outbound protocols, relay chains & transport streams independently</small></div>' +
        '<span class="tag soft">' + list.length + ' Outbounds</span>' +
      '</div>' +
      '<div class="table-wrap"><table><thead><tr>' +
        '<th>Tag</th><th>Protocol</th><th>Endpoint</th><th>Relay</th><th>Remark</th>' +
      '</tr></thead><tbody>' + (rows || '<tr><td colspan="5" style="text-align:center;opacity:0.7">No outbounds configured yet</td></tr>') + '</tbody></table></div>';
    targetEl.appendChild(section);
  }

var liveTrafficEnabled = true;
var liveTrafficTimer = null;
var prevClientStats = {}; // clientKey -> { up, down, ts, upRate, downRate }

function fmtRate(bytesPerSec) {
  if (!bytesPerSec || bytesPerSec <= 0) return '0 B/s';
  var units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  var idx = 0;
  var val = bytesPerSec;
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024;
    idx++;
  }
  return (val >= 10 ? val.toFixed(1) : val.toFixed(2)) + ' ' + units[idx];
}

function fmtBytesCompact(b) {
  b = Number(b || 0);
  if (b <= 0) return '0 B';
  var u = ['B', 'KB', 'MB', 'GB', 'TB'];
  var i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return (b >= 10 ? b.toFixed(1) : b.toFixed(2)) + ' ' + u[i];
}

function updateClientTrafficSnapshots(inbounds) {
  var now = Date.now();
  (inbounds || []).forEach(function (ib) {
    (ib.clients || []).forEach(function (cl) {
      var key = String(cl.id || cl.email || cl.uuid || '');
      if (!key) return;
      var up = Number(cl.up || cl.upload || 0);
      var down = Number(cl.down || cl.download || 0);
      var prev = prevClientStats[key];
      if (prev) {
        var dt = Math.max(0.25, (now - prev.ts) / 1000);
        var dUp = Math.max(0, up - prev.up);
        var dDown = Math.max(0, down - prev.down);
        prevClientStats[key] = {
          up: up,
          down: down,
          ts: now,
          upRate: dUp / dt,
          downRate: dDown / dt
        };
      } else {
        prevClientStats[key] = {
          up: up,
          down: down,
          ts: now,
          upRate: 0,
          downRate: 0
        };
      }
    });
  });
}

function renderLiveClientMonitor() {
  var host = document.querySelector('#clientsView, .clients-section, main, .content') || document.body;
  if (!host) return;

  var wrap = document.getElementById('fapLiveMonitorWrap');
  if (!wrap) {
    wrap = document.createElement('div');
    wrap.id = 'fapLiveMonitorWrap';
    wrap.className = 'card';
    wrap.style.cssText = 'margin-bottom:16px;padding:14px 16px;';
    host.prepend(wrap);
  }

  var inbounds = (typeof state !== 'undefined' && Array.isArray(state.inbounds)) ? state.inbounds : [];
  var allClients = [];
  var totalUpRate = 0;
  var totalDownRate = 0;

  inbounds.forEach(function (ib) {
    (ib.clients || []).forEach(function (cl) {
      var key = String(cl.id || cl.email || cl.uuid || '');
      var st = prevClientStats[key] || { up: Number(cl.up || 0), down: Number(cl.down || 0), upRate: 0, downRate: 0 };
      totalUpRate += st.upRate || 0;
      totalDownRate += st.downRate || 0;
      allClients.push({ inbound: ib, client: cl, stat: st });
    });
  });

  var rows = allClients.map(function (item) {
    var cl = item.client;
    var ib = item.inbound;
    var st = item.stat;

    return '<tr>' +
      '<td><b>' + (cl.email || cl.remark || cl.id || '-') + '</b><div style="font-size:11px;opacity:0.68">' + (ib.remark || ib.tag || '') + ' (' + String(ib.protocol || '').toUpperCase() + ')</div></td>' +
      '<td style="color:#38bdf8;font-family:monospace;">&uarr; ' + fmtRate(st.upRate) + '</td>' +
      '<td style="color:#a78bfa;font-family:monospace;">&darr; ' + fmtRate(st.downRate) + '</td>' +
      '<td style="font-family:monospace;">&uarr; ' + fmtBytesCompact(st.up) + ' / &darr; ' + fmtBytesCompact(st.down) + '</td>' +
      '<td><b>' + fmtBytesCompact(st.up + st.down) + '</b>' + (cl.total ? ' / ' + fmtBytesCompact(cl.total) : '') + '</td>' +
    '</tr>';
  }).join('');

  wrap.innerHTML =
    '<div style="display:flex;flex-wrap:wrap;justify-content:space-between;align-items:center;gap:10px;margin-bottom:12px;">' +
      '<div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;">' +
        '<h3 style="margin:0;font-size:15px;">Client Traffic</h3>' +
        '<span class="tag soft" style="color:#38bdf8">&uarr; ' + fmtRate(totalUpRate) + '</span>' +
        '<span class="tag soft" style="color:#a78bfa">&darr; ' + fmtRate(totalDownRate) + '</span>' +
      '</div>' +
      '<div style="display:flex;align-items:center;gap:8px;">' +
        '<button type="button" class="btn sm" id="fapToggleLiveBtn">' + (liveTrafficEnabled ? 'Live: ON' : 'Live: OFF') + '</button>' +
      '</div>' +
    '</div>' +
    '<div class="table-wrap"><table><thead><tr>' +
      '<th>Client</th><th>Upload Speed</th><th>Download Speed</th><th>Up &amp; Down</th><th>Total Used</th>' +
    '</tr></thead><tbody>' + (rows || '<tr><td colspan="5" style="text-align:center;opacity:0.7;">No clients available</td></tr>') + '</tbody></table></div>';

  var btn = document.getElementById('fapToggleLiveBtn');
  if (btn) {
    btn.onclick = function () {
      liveTrafficEnabled = !liveTrafficEnabled;
      renderLiveClientMonitor();
      if (liveTrafficEnabled) {
        refreshLiveClientsTraffic();
      }
    };
  }
}

function refreshLiveClientsTraffic() {
  if (!liveTrafficEnabled || typeof api !== 'function') return;
  api('GET', 'inbounds').then(function (res) {
    if (!Array.isArray(res) || typeof state === 'undefined') return;
    state.inbounds = res;
    updateClientTrafficSnapshots(res);
    var onClients = !!document.querySelector('[data-page="clients"].active, #clientsView:not(.hidden)') ||
                    /client/i.test(window.location.hash || '') ||
                    !!document.querySelector('[data-act="toggle-client"]');
    if (onClients) {
      renderLiveClientMonitor();
    }
  }).catch(function () {});
}

function fapGenUUIDv4() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
    var r = (Math.random() * 16) | 0;
    var v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function fapGenRandomEmail() {
  var chars = 'abcdefghijklmnopqrstuvwxyz0123456789';
  var tag = '';
  for (var i = 0; i < 6; i++) {
    tag += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return 'user_' + tag + '@fastasian.local';
}

function fapFmtBytesLive(val) {
  var n = Number(val || 0);
  if (!isFinite(n) || n <= 0) return '0 B';
  var units = ['B', 'KB', 'MB', 'GB', 'TB'];
  var idx = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  var num = n / Math.pow(1024, idx);
  return (idx === 0 ? Math.round(num) : num.toFixed(2)) + ' ' + units[idx];
}

function fapIsInboundsSectionVisible() {
  var activeNav = document.querySelector('#sidebar .active, .nav .active, nav .active, [data-nav].active, [data-tab].active, [data-view].active, .tab-btn.active, .nav-item.active');
  if (activeNav) {
    var k = ((activeNav.getAttribute('data-view') || activeNav.getAttribute('data-tab') || activeNav.getAttribute('data-nav') || activeNav.textContent || '')).trim().toLowerCase();
    if (k.includes('inbound')) return true;
    if (k.length > 0 && !k.includes('inbound')) return false;
  }
  var sec = document.querySelector('#inboundsSection, #inbounds-view, #view-inbounds, [data-view="inbounds"]');
  if (sec) {
    var r = sec.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) return true;
  }
  var headings = Array.from(document.querySelectorAll('h1, h2, h3, .section-title, .page-title'));
  for (var i = 0; i < headings.length; i++) {
    var h = headings[i];
    var rect = h.getBoundingClientRect();
    if (rect.width > 0 && rect.height > 0 && /^inbounds?$/i.test((h.textContent || '').trim())) {
      return true;
    }
  }
  return false;
}

function fapFindInboundsSearchInput() {
  var inputs = Array.from(document.querySelectorAll('input[type="search"], input[type="text"], input:not([type])'));
  for (var i = 0; i < inputs.length; i++) {
    var inp = inputs[i];
    if (inp.closest('#fapInboundJsonModal, #sidebar, nav, .sidebar, .nav, .modal')) continue;
    var rect = inp.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) continue;
    var hint = ((inp.placeholder || '') + ' ' + (inp.id || '') + ' ' + (inp.className || '') + ' ' + (inp.getAttribute('aria-label') || '')).toLowerCase();
    var inInboundSec = Boolean(inp.closest('#inboundsSection, #inbounds-view, #view-inbounds, [data-view="inbounds"]'));
    if (inInboundSec || /search|filter|inbound|جستجو|فیلتر/.test(hint)) {
      return inp;
    }
  }
  return null;
}

function fapInjectCustomStyles() {
  if (document.getElementById('fapCustomPanelStyles')) return;
  var st = document.createElement('style');
  st.id = 'fapCustomPanelStyles';
  st.textContent =
    '#fapInboundPageJsonBar{display:inline-flex;align-items:center;gap:8px;flex-wrap:nowrap;flex-shrink:0;}' +
    '#fapInboundPageJsonBar .fap-ib-json-btn{display:inline-flex;align-items:center;justify-content:center;gap:6px;' +
      'height:36px;padding:0 12px;border-radius:10px;font-size:12.5px;font-weight:600;white-space:nowrap;cursor:pointer;' +
      'background:rgba(15,23,42,0.68);color:#e2e8f0;border:1px solid rgba(125,211,252,0.32);' +
      'backdrop-filter:blur(10px);transition:all .16s ease;}' +
    '#fapInboundPageJsonBar .fap-ib-json-btn:hover{border-color:rgba(125,211,252,0.7);background:rgba(30,41,59,0.9);color:#fff;}' +
    '#fapInboundPageJsonBar .fap-ib-json-btn.primary{background:linear-gradient(135deg,rgba(14,165,233,0.32),rgba(56,189,248,0.2));border-color:rgba(56,189,248,0.5);}' +
    '.fap-rand-email-btn{height:34px;padding:0 10px;border-radius:8px;font-size:12px;font-weight:600;cursor:pointer;' +
      'background:rgba(56,189,248,0.16);color:#7dd3fc;border:1px solid rgba(56,189,248,0.38);white-space:nowrap;}' +
    '.fap-rand-email-btn:hover{background:rgba(56,189,248,0.28);color:#fff;}' +
    '.fap-live-traffic-badge{display:inline-flex;align-items:center;gap:6px;font-size:11.5px;font-variant-numeric:tabular-nums;' +
      'padding:2px 8px;border-radius:999px;background:rgba(15,23,42,0.55);border:1px solid rgba(148,163,184,0.22);color:#cbd5e1;}' +
    '@media (max-width:640px){' +
      '#fapInboundPageJsonBar{width:100%;display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:6px;}' +
      '#fapInboundPageJsonBar .fap-ib-json-btn{width:100%;height:38px;font-size:12px;}' +
    '}';
  document.head.appendChild(st);
}

function fapEnhanceClientModalFields() {
  var modals = Array.from(document.querySelectorAll('.modal, [role="dialog"], .drawer, form'));
  for (var i = 0; i < modals.length; i++) {
    var m = modals[i];
    if (m.id === 'fapInboundJsonModal') continue;
    var rect = m.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) continue;
    var txt = (m.textContent || '').toLowerCase();
    if (!txt.includes('client') && !txt.includes('uuid') && !txt.includes('email')) continue;

    var inputs = Array.from(m.querySelectorAll('input'));
    var emailInp = null;
    var uuidInp = null;

    for (var j = 0; j < inputs.length; j++) {
      var inp = inputs[j];
      var sig = ((inp.name || '') + ' ' + (inp.id || '') + ' ' + (inp.placeholder || '') + ' ' + (inp.getAttribute('aria-label') || '')).toLowerCase();
      if (!emailInp && (inp.type === 'email' || sig.includes('email') || sig.includes('remark') || sig.includes('user'))) {
        emailInp = inp;
      }
      if (!uuidInp && (sig.includes('uuid') || sig.includes('id') || sig.includes('password'))) {
        uuidInp = inp;
      }
    }

    if (emailInp && !emailInp.dataset.fapRandWired) {
      emailInp.dataset.fapRandWired = '1';
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'fap-rand-email-btn';
      btn.textContent = 'Random';
      btn.onclick = function () {
        emailInp.value = fapGenRandomEmail();
        emailInp.dispatchEvent(new Event('input', { bubbles: true }));
        emailInp.dispatchEvent(new Event('change', { bubbles: true }));
      };
      if (emailInp.parentElement) {
        emailInp.parentElement.style.display = 'flex';
        emailInp.parentElement.style.alignItems = 'center';
        emailInp.parentElement.style.gap = '6px';
        emailInp.insertAdjacentElement('afterend', btn);
      }
    }

    if (uuidInp) {
      uuidInp.style.display = '';
      uuidInp.removeAttribute('type');
      if (uuidInp.type === 'hidden') uuidInp.type = 'text';
      if (!uuidInp.dataset.fapSeeded && (!uuidInp.value || uuidInp.value.trim() === '')) {
        uuidInp.value = fapGenUUIDv4();
        uuidInp.dataset.fapSeeded = '1';
        uuidInp.dispatchEvent(new Event('input', { bubbles: true }));
        uuidInp.dispatchEvent(new Event('change', { bubbles: true }));
      }
    }
  }
}

var fapPrevTrafficSample = {};

function fapFmtRateLive(bytesPerSec) {
  if (!bytesPerSec || bytesPerSec <= 0) return '0 B/s';
  if (bytesPerSec < 1024) return Math.round(bytesPerSec) + ' B/s';
  if (bytesPerSec < 1024 * 1024) return (bytesPerSec / 1024).toFixed(1) + ' KB/s';
  return (bytesPerSec / (1024 * 1024)).toFixed(2) + ' MB/s';
}

function fapUpdateLiveClientTrafficDOM(inbounds) {
  if (!Array.isArray(inbounds)) return;
  var now = Date.now();
  var clientMap = {};
  for (var i = 0; i < inbounds.length; i++) {
    var ib = inbounds[i] || {};
    var list = Array.isArray(ib.clients) ? ib.clients : (Array.isArray(ib.users) ? ib.users : []);
    for (var j = 0; j < list.length; j++) {
      var cl = list[j] || {};
      var keys = [cl.id, cl.email, cl.uuid].filter(Boolean);
      for (var k = 0; k < keys.length; k++) {
        clientMap[String(keys[k]).toLowerCase()] = cl;
      }
    }
  }

  var rows = Array.from(document.querySelectorAll('tr, .client-row, .client-item, [data-client-email], [data-email]'));
  for (var r = 0; r < rows.length; r++) {
    var row = rows[r];
    var rowText = (row.textContent || '').toLowerCase();
    var matched = null;
    for (var key in clientMap) {
      if (!Object.prototype.hasOwnProperty.call(clientMap, key)) continue;
      if (rowText.includes(key)) {
        matched = clientMap[key];
        break;
      }
    }
    if (!matched) continue;

    var up = Number(matched.up || matched.upload || 0);
    var down = Number(matched.down || matched.download || 0);
    var sampleKey = String(matched.id || matched.email || matched.uuid || '').toLowerCase();
    var upRate = 0;
    var downRate = 0;
    if (sampleKey) {
      var prev = fapPrevTrafficSample[sampleKey];
      if (prev && now > prev.ts) {
        var dtSec = (now - prev.ts) / 1000;
        if (up >= prev.up) upRate = (up - prev.up) / dtSec;
        if (down >= prev.down) downRate = (down - prev.down) / dtSec;
      }
      fapPrevTrafficSample[sampleKey] = { up: up, down: down, ts: now };
    }

    var targetCell = null;
    var cells = Array.from(row.querySelectorAll('td, .traffic-cell, [data-col="traffic"]'));
    for (var c = 0; c < cells.length; c++) {
      var cText = (cells[c].textContent || '').trim();
      if (/\b(\d+(\.\d+)?)\s*(B|KB|MB|GB|TB)\b/i.test(cText) || /traffic/i.test(cells[c].getAttribute('data-label') || '')) {
        targetCell = cells[c];
        break;
      }
    }
    if (!targetCell) {
      targetCell = cells.length >= 2 ? cells[cells.length - 2] : row;
    }

    var staleBadges = row.querySelectorAll('.fap-live-traffic-badge');
    for (var b = 0; b < staleBadges.length; b++) {
      if (staleBadges[b].parentElement !== targetCell) {
        staleBadges[b].remove();
      }
    }

    var badge = targetCell.querySelector('.fap-live-traffic-badge');
    if (!badge) {
      badge = document.createElement('span');
      badge.className = 'fap-live-traffic-badge';
      targetCell.appendChild(badge);
    }
    badge.textContent =
      '↑ ' + fapFmtRateLive(upRate) + ' · ↓ ' + fapFmtRateLive(downRate);
  }
}

async function fapPollClientTrafficOneSecond() {
  if (document.hidden) return;
  fapEnhanceClientModalFields();
  if (typeof api !== 'function') return;
  try {
    var data = await api('GET', 'inbounds');
    var list = Array.isArray(data) ? data : (data && Array.isArray(data.inbounds) ? data.inbounds : null);
    if (list) {
      if (typeof state !== 'undefined' && state && typeof state === 'object') {
        state.inbounds = list;
      }
      fapUpdateLiveClientTrafficDOM(list);
    }
  } catch (_) {}
}

function fapOpenInboundJsonModal(mode, initialJson) {
  var old = document.getElementById('fapInboundJsonModal');
  if (old) old.remove();

  var isImport = mode === 'import';
  var wrap = document.createElement('div');
  wrap.id = 'fapInboundJsonModal';
  wrap.style.cssText =
    'position:fixed;inset:0;z-index:10000;display:flex;align-items:center;justify-content:center;padding:14px;' +
    'background:rgba(2,8,23,0.74);backdrop-filter:blur(10px);';

  wrap.innerHTML =
    '<div style="width:100%;max-width:720px;border-radius:18px;padding:18px;' +
      'background:rgba(15,23,42,0.95);border:1px solid rgba(125,211,252,0.35);' +
      'box-shadow:0 24px 60px rgba(0,0,0,0.65);color:#f8fafc;">' +
      '<div style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px;">' +
        '<div>' +
          '<h3 style="margin:0;font-size:16px;">' + (isImport ? 'Import Inbound & Clients (JSON)' : 'Export Inbound & Clients (JSON)') + '</h3>' +
          '<div style="font-size:12px;opacity:0.75;margin-top:2px;">' +
            (isImport
              ? 'Paste Inbound JSON (including clients) into this note box and click Import.'
              : 'Copy or download JSON of your inbounds and clients.') +
          '</div>' +
        '</div>' +
        '<button type="button" id="fapCloseInboundJsonModal" style="background:none;border:none;color:#94a3b8;font-size:20px;cursor:pointer;">&times;</button>' +
      '</div>' +
      '<textarea id="fapInboundJsonTextarea" spellcheck="false" style="width:100%;min-height:280px;resize:vertical;padding:12px;border-radius:12px;' +
        'font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:12.5px;line-height:1.5;' +
        'background:rgba(2,8,23,0.78);color:#e2e8f0;border:1px solid rgba(255,255,255,0.16);box-sizing:border-box;"></textarea>' +
      '<div id="fapInboundJsonStatus" style="font-size:12px;margin-top:8px;min-height:18px;"></div>' +
      '<div style="display:flex;flex-wrap:wrap;gap:8px;justify-content:flex-end;margin-top:12px;">' +
        (!isImport
          ? '<button type="button" class="btn sm" id="fapCopyInboundJsonBtn">Copy JSON</button>' +
            '<button type="button" class="btn sm primary" id="fapDownloadInboundJsonBtn">Download .json</button>'
          : '<button type="button" class="btn sm primary" id="fapRunImportInboundJsonBtn">Import & Create</button>') +
        '<button type="button" class="btn sm" id="fapCancelInboundJsonBtn">Close</button>' +
      '</div>' +
    '</div>';

  document.body.appendChild(wrap);

  var ta = document.getElementById('fapInboundJsonTextarea');
  var st = document.getElementById('fapInboundJsonStatus');
  if (ta) {
    ta.placeholder = JSON.stringify({
      remark: 'vless-reality',
      protocol: 'vless',
      port: 443,
      clients: [{ email: 'user1', uuid: fapGenUUIDv4() }]
    }, null, 2);
    if (!isImport && initialJson) {
      ta.value = initialJson;
    }
  }

  function closeModal() { wrap.remove(); }
  document.getElementById('fapCloseInboundJsonModal').onclick = closeModal;
  document.getElementById('fapCancelInboundJsonBtn').onclick = closeModal;
  wrap.addEventListener('click', function (e) { if (e.target === wrap) closeModal(); });

  if (!isImport) {
    var copyBtn = document.getElementById('fapCopyInboundJsonBtn');
    if (copyBtn && ta) {
      copyBtn.onclick = function () {
        navigator.clipboard.writeText(ta.value).then(function () {
          st.style.color = '#34d399';
          st.textContent = 'Copied to clipboard.';
        }).catch(function () {
          ta.select();
          document.execCommand('copy');
          st.style.color = '#34d399';
          st.textContent = 'Copied.';
        });
      };
    }
    var dlBtn = document.getElementById('fapDownloadInboundJsonBtn');
    if (dlBtn && ta) {
      dlBtn.onclick = function () {
        var blob = new Blob([ta.value], { type: 'application/json' });
        var url = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url;
        a.download = 'inbounds-export.json';
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
      };
    }
  } else {
    var impBtn = document.getElementById('fapRunImportInboundJsonBtn');
    if (impBtn && ta) {
      impBtn.onclick = function () {
        var raw = (ta.value || '').trim();
        if (!raw) {
          st.style.color = '#f87171';
          st.textContent = 'Please paste JSON first.';
          return;
        }
        impBtn.disabled = true;
        st.style.color = '#7dd3fc';
        st.textContent = 'Creating inbound & clients...';

        fapImportInboundsJSON(raw).then(function (msg) {
          st.style.color = '#34d399';
          st.textContent = msg;
          setTimeout(closeModal, 650);
        }).catch(function (err) {
          impBtn.disabled = false;
          st.style.color = '#f87171';
          st.textContent = 'Error: ' + (err && err.message ? err.message : String(err));
        });
      };
    }
  }
}

async function fapExportInboundsJSON() {
  var inbounds = [];
  if (typeof state !== 'undefined' && state && Array.isArray(state.inbounds) && state.inbounds.length) {
    inbounds = state.inbounds.slice();
  } else if (typeof window !== 'undefined' && window.state && Array.isArray(window.state.inbounds) && window.state.inbounds.length) {
    inbounds = window.state.inbounds.slice();
  }

  if (!inbounds.length) {
    try {
      var res = await fapCallAPI('GET', 'inbounds');
      if (Array.isArray(res)) {
        inbounds = res;
      } else if (res && Array.isArray(res.inbounds)) {
        inbounds = res.inbounds;
      } else if (res && Array.isArray(res.data)) {
        inbounds = res.data;
      }
      if (typeof state !== 'undefined' && state && Array.isArray(inbounds)) {
        state.inbounds = inbounds;
      }
    } catch (_) {}
  }

  var enriched = await Promise.all((inbounds || []).map(async function (ib) {
    var copy = Object.assign({}, ib);
    var ibId = copy.id || copy.inbound_id || '';
    if (!Array.isArray(copy.clients) && ibId) {
      try {
        var cRes = await fapCallAPI('GET', 'inbounds/' + encodeURIComponent(ibId) + '/clients');
        if (Array.isArray(cRes)) copy.clients = cRes;
        else if (cRes && Array.isArray(cRes.clients)) copy.clients = cRes.clients;
      } catch (_) {}
    }
    return copy;
  }));

  return JSON.stringify({
    version: '1.1.8',
    exported_at: new Date().toISOString(),
    inbounds: enriched
  }, null, 2);
}

async function fapCallAPI(method, path, body) {
  if (typeof api === 'function') {
    return await api(method, path, body);
  }
  if (window && typeof window.api === 'function') {
    return await window.api(method, path, body);
  }
  if (window && typeof window.fapAPI === 'function') {
    return await window.fapAPI(method, path, body);
  }

  var pathname = window.location.pathname || '/';
  // Support both /<base>/... and /<base>/api/... paths cleanly
  var base = pathname.replace(/\/+$|\/(?:inbounds|clients|settings|logs|sub)(?:\/.*)?$/i, '');
  var cleanPath = String(path || '').replace(/^\/+/, '');
  if (!/^api\//i.test(cleanPath)) {
    cleanPath = 'api/' + cleanPath;
  }
  var url = (base || '') + '/' + cleanPath;

  var opts = {
    method: method,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' }
  };
  if (body !== undefined && body !== null) {
    opts.body = typeof body === 'string' ? body : JSON.stringify(body);
  }

  var res = await fetch(url, opts);
  var txt = await res.text();
  var data = null;
  try {
    data = txt ? JSON.parse(txt) : null;
  } catch (_) {
    data = txt;
  }
  if (!res.ok) {
    var msg = (data && (data.error || data.message)) || ('HTTP ' + res.status);
    throw new Error(msg);
  }
  return data;
}

async function fapImportInboundsJSON(raw) {
  var parsed = JSON.parse(raw);
  var items = Array.isArray(parsed)
    ? parsed
    : (Array.isArray(parsed.inbounds) ? parsed.inbounds : [parsed]);

  if (!items.length) throw new Error('No inbound object found in JSON');

  var ibCount = 0;
  var clCount = 0;

  for (var i = 0; i < items.length; i++) {
    var ib = JSON.parse(JSON.stringify(items[i] || {}));
    var clients = Array.isArray(ib.clients) ? ib.clients : (Array.isArray(ib.users) ? ib.users : []);
    delete ib.id;
    delete ib.tag;
    delete ib.clients;
    delete ib.users;
    delete ib.up;
    delete ib.down;
    delete ib.live_up;
    delete ib.live_down;

    var res = await fapCallAPI('POST', 'inbounds', ib);
    var inboundId = res && (res.id || (res.inbound && res.inbound.id) || (res.data && res.data.id));
    ibCount++;

    if (inboundId && clients.length) {
      for (var j = 0; j < clients.length; j++) {
        var cl = JSON.parse(JSON.stringify(clients[j] || {}));
        delete cl.id;
        delete cl.up;
        delete cl.down;
        delete cl.live_up;
        delete cl.live_down;
        delete cl.online;
        delete cl.link;
        delete cl.sub_url;
        if (!cl.uuid && typeof fapGenUUIDv4 === 'function') {
          cl.uuid = fapGenUUIDv4();
        }
        cl.inbound_id = inboundId;
        await fapCallAPI('POST', 'clients', cl);
        clCount++;
      }
    }
  }

  if (typeof loadAll === 'function') {
    await loadAll();
  } else if (window && typeof window.loadAll === 'function') {
    await window.loadAll();
  }
  return 'Imported ' + ibCount + ' inbound(s) and ' + clCount + ' client(s).';
}

function fapSyncInboundsToolbar() {
  fapInjectCustomStyles();
  var bar = document.getElementById('fapInboundPageJsonBar');

  if (!fapIsInboundsSectionVisible()) {
    if (bar) bar.remove();
    return;
  }

  var searchInp = fapFindInboundsSearchInput();
  if (bar && searchInp && bar.parentElement && bar.parentElement.contains(searchInp)) {
    return;
  }
  if (bar) bar.remove();

  bar = document.createElement('div');
  bar.id = 'fapInboundPageJsonBar';
  bar.innerHTML =
    '<button type="button" class="fap-ib-json-btn" id="fapInboundPageExportBtn">Export JSON</button>' +
    '<button type="button" class="fap-ib-json-btn primary" id="fapInboundPageImportBtn">Import JSON</button>';

  if (searchInp && searchInp.parentElement) {
    var row = searchInp.parentElement;
    row.style.display = 'flex';
    row.style.flexWrap = 'wrap';
    row.style.alignItems = 'center';
    row.style.gap = '8px';
    searchInp.style.flex = '1 1 200px';
    searchInp.style.minWidth = '160px';
    searchInp.insertAdjacentElement('afterend', bar);
  } else {
    var mainEl = document.querySelector('#inboundsSection, #inbounds-view, #view-inbounds, [data-view="inbounds"], main');
    if (!mainEl) return;
    mainEl.prepend(bar);
  }

  document.getElementById('fapInboundPageExportBtn').onclick = async function () {
    var jsonText = await fapExportInboundsJSON();
    fapOpenInboundJsonModal('export', jsonText);
  };
  document.getElementById('fapInboundPageImportBtn').onclick = function () {
    fapOpenInboundJsonModal('import', '');
  };
}

setInterval(fapSyncInboundsToolbar, 600);
setInterval(fapPollClientTrafficOneSecond, 1000);
setTimeout(function () {
  fapSyncInboundsToolbar();
  fapEnhanceClientModalFields();
  fapPollClientTrafficOneSecond();
}, 250);

// === FAP_RELEASE_NOTIFIER_START ===
var FAP_CURRENT_VERSION = '1.1.4';
var FAP_GITHUB_REPO = 'VahidDashtbani/fastasiapanel';

function fapNormalizeVersion(v) {
  return String(v || '').trim().replace(/^v/i, '');
}

function fapIsNewerVersion(latest, current) {
  var a = fapNormalizeVersion(latest).split('.').map(function (n) { return parseInt(n, 10) || 0; });
  var b = fapNormalizeVersion(current).split('.').map(function (n) { return parseInt(n, 10) || 0; });
  var len = Math.max(a.length, b.length);
  for (var i = 0; i < len; i++) {
    var x = a[i] || 0;
    var y = b[i] || 0;
    if (x > y) return true;
    if (x < y) return false;
  }
  return false;
}

async function fapTriggerSelfUpdate() {
  var btn = document.getElementById('fapUpdateNowBtn');
  var statusEl = document.getElementById('fapUpdateStatusText');
  if (btn) btn.disabled = true;
  if (statusEl) statusEl.textContent = 'در حال اجرای آپدیت...';

  try {
    if (typeof fapCallAPI === 'function') {
      await fapCallAPI('POST', 'api/update', { version: 'latest' });
    } else {
      var res = await fetch('api/update', { method: 'POST', credentials: 'same-origin' });
      if (!res.ok) throw new Error('HTTP ' + res.status);
    }
    if (statusEl) statusEl.textContent = 'آپدیت انجام شد؛ در حال بارگذاری مجدد...';
    setTimeout(function () { window.location.reload(); }, 2500);
  } catch (err) {
    if (statusEl) statusEl.textContent = 'خطا در آپدیت خودکار: ' + (err && err.message ? err.message : err) + ' (از update.sh در سرور استفاده کنید)';
    if (btn) btn.disabled = false;
  }
}

function fapShowReleaseBanner(rel) {
  if (!rel || !rel.tag_name) return;
  if (!fapIsNewerVersion(rel.tag_name, FAP_CURRENT_VERSION)) return;
  if (document.getElementById('fapReleaseUpdateBanner')) return;

  var bar = document.createElement('div');
  bar.id = 'fapReleaseUpdateBanner';
  bar.style.cssText =
    'position:fixed;bottom:16px;left:16px;right:16px;z-index:10050;max-width:760px;margin:0 auto;' +
    'background:#0f172a;color:#f8fafc;border:1px solid #22c55e;border-radius:12px;' +
    'padding:12px 16px;display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:10px;' +
    'box-shadow:0 12px 32px rgba(0,0,0,0.45);direction:rtl;font-family:inherit;';

  bar.innerHTML =
    '<div style="display:flex;flex-direction:column;gap:4px;">' +
      '<strong style="color:#4ade80;">نسخه جدید ' + rel.tag_name + ' منتشر شد!</strong>' +
      '<span id="fapUpdateStatusText" style="font-size:13px;color:#cbd5e1;">نسخه فعلی شما v' + FAP_CURRENT_VERSION + ' است. برای دریافت قابلیت‌ها و رفع باگ‌ها آپدیت کنید.</span>' +
    '</div>' +
    '<div style="display:flex;align-items:center;gap:8px;">' +
      '<button type="button" id="fapUpdateNowBtn" style="background:#16a34a;color:#fff;border:0;border-radius:8px;padding:8px 14px;cursor:pointer;font-weight:600;">آپدیت خودکار</button>' +
      (rel.html_url ? '<a href="' + rel.html_url + '" target="_blank" rel="noopener" style="color:#93c5fd;font-size:13px;text-decoration:none;">مشاهده ریلیز</a>' : '') +
      '<button type="button" id="fapDismissUpdateBtn" style="background:transparent;color:#94a3b8;border:1px solid #334155;border-radius:8px;padding:6px 10px;cursor:pointer;">بستن</button>' +
    '</div>';

  document.body.appendChild(bar);
  document.getElementById('fapUpdateNowBtn').onclick = fapTriggerSelfUpdate;
  document.getElementById('fapDismissUpdateBtn').onclick = function () { bar.remove(); };
}

async function fapCheckLatestRelease() {
  try {
    var url = 'https://api.github.com/repos/' + FAP_GITHUB_REPO + '/releases/latest';
    var res = await fetch(url, { headers: { 'Accept': 'application/vnd.github+json' } });
    if (!res.ok) return;
    var data = await res.json();
    fapShowReleaseBanner(data);
  } catch (_) {}
}

setTimeout(fapCheckLatestRelease, 1500);
setInterval(fapCheckLatestRelease, 3600000);
// === FAP_RELEASE_NOTIFIER_END ===


// Outbound direct edit handler
window.openOutboundEditModal = function(idx, outboundObj) {
  const modal = document.getElementById("outboundEditModal");
  const idxInput = document.getElementById("editOutboundIndex");
  const tagInput = document.getElementById("editOutboundTag");
  const protoInput = document.getElementById("editOutboundProtocol");
  const jsonInput = document.getElementById("editOutboundJson");
  if (!modal || !idxInput || !tagInput || !protoInput || !jsonInput) return;

  const data = outboundObj || (Array.isArray(window.__outboundsCache) ? window.__outboundsCache[idx] : {}) || {};
  idxInput.value = String(idx ?? 0);
  tagInput.value = data.tag || "";
  protoInput.value = data.protocol || "";
  jsonInput.value = JSON.stringify(data, null, 2);
  modal.style.display = "flex";
  modal.classList.remove("hidden");
};

function closeOutboundEditModal() {
  const modal = document.getElementById("outboundEditModal");
  if (!modal) return;
  modal.style.display = "none";
  modal.classList.add("hidden");
}

document.addEventListener("DOMContentLoaded", () => {
  const closeBtn = document.getElementById("closeOutboundEditModal");
  const cancelBtn = document.getElementById("cancelOutboundEditBtn");
  const form = document.getElementById("outboundEditForm");

  if (closeBtn) closeBtn.addEventListener("click", closeOutboundEditModal);
  if (cancelBtn) cancelBtn.addEventListener("click", closeOutboundEditModal);

  if (form) {
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const idx = Number(document.getElementById("editOutboundIndex")?.value || 0);
      const tag = document.getElementById("editOutboundTag")?.value.trim() || "";
      const protocol = document.getElementById("editOutboundProtocol")?.value.trim() || "";
      const rawJson = document.getElementById("editOutboundJson")?.value || "{}";

      let parsed;
      try {
        parsed = JSON.parse(rawJson);
      } catch (err) {
        alert("Invalid JSON: " + err.message);
        return;
      }
      parsed.tag = tag;
      parsed.protocol = protocol;

      const list = Array.isArray(window.__outboundsCache) ? [...window.__outboundsCache] : [];
      list[idx] = parsed;
      window.__outboundsCache = list;

      const endpoints = [
        `/api/outbounds/${idx}`,
        `/api/outbounds/${encodeURIComponent(tag)}`,
        "/api/outbounds"
      ];

      let saved = false;
      for (const ep of endpoints) {
        for (const method of ["PUT", "POST"]) {
          try {
            const payload = ep === "/api/outbounds" ? { index: idx, outbound: parsed, outbounds: list } : parsed;
            const res = await fetch(ep, {
              method,
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify(payload)
            });
            if (res.ok) {
              saved = true;
              break;
            }
          } catch (_) {}
        }
        if (saved) break;
      }

      closeOutboundEditModal();
      if (typeof window.loadOutbounds === "function") {
        window.loadOutbounds();
      } else if (typeof window.renderOutbounds === "function") {
        window.renderOutbounds(list);
      }
    });
  }
});


// QR Code with Config & Subscription switcher
(function() {
  let currentMode = "config";
  let currentData = { config: "", subscription: "", label: "" };

  function buildSubUrl(item) {
    if (!item) return "";
    if (item.subUrl) return item.subUrl;
    if (item.subscriptionUrl) return item.subscriptionUrl;
    if (item.sub_url) return item.sub_url;
    if (item.subscription_url) return item.subscription_url;
    const token = item.subId || item.sub_id || item.token || item.uuid || item.id || "";
    const origin = window.location.origin;
    const baseMatch = window.location.pathname.match(/^(\/[^/]+)/);
    const basePath = baseMatch ? baseMatch[1] : "";
    return token ? `${origin}${basePath}/sub/${encodeURIComponent(String(token))}` : (item.link || item.uri || "");
  }

  function buildConfigValue(item) {
    if (!item) return "";
    if (typeof item === "string") return item;
    return item.link || item.uri || item.config || item.url || item.vless || item.vmess || buildSubUrl(item);
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

  
  function renderUnifiedServerQR(boxId, text, sizePx) {
    const box = document.getElementById(boxId || "fapQrCanvasBox");
    if (!box) return;
    box.innerHTML = "";
    const val = String(text || "").trim();
    const px = sizePx || 210;
    if (!val) {
      box.innerHTML = '<span style="color:#111827;font-size:13px;">No data available</span>';
      return;
    }
    const prefix = (typeof basePrefix === "function") ? basePrefix() : "";
    const qrUrl = (prefix ? prefix : "") + "/api/qr?data=" + encodeURIComponent(val);
    const img = document.createElement("img");
    img.alt = "QR Code";
    img.width = px;
    img.height = px;
    img.style.width = px + "px";
    img.style.height = px + "px";
    img.style.background = "#ffffff";
    img.style.padding = "6px";
    img.style.borderRadius = "10px";
    img.style.display = "block";
    img.style.margin = "0 auto";
    img.src = qrUrl;
    img.onerror = function () {
      if (typeof window.QRCode === "function") {
        box.innerHTML = "";
        try {
          new window.QRCode(box, { text: val, width: px, height: px, correctLevel: window.QRCode.CorrectLevel ? window.QRCode.CorrectLevel.L : 1 });
          return;
        } catch (_) {}
      }
      box.innerHTML = '<div style="color:#111827;font-size:11px;word-break:break-all;padding:8px;">QR render failed</div>';
    };
    box.appendChild(img);
  }

  function renderScannableQr(text) {
    renderUnifiedServerQR("fapQrCanvasBox", text, 210);
  }

  function renderQrIntoBox(text) {
    renderScannableQr(text);
  }

  function syncQrTab() {
    const tabConfig = document.getElementById("fapQrTabConfig");
    const tabSub = document.getElementById("fapQrTabSub");
    const labelEl = document.getElementById("fapQrValueLabel");
    const inputEl = document.getElementById("fapQrValueInput");

    const isSub = currentMode === "subscription";
    const activeVal = isSub ? (currentData.subscription || currentData.config) : (currentData.config || currentData.subscription);

    if (tabConfig) {
      tabConfig.className = !isSub ? "btn btn-primary" : "btn btn-secondary";
    }
    if (tabSub) {
      tabSub.className = isSub ? "btn btn-primary" : "btn btn-secondary";
    }
    if (labelEl) {
      labelEl.textContent = isSub ? "Subscription Link" : "Config Link";
    }
    if (inputEl) {
      inputEl.value = activeVal || "";
    }
    renderQrIntoBox(activeVal || "");
  }

  window.openFapQrModal = function(payload, defaultTab) {
    const modal = document.getElementById("fapQrModal");
    const titleEl = document.getElementById("fapQrTitle");
    if (!modal) return;

    const item = payload && typeof payload === "object" ? payload : { link: String(payload || "") };
    currentData = {
      config: buildConfigValue(item),
      subscription: buildSubUrl(item),
      label: item.email || item.name || item.remark || item.tag || ""
    };
    currentMode = defaultTab === "subscription" ? "subscription" : "config";

    if (titleEl) {
      titleEl.textContent = currentData.label ? `QR Code - ${currentData.label}` : "QR Code";
    }

    modal.style.display = "flex";
    modal.classList.remove("hidden");
    syncQrTab();
  };

  window.closeFapQrModal = function() {
    const modal = document.getElementById("fapQrModal");
    if (!modal) return;
    modal.style.display = "none";
    modal.classList.add("hidden");
  };

  document.addEventListener("DOMContentLoaded", () => {
    document.getElementById("fapQrCloseBtn")?.addEventListener("click", window.closeFapQrModal);
    document.getElementById("fapQrTabConfig")?.addEventListener("click", () => {
      currentMode = "config";
      syncQrTab();
    });
    document.getElementById("fapQrTabSub")?.addEventListener("click", () => {
      currentMode = "subscription";
      syncQrTab();
    });
    document.getElementById("fapQrCopyBtn")?.addEventListener("click", async () => {
      const inputEl = document.getElementById("fapQrValueInput");
      if (!inputEl || !inputEl.value) return;
      try {
        await navigator.clipboard.writeText(inputEl.value);
      } catch (_) {
        inputEl.select();
        document.execCommand("copy");
      }
    });
  });
})();





// === FAP QR & SUBSCRIPTION CONFIGS START ===
(function() {
  // الگوریتم تولید QR Code استاندارد (QR Code Model 2, Byte Mode, Reed-Solomon GF(256))
  // کاملاً آفلاین و اسکن‌پذیر برای تمامی گوشی‌ها و اسکنرها
  const QRGen = (function() {
    const EXP = new Uint8Array(512);
    const LOG = new Uint8Array(256);
    let x = 1;
    for (let i = 0; i < 255; i++) {
      EXP[i] = x;
      LOG[x] = i;
      x <<= 1;
      if (x & 0x100) x ^= 0x11d;
    }
    for (let i = 255; i < 512; i++) EXP[i] = EXP[i - 255];

    function gfMul(a, b) {
      if (a === 0 || b === 0) return 0;
      return EXP[(LOG[a] + LOG[b]) % 255];
    }

    function rsPoly(ecLen) {
      let poly = [1];
      for (let i = 0; i < ecLen; i++) {
        const next = new Array(poly.length + 1).fill(0);
        for (let j = 0; j < poly.length; j++) {
          next[j] ^= poly[j];
          next[j + 1] ^= gfMul(poly[j], EXP[i]);
        }
        poly = next;
      }
      return poly;
    }

    function rsEncode(data, ecLen) {
      const gen = rsPoly(ecLen);
      const res = new Array(ecLen).fill(0);
      for (let i = 0; i < data.length; i++) {
        const factor = data[i] ^ res[0];
        res.shift();
        res.push(0);
        for (let j = 0; j < ecLen; j++) {
          res[j] ^= gfMul(gen[j], factor);
        }
      }
      return res;
    }

    // نسخه‌های استاندارد QR با ECC سطح L/M برای طول‌های مختلف
    const SPECS = [
      // ver, size, totalCodewords, ecCodewordsPerBlock, blocksG1, dataPerBlockG1, blocksG2, dataPerBlockG2
      [1,  21,  26,  7,  1, 19, 0, 0],
      [2,  25,  44, 10,  1, 34, 0, 0],
      [3,  29,  70, 15,  1, 55, 0, 0],
      [4,  33, 100, 20,  1, 80, 0, 0],
      [5,  37, 134, 26,  1, 108, 0, 0],
      [6,  41, 172, 18,  2, 68, 0, 0],
      [7,  45, 196, 20,  2, 78, 0, 0],
      [8,  49, 242, 24,  2, 97, 0, 0],
      [9,  53, 292, 30,  2, 116, 0, 0],
      [10, 57, 346, 18,  2, 68, 2, 69],
      [12, 65, 464, 22,  2, 81, 4, 82],
      [14, 73, 582, 26,  4, 90, 1, 91],
      [16, 81, 728, 30,  5, 98, 2, 99],
      [18, 89, 896, 28,  5, 108, 4, 109],
      [20, 97, 1088, 28, 6, 114, 5, 115],
      [22, 105, 1276, 28, 8, 120, 4, 121],
      [25, 117, 1600, 30, 10, 126, 6, 127]
    ];

    function alignCenters(ver) {
      if (ver === 1) return [];
      const count = Math.floor(ver / 7) + 2;
      const size = ver * 4 + 17;
      if (count === 2) return [6, size - 7];
      const first = 6;
      const last = size - 7;
      const step = Math.round((last - first) / (count - 1));
      const coords = [first];
      for (let i = 1; i < count - 1; i++) {
        coords.push(Math.round(first + (i * (last - first)) / (count - 1)));
      }
      coords.push(last);
      return coords;
    }

    function encodeBits(text) {
      const bytes = new TextEncoder().encode(text);
      for (const spec of SPECS) {
        const [ver, size, totalCw, ecCw, b1, d1, b2, d2] = spec;
        const dataCap = b1 * d1 + b2 * d2;
        const ccBits = ver <= 9 ? 8 : 16;
        // mode (4) + charCount (ccBits) + bytes*8 + terminator (up to 4)
        const maxPayloadBytes = Math.floor((dataCap * 8 - 4 - ccBits) / 8);
        if (bytes.length > maxPayloadBytes) continue;

        const bits = [];
        const pushBits = (val, len) => {
          for (let i = len - 1; i >= 0; i--) bits.push((val >> i) & 1);
        };
        pushBits(0b0100, 4); // Byte mode
        pushBits(bytes.length, ccBits);
        for (const b of bytes) pushBits(b, 8);
        const termLen = Math.min(4, dataCap * 8 - bits.length);
        pushBits(0, termLen);
        while (bits.length % 8 !== 0) bits.push(0);

        const dataCw = [];
        for (let i = 0; i < bits.length; i += 8) {
          let byte = 0;
          for (let j = 0; j < 8; j++) byte = (byte << 1) | bits[i + j];
          dataCw.push(byte);
        }
        const padBytes = [0xec, 0x11];
        let pIdx = 0;
        while (dataCw.length < dataCap) {
          dataCw.push(padBytes[pIdx % 2]);
          pIdx++;
        }

        // Split into blocks and compute RS ECC
        const dataBlocks = [];
        const eccBlocks = [];
        let offset = 0;
        for (let i = 0; i < b1; i++) {
          const blk = dataCw.slice(offset, offset + d1);
          offset += d1;
          dataBlocks.push(blk);
          eccBlocks.push(rsEncode(blk, ecCw));
        }
        for (let i = 0; i < b2; i++) {
          const blk = dataCw.slice(offset, offset + d2);
          offset += d2;
          dataBlocks.push(blk);
          eccBlocks.push(rsEncode(blk, ecCw));
        }

        const interleaved = [];
        const maxD = Math.max(d1, d2);
        for (let i = 0; i < maxD; i++) {
          for (const blk of dataBlocks) if (i < blk.length) interleaved.push(blk[i]);
        }
        for (let i = 0; i < ecCw; i++) {
          for (const eblk of eccBlocks) interleaved.push(eblk[i]);
        }

        const finalBits = [];
        for (const cw of interleaved) {
          for (let i = 7; i >= 0; i--) finalBits.push((cw >> i) & 1);
        }

        return buildMatrix(ver, size, finalBits);
      }
      return null;
    }

    function buildMatrix(ver, size, dataBits) {
      const mat = Array.from({ length: size }, () => new Int8Array(size)); // 0 empty, 1 dark, -1 light
      const locked = Array.from({ length: size }, () => new Uint8Array(size));

      const setMod = (r, c, dark) => {
        if (r < 0 || r >= size || c < 0 || c >= size) return;
        mat[r][c] = dark ? 1 : -1;
        locked[r][c] = 1;
      };

      const placeFinder = (top, left) => {
        for (let r = -1; r <= 7; r++) {
          for (let c = -1; c <= 7; c++) {
            const rr = top + r, cc = left + c;
            if (rr < 0 || rr >= size || cc < 0 || cc >= size) continue;
            const inBox = r >= 0 && r <= 6 && c >= 0 && c <= 6;
            const border = r === 0 || r === 6 || c === 0 || c === 6;
            const center = r >= 2 && r <= 4 && c >= 2 && c <= 4;
            setMod(rr, cc, inBox && (border || center));
          }
        }
      };

      placeFinder(0, 0);
      placeFinder(0, size - 7);
      placeFinder(size - 7, 0);

      // Alignment patterns
      const centers = alignCenters(ver);
      for (const r of centers) {
        for (const c of centers) {
          if (locked[r][c]) continue;
          for (let dr = -2; dr <= 2; dr++) {
            for (let dc = -2; dc <= 2; dc++) {
              const isEdge = Math.abs(dr) === 2 || Math.abs(dc) === 2;
              const isCenter = dr === 0 && dc === 0;
              setMod(r + dr, c + dc, isEdge || isCenter);
            }
          }
        }
      }

      // Timing patterns
      for (let i = 8; i < size - 8; i++) {
        if (!locked[6][i]) setMod(6, i, i % 2 === 0);
        if (!locked[i][6]) setMod(i, 6, i % 2 === 0);
      }

      // Dark module
      setMod(size - 8, 8, true);

      // Reserve format info cells
      for (let i = 0; i < 9; i++) {
        if (i !== 6) {
          locked[8][i] = 1;
          locked[i][8] = 1;
        }
      }
      for (let i = 0; i < 8; i++) {
        locked[8][size - 1 - i] = 1;
        locked[size - 1 - i][8] = 1;
      }

      // Version info for ver >= 7
      if (ver >= 7) {
        let vBits = ver;
        let rem = vBits << 12;
        const poly = 0x1f25;
        for (let i = 17; i >= 12; i--) {
          if ((rem >> i) & 1) rem ^= poly << (i - 12);
        }
        const verInfo = (ver << 12) | rem;
        for (let i = 0; i < 18; i++) {
          const bit = ((verInfo >> i) & 1) === 1;
          const r = Math.floor(i / 3);
          const c = size - 11 + (i % 3);
          setMod(r, c, bit);
          setMod(c, r, bit);
        }
      }

      // Place data bits in zigzag
      let bitIdx = 0;
      let upward = true;
      for (let col = size - 1; col >= 1; col -= 2) {
        if (col === 6) col = 5;
        for (let step = 0; step < size; step++) {
          const row = upward ? (size - 1 - step) : step;
          for (let dx = 0; dx < 2; dx++) {
            const c = col - dx;
            if (locked[row][c]) continue;
            const bit = bitIdx < dataBits.length ? dataBits[bitIdx++] : 0;
            mat[row][c] = bit === 1 ? 1 : -1;
          }
        }
        upward = !upward;
      }

      // Apply mask 0: (r + c) % 2 === 0
      const mask0 = (r, c) => ((r + c) % 2) === 0;
      for (let r = 0; r < size; r++) {
        for (let c = 0; c < size; c++) {
          if (!locked[r][c]) {
            const isDark = mat[r][c] === 1;
            mat[r][c] = (isDark ^ mask0(r, c)) ? 1 : -1;
          }
        }
      }

      // Format info for ECC L (01) and mask 0 (000) => 01000
      const fmtData = 0b01000;
      let fRem = fmtData << 10;
      const gPoly = 0x537;
      for (let i = 14; i >= 10; i--) {
        if ((fRem >> i) & 1) fRem ^= gPoly << (i - 10);
      }
      const fmtBits = ((fmtData << 10) | fRem) ^ 0x5412;

      const placeFormat = (bitsVal) => {
        for (let i = 0; i < 15; i++) {
          const dark = ((bitsVal >> (14 - i)) & 1) === 1;
          if (i < 6) {
            mat[8][i] = dark ? 1 : -1;
          } else if (i === 6) {
            mat[8][7] = dark ? 1 : -1;
          } else if (i === 7) {
            mat[8][8] = dark ? 1 : -1;
          } else if (i === 8) {
            mat[7][8] = dark ? 1 : -1;
          } else {
            mat[14 - i][8] = dark ? 1 : -1;
          }

          if (i < 8) {
            mat[size - 1 - i][8] = dark ? 1 : -1;
          } else {
            mat[8][size - 15 + i] = dark ? 1 : -1;
          }
        }
      };
      placeFormat(fmtBits);

      return { size, mat };
    }

    function toSvg(text, pxSize) {
      const built = encodeBits(text);
      if (!built) return "";
      const { size, mat } = built;
      const quiet = 4;
      const total = size + quiet * 2;
      let rects = "";
      for (let r = 0; r < size; r++) {
        for (let c = 0; c < size; c++) {
          if (mat[r][c] === 1) {
            rects += `<rect x="${c + quiet}" y="${r + quiet}" width="1" height="1" fill="#000000"/>`;
          }
        }
      }
      return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${total} ${total}" width="${pxSize}" height="${pxSize}" shape-rendering="crispEdges"><rect width="${total}" height="${total}" fill="#ffffff"/>${rects}</svg>`;
    }

    return { toSvg };
  })();

  let activeQrTab = "config";
  let currentQrPayload = { config: "", subscription: "", title: "QR Code" };

  function tryDecodeBase64Utf8(str) {
    if (!str) return "";
    const cleaned = String(str).replace(/\s+/g, "");
    if (!/^[A-Za-z0-9+/=_-]+$/.test(cleaned) || cleaned.length < 16) return "";
    try {
      const norm = cleaned.replace(/-/g, "+").replace(/_/g, "/");
      const pad = norm.length % 4 === 0 ? "" : "=".repeat(4 - (norm.length % 4));
      const bin = atob(norm + pad);
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
      const decoded = new TextDecoder("utf-8", { fatal: false }).decode(bytes);
      if (/(vless|vmess|trojan|ss|ssr|hysteria2|hy2|tuic|wireguard):\/\//i.test(decoded)) {
        return decoded.trim();
      }
      return "";
    } catch (_) {
      return "";
    }
  }

  function extractConfigsFromText(raw) {
    if (!raw) return [];
    const lines = String(raw).split(/\r?\n/).map(s => s.trim()).filter(Boolean);
    const results = [];
    for (const ln of lines) {
      if (/(vless|vmess|trojan|ss|ssr|hysteria2|hy2|tuic|wireguard):\/\//i.test(ln)) {
        results.push(ln);
      } else {
        const dec = tryDecodeBase64Utf8(ln);
        if (dec) {
          dec.split(/\r?\n/).map(s => s.trim()).filter(Boolean).forEach(u => results.push(u));
        }
      }
    }
    return Array.from(new Set(results));
  }

  function detectSubUrl(obj) {
    if (!obj || typeof obj !== "object") return "";
    const direct = obj.subUrl || obj.subscriptionUrl || obj.sub_url || obj.subscription_url || obj.subLink || "";
    if (direct) return String(direct);
    const token = obj.subId || obj.sub_id || obj.subToken || obj.token || "";
    if (!token) return "";
    const origin = window.location.origin;
    const baseMatch = window.location.pathname.match(/^(\/[^/]+)/);
    const basePath = baseMatch ? baseMatch[1] : "";
    return `${origin}${basePath}/sub/${encodeURIComponent(String(token))}`;
  }

  function detectConfigText(obj) {
    if (!obj) return "";
    if (typeof obj === "string") {
      const found = extractConfigsFromText(obj);
      return found.length ? found[0] : obj.trim();
    }
    if (Array.isArray(obj.configs) && obj.configs.length) {
      const links = obj.configs.map(c => typeof c === "string" ? c : (c.link || c.uri || c.url || "")).filter(Boolean);
      if (links.length) return links[0];
    }
    const candidate = String(obj.config || obj.link || obj.uri || obj.url || obj.vless || obj.vmess || obj.trojan || obj.ss || "");
    const decoded = extractConfigsFromText(candidate);
    return decoded.length ? decoded[0] : candidate.trim();
  }

  function renderScannableQr(text) {
    renderUnifiedServerQR("fapQrCanvasBox", text, 210);
  }

  function refreshQrModalState() {
    const isSub = activeQrTab === "subscription";
    const cfgBtn = document.getElementById("fapQrTabConfig");
    const subBtn = document.getElementById("fapQrTabSub");
    const labelEl = document.getElementById("fapQrValueLabel");
    const inputEl = document.getElementById("fapQrValueInput");

    const val = isSub
      ? (currentQrPayload.subscription || currentQrPayload.config)
      : (currentQrPayload.config || currentQrPayload.subscription);

    if (cfgBtn) {
      cfgBtn.style.background = !isSub ? "#4f46e5" : "rgba(255,255,255,0.06)";
      cfgBtn.style.color = !isSub ? "#ffffff" : "#cbd5e1";
    }
    if (subBtn) {
      subBtn.style.background = isSub ? "#4f46e5" : "rgba(255,255,255,0.06)";
      subBtn.style.color = isSub ? "#ffffff" : "#cbd5e1";
    }
    if (labelEl) {
      labelEl.textContent = isSub ? "Subscription Link" : "Config Link";
    }
    if (inputEl) {
      inputEl.value = val || "";
    }
    renderScannableQr(val || "");
  }

  window.openFapQrModal = function(item, preferredTab) {
    const modal = document.getElementById("fapQrModal");
    const titleEl = document.getElementById("fapQrTitle");
    if (!modal) return;

    const obj = (item && typeof item === "object") ? item : { config: String(item || "") };
    currentQrPayload = {
      config: detectConfigText(obj),
      subscription: detectSubUrl(obj),
      title: obj.email || obj.name || obj.remark || obj.tag || "QR Code"
    };
    activeQrTab = preferredTab === "subscription" ? "subscription" : "config";

    if (titleEl) {
      titleEl.textContent = `QR - ${currentQrPayload.title}`;
    }

    modal.style.display = "flex";
    refreshQrModalState();
  };

  window.closeFapQrModal = function() {
    const modal = document.getElementById("fapQrModal");
    if (modal) modal.style.display = "none";
  };

  // پیدا کردن کانفیگ‌ها از متن خام اشتراک یا آیتم‌های صفحه Subscription
  function renderConfigsIntoSubscriptionArea() {
    const allNodes = Array.from(document.querySelectorAll("section, div, main, article, .card, .tab-pane, .page"));
    const subSections = allNodes.filter(el => {
      if (el.id === "fapQrModal" || el.classList.contains("fap-sub-configs-box")) return false;
      const txt = el.innerText || "";
      return txt.includes("کد اشتراک") && txt.includes("خروجی خام اشتراک");
    });

    // کوچک‌ترین کانتینر شامل بخش Subscription را انتخاب می‌کنیم
    subSections.sort((a, b) => (a.innerText || "").length - (b.innerText || "").length);
    const targetContainer = subSections[0];
    if (!targetContainer) return;

    let box = targetContainer.querySelector(".fap-sub-configs-box");
    if (!box) {
      box = document.createElement("div");
      box.className = "fap-sub-configs-box";
      box.style.cssText = "margin-top:14px;padding:14px;border-radius:12px;border:1px solid rgba(129,140,248,0.35);background:rgba(15,23,42,0.75);display:grid;gap:10px;";
      targetContainer.appendChild(box);
    }

    const foundConfigs = extractConfigsFromText(targetContainer.innerText || "");
    const subLinkMatch = (targetContainer.innerText || "").match(/https?:\/\/[^\s]+\/sub\/[^\s]+/i);
    const subUrl = subLinkMatch ? subLinkMatch[0].trim() : "";

    if (!foundConfigs.length) {
      box.innerHTML = '<div style="font-size:12px;color:#94a3b8;">کانفیگی در این اشتراک پیدا نشد.</div>';
      return;
    }

    box.innerHTML = `<div style="font-size:13px;font-weight:700;color:#e2e8f0;">کانفیگ‌های اشتراک (${foundConfigs.length})</div>`;

    foundConfigs.forEach((cfgLink, idx) => {
      const hashIdx = cfgLink.indexOf("#");
      const label = hashIdx > -1 ? decodeURIComponent(cfgLink.slice(hashIdx + 1)) : `Config #${idx + 1}`;

      const row = document.createElement("div");
      row.style.cssText = "display:flex;align-items:center;gap:8px;padding:8px 10px;border-radius:8px;background:rgba(2,6,23,0.8);border:1px solid rgba(255,255,255,0.09);";

      const inp = document.createElement("input");
      inp.type = "text";
      inp.readOnly = true;
      inp.value = cfgLink;
      inp.title = label;
      inp.style.cssText = "flex:1;min-width:0;padding:7px 9px;border-radius:6px;border:1px solid rgba(255,255,255,0.12);background:#070b17;color:#e2e8f0;font-family:monospace;font-size:11px;";

      const copyBtn = document.createElement("button");
      copyBtn.type = "button";
      copyBtn.textContent = "کپی کانفیگ";
      copyBtn.style.cssText = "padding:7px 11px;border-radius:6px;border:1px solid rgba(148,163,184,0.3);background:rgba(255,255,255,0.08);color:#f8fafc;cursor:pointer;font-size:11px;white-space:nowrap;";
      copyBtn.onclick = async () => {
        try { await navigator.clipboard.writeText(cfgLink); } catch (_) {}
      };

      const qrBtn = document.createElement("button");
      qrBtn.type = "button";
      qrBtn.textContent = "QR کانفیگ";
      qrBtn.style.cssText = "padding:7px 11px;border-radius:6px;border:1px solid rgba(129,140,248,0.5);background:rgba(79,70,229,0.35);color:#fff;cursor:pointer;font-size:11px;white-space:nowrap;";
      qrBtn.onclick = () => window.openFapQrModal({ email: label, config: cfgLink, subUrl }, "config");

      row.appendChild(inp);
      row.appendChild(copyBtn);
      row.appendChild(qrBtn);
      box.appendChild(row);
    });
  }

  document.addEventListener("DOMContentLoaded", () => {
    document.getElementById("fapQrCloseBtn")?.addEventListener("click", window.closeFapQrModal);
    document.getElementById("fapQrTabConfig")?.addEventListener("click", () => {
      activeQrTab = "config";
      refreshQrModalState();
    });
    document.getElementById("fapQrTabSub")?.addEventListener("click", () => {
      activeQrTab = "subscription";
      refreshQrModalState();
    });
    document.getElementById("fapQrCopyBtn")?.addEventListener("click", async () => {
      const inp = document.getElementById("fapQrValueInput");
      if (!inp || !inp.value) return;
      try { await navigator.clipboard.writeText(inp.value); } catch (_) {}
    });

    setInterval(renderConfigsIntoSubscriptionArea, 1200);
    setTimeout(renderConfigsIntoSubscriptionArea, 400);
  });
})();
// === FAP QR & SUBSCRIPTION CONFIGS END ===


// Reliable delegated handler for Outbound Delete buttons across all views
(function() {
  if (window.__fapDeleteOutboundDelegated) return;
  window.__fapDeleteOutboundDelegated = true;

  document.addEventListener('click', async function(e) {
    const btn = e.target && e.target.closest ? e.target.closest('[data-act="del-outbound"], .del-outbound-btn') : null;
    if (!btn) return;
    e.preventDefault();
    e.stopImmediatePropagation();

    const oid = btn.getAttribute('data-oid') || btn.getAttribute('data-id') || btn.getAttribute('data-tag') || '';
    if (!oid) return;
    if (!confirm('Delete outbound "' + oid + '"?')) return;

    const baseMatch = window.location.pathname.match(/^(\/[^/]+)/);
    const basePath = (baseMatch && baseMatch[1] !== '/outbounds' && baseMatch[1] !== '/routing') ? baseMatch[1] : '';
    const url = basePath + '/api/outbounds/' + encodeURIComponent(oid);

    btn.disabled = true;
    try {
      const res = await fetch(url, { method: 'DELETE' });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        alert('Delete failed: ' + (data.error || data.message || res.statusText));
        btn.disabled = false;
        return;
      }
      if (typeof toast === 'function') toast('Outbound deleted');
      if (typeof renderOutbounds === 'function' && document.getElementById('outbounds-table-box')) {
        renderOutbounds();
      } else if (typeof renderRouting === 'function') {
        renderRouting();
      } else {
        window.location.reload();
      }
    } catch (err) {
      alert('Delete error: ' + (err && err.message ? err.message : String(err)));
      btn.disabled = false;
    }
  }, true);
})();
