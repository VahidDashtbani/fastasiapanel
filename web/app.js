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
    return fetch('api/' + path, opts).then(function (res) {
      if (res.status === 401) {
        location.href = 'login';
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

  function go(page) {
    stopTimer();
    state.page = page;
    titleEl.textContent = titles[page];
    topActions.innerHTML = '';
    document.querySelectorAll('#nav button').forEach(function (b) {
      b.classList.toggle('on', b.dataset.page === page);
    });
    if (page === 'dashboard') renderDashboard();
    if (page === 'inbounds') renderInbounds();
    if (page === 'clients') renderClients();
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

  function renderDashboard() {
    function load() {
      api('GET', 'status').then(function (s) {
        var sys = s.sys || {};
        var memPct = sys.mem_total ? (sys.mem_used * 100 / sys.mem_total) : 0;
        var diskPct = sys.disk_total ? (sys.disk_used * 100 / sys.disk_total) : 0;
        var netSpd = '↑ ' + fmtBytes(sys.net_up_speed || 0) + '/s · ↓ ' + fmtBytes(sys.net_down_speed || 0) + '/s';
        var netTot = 'Total: ↑ ' + fmtBytes(sys.net_up || 0) + ' · ↓ ' + fmtBytes(sys.net_down || 0);

        view.innerHTML =
          '<div class="gauges">' +
          gaugeCard('CPU Usage', sys.cpu || 0, (sys.cpu || 0).toFixed(1) + '%', (sys.cpu_cores || 1) + ' Cores · Load: ' + esc(sys.loads || '-')) +
          gaugeCard('RAM Usage', memPct, fmtBytes(sys.mem_used || 0) + ' / ' + fmtBytes(sys.mem_total || 0), 'Panel RSS: ' + fmtBytes(s.panel_mem)) +
          gaugeCard('Disk Usage', diskPct, fmtBytes(sys.disk_used || 0) + ' / ' + fmtBytes(sys.disk_total || 0), esc((sys.os || '') + ' / ' + (sys.arch || ''))) +
          statCard('Server Network', '<span class="num" style="font-size:15px">' + netSpd + '</span>', '<span class="num">' + netTot + '</span>') +
          '</div>' +
          '<div class="cards">' +
          statCard('Xray Core Status', '<span class="dot ' + (s.xray_running ? 'ok' : '') + '"></span>' + (s.xray_running ? 'Running' : 'Stopped'), esc(s.xray_version || '')) +
          statCard('Inbounds / Outbounds', '<span class="num">' + s.inbounds + ' / ' + (s.outbounds || 0) + '</span>', 'Routing Rules: <span class="num">' + (s.routing_rules || 0) + '</span>') +
          statCard('Clients (Online / Active)', '<span class="num">' + (s.online || 0) + ' / ' + s.active + '</span>', 'Total Clients: <span class="num">' + s.clients + '</span>') +
          statCard('Total Client Traffic', '<span class="num">' + fmtBytes(s.up + s.down) + '</span>', '↑ ' + fmtBytes(s.up) + ' · ↓ ' + fmtBytes(s.down)) +
          statCard('Panel Uptime', fmtUptime(s.uptime), 'DB: ' + esc(s.db_type) + ' · v' + esc(s.version)) +
          '</div>';
        applyBars();
      }).catch(fail);
    }
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
      '<th>Status</th><th>Email / Name</th><th>Inbound</th><th>Traffic Usage</th><th>Expiry</th><th>Limits / Quota</th><th>Actions</th>' +
      '</tr></thead><tbody>';

    rows.forEach(function (item) {
      var ib = item.ib;
      var c = item.c;
      var used = (c.up || 0) + (c.down || 0);
      var pct = c.total_bytes > 0 ? Math.min(100, Math.round(used * 100 / c.total_bytes)) : 0;
      var barCls = pct >= 100 ? 'full' : pct >= 85 ? 'high' : '';
      var usage = c.total_bytes > 0
        ? '<span class="bar ' + barCls + '"><i data-pct="' + pct + '"></i></span><span class="num">' + fmtBytes(used) + ' / ' + fmtBytes(c.total_bytes) + '</span>'
        : '<span class="num">' + fmtBytes(used) + '</span> <span class="tag soft">Unlimited</span>';
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
        '<td>' + usage + '</td>' +
        '<td><span class="tag ' + ex.cls + '">' + ex.text + '</span></td>' +
        '<td>' + (limits.length ? '<span class="tag soft">' + esc(limits.join(' · ')) + '</span>' : '<span class="tag soft">-</span>') + '</td>' +
        '<td><div class="row-actions">' +
        '<button class="icon-btn" data-act="qr" data-cid="' + esc(c.id) + '" data-id="' + esc(ib.id) + '">QR</button>' +
        '<button class="icon-btn" data-act="copy" data-cid="' + esc(c.id) + '" data-id="' + esc(ib.id) + '">Link</button>' +
        (c.sub_url ? '<button class="icon-btn" data-act="copy-sub" data-cid="' + esc(c.id) + '" data-id="' + esc(ib.id) + '">Sub</button>' : '') +
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
      opt('hysteria2', 'Hysteria2', proto) +
      opt('mtproto', 'MTProto', proto) +
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
    if (f.security.value === 'reality') {
      if (f.network.value !== 'tcp' && f.network.value !== 'grpc' && f.network.value !== 'xhttp') f.network.value = 'tcp';
      if (f.protocol.value !== 'vless') f.security.value = 'none';
    }
    if (f.protocol.value === 'shadowsocks') {
      f.security.value = 'none';
      f.network.value = 'tcp';
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

  function renderRouting() {
    topActions.innerHTML = '<div class="top-tools">' +
      '<button class="btn sec" data-act="update-geo">Update GeoIP / GeoSite</button>' +
      '<button class="btn sec" data-act="new-outbound">+ New Outbound</button>' +
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

  function openOutboundModal() {
    var body = '<div class="grid2">' +
      '<label>Unique Tag<input class="ltr" name="tag" placeholder="warp-out" required></label>' +
      '<label>Remark<input name="remark" placeholder="Cloudflare WARP"></label>' +
      '<label>Protocol<select name="protocol">' +
      opt('socks', 'SOCKS5', 'socks') +
      opt('http', 'HTTP Proxy', '') +
      opt('freedom', 'Freedom', '') +
      opt('blackhole', 'Blackhole', '') +
      '</select></label>' +
      '<label>Server Address<input class="ltr" name="address" placeholder="127.0.0.1"></label>' +
      '<label>Port<input class="ltr" name="port" type="number" placeholder="40000"></label>' +
      '<label>Username (Optional)<input class="ltr" name="username"></label>' +
      '<label class="span2">Password (Optional)<input class="ltr" name="password"></label>' +
      '</div>';
    openModal('New Outbound', body, function (form, setErr) {
      var f = form.elements;
      api('POST', 'outbounds', {
        tag: f.tag.value.trim(),
        remark: f.remark.value.trim(),
        protocol: f.protocol.value,
        address: f.address.value.trim(),
        port: parseInt(f.port.value, 10) || 0,
        username: f.username.value.trim(),
        password: f.password.value.trim()
      }).then(function () {
        closeModal();
        toast('Outbound created');
        renderRouting();
      }).catch(function (e) { setErr(e.message); });
    });
  }

  function openRuleModal(existingRule) {
    var r = existingRule || {};
    var isEdit = Boolean(r.id);
    var obOpts = opt('direct', 'direct', r.outbound_tag || 'direct') + opt('blocked', 'blocked', r.outbound_tag || '');
    (state.outbounds || []).forEach(function (o) {
      obOpts += opt(o.tag, o.tag + (o.remark ? ' (' + o.remark + ')' : ''), r.outbound_tag || '');
    });

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
    topActions.innerHTML = '<div class="top-tools">' +
      '<button class="btn sec" data-act="log-err">Error Log</button>' +
      '<button class="btn sec" data-act="log-acc">Access Log</button>' +
      '<button class="btn" data-act="show-xcfg">Inspect Xray JSON</button></div>';

    function loadLog() {
      api('GET', 'xray/logs?type=' + state.logType).then(function (res) {
        view.innerHTML = '<div class="card pad"><h3>Live Xray Log (' + esc(state.logType.toUpperCase()) + ')</h3>' +
          '<pre class="link-box" style="max-height:460px;white-space:pre-wrap;font-size:12px">' + esc(res.lines || '') + '</pre></div>';
      }).catch(fail);
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
    var ctx = canvas.getContext('2d');
    var payload = String(text || '');
    var bytes = [];
    for (var i = 0; i < payload.length; i++) {
      var code = payload.charCodeAt(i);
      if (code < 0x80) bytes.push(code);
      else if (code < 0x800) {
        bytes.push(0xc0 | (code >> 6), 0x80 | (code & 0x3f));
      } else {
        bytes.push(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f));
      }
    }
    var ver = bytes.length <= 17 ? 1 : bytes.length <= 32 ? 2 : bytes.length <= 53 ? 3 : bytes.length <= 78 ? 4 : bytes.length <= 106 ? 5 : 6;
    var n = ver * 4 + 17;
    var size = 224;
    var quiet = 4;
    var cell = Math.max(2, Math.floor(size / (n + quiet * 2)));
    canvas.width = n * cell + quiet * cell * 2;
    canvas.height = canvas.width;
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, canvas.width, canvas.height);

    var grid = Array.from({ length: n }, function () { return Array(n).fill(null); });
    var isFunc = Array.from({ length: n }, function () { return Array(n).fill(false); });

    function setMod(x, y, val, func) {
      if (x < 0 || y < 0 || x >= n || y >= n) return;
      grid[y][x] = !!val;
      if (func) isFunc[y][x] = true;
    }
    function finder(cx, cy) {
      for (var dy = -4; dy <= 4; dy++) {
        for (var dx = -4; dx <= 4; dx++) {
          var ax = Math.max(dx, -dx), ay = Math.max(dy, -dy);
          var m = Math.max(ax, ay);
          if (m <= 4) {
            var dark = (m <= 1) || (m === 3);
            setMod(cx + dx, cy + dy, dark, true);
          }
        }
      }
    }
    finder(3, 3);
    finder(n - 4, 3);
    finder(3, n - 4);
    for (var i = 8; i < n - 8; i++) {
      setMod(i, 6, i % 2 === 0, true);
      setMod(6, i, i % 2 === 0, true);
    }
    if (ver >= 2) {
      var ap = n - 7;
      for (var dy = -2; dy <= 2; dy++) {
        for (var dx = -2; dx <= 2; dx++) {
          var d = Math.max(Math.abs(dx), Math.abs(dy)) !== 1;
          setMod(ap + dx, ap + dy, d, true);
        }
      }
    }
    setMod(8, n - 8, true, true);

    var bits = [0, 1, 0, 0];
    var lenBits = ver <= 9 ? 8 : 16;
    var lenVal = Math.min(bytes.length, (1 << lenBits) - 1);
    for (var b = lenBits - 1; b >= 0; b--) bits.push((lenVal >> b) & 1);
    for (var i = 0; i < bytes.length; i++) {
      for (var b = 7; b >= 0; b--) bits.push((bytes[i] >> b) & 1);
    }
    var capBits = ver === 1 ? 152 : ver === 2 ? 272 : ver === 3 ? 440 : ver === 4 ? 640 : ver === 5 ? 864 : 1088;
    for (var p = 0; p < 4 && bits.length < capBits; p++) bits.push(0);
    while (bits.length % 8 !== 0) bits.push(0);
    var pads = [0xec, 0x11];
    var pi = 0;
    while (bits.length < capBits) {
      var pv = pads[pi++ % 2];
      for (var b = 7; b >= 0 && bits.length < capBits; b--) bits.push((pv >> b) & 1);
    }

    var bitIdx = 0;
    var up = true;
    for (var x = n - 1; x > 0; x -= 2) {
      if (x === 6) x = 5;
      for (var step = 0; step < n; step++) {
        var y = up ? (n - 1 - step) : step;
        for (var c = 0; c < 2; c++) {
          var xx = x - c;
          if (!isFunc[y][xx]) {
            var bit = bitIdx < bits.length ? bits[bitIdx++] : 0;
            var masked = ((x + y) % 2 === 0) ? (bit ^ 1) : bit;
            setMod(xx, y, masked === 1, false);
          }
        }
      }
      up = !up;
    }

    // Format info (EC level L = 01, mask 0 = 000) -> 0x77c0
    var fmt = 0x77c0;
    for (var i = 0; i < 15; i++) {
      var fbit = ((fmt >> (14 - i)) & 1) === 1;
      if (i < 6) setMod(8, i, fbit, true);
      else if (i < 8) setMod(8, i + 1, fbit, true);
      else if (i === 8) setMod(7, 8, fbit, true);
      else setMod(14 - i, 8, fbit, true);

      if (i < 8) setMod(n - 1 - i, 8, fbit, true);
      else setMod(8, n - 15 + i, fbit, true);
    }

    ctx.fillStyle = '#0f172a';
    for (var y = 0; y < n; y++) {
      for (var x = 0; x < n; x++) {
        if (grid[y][x]) {
          ctx.fillRect((x + quiet) * cell, (y + quiet) * cell, cell, cell);
        }
      }
    }
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
          setTimeout(function () { location.href = 'login'; }, 900);
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
      return api('POST', 'outbounds/' + el.dataset.oid + '/toggle').then(function () { renderRouting(); }).catch(fail);
    }
    if (act === 'del-ob') {
      return api('DELETE', 'outbounds/' + el.dataset.oid).then(function () { toast('Outbound deleted'); renderRouting(); }).catch(fail);
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
    if (act === 'copy' && cl) return copy(cl.link);
    if (act === 'copy-sub' && cl && cl.sub_url) return copy(cl.sub_url);
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
    api('POST', 'logout').then(function () { location.href = 'login'; }).catch(fail);
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

function fapUpdateLiveClientTrafficDOM(inbounds) {
  if (!Array.isArray(inbounds)) return;
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
    var rowKey = (row.getAttribute('data-client-email') || row.getAttribute('data-email') || row.getAttribute('data-client-id') || '').toLowerCase();
    var matched = rowKey && clientMap[rowKey] ? clientMap[rowKey] : null;
    if (!matched) {
      var rowText = (row.textContent || '').toLowerCase();
      for (var key in clientMap) {
        if (key.includes('@') && rowText.includes(key)) {
          matched = clientMap[key];
          break;
        }
      }
    }
    if (!matched) continue;

    var up = Number(matched.up || matched.upload || 0);
    var down = Number(matched.down || matched.download || 0);
    var badge = row.querySelector('.fap-live-traffic-badge');
    if (!badge) {
      badge = document.createElement('span');
      badge.className = 'fap-live-traffic-badge';
      row.appendChild(badge);
    }
    badge.textContent = '↑ ' + fapFmtBytesLive(up) + '  ↓ ' + fapFmtBytesLive(down);
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

function fapExportInboundsJSON() {
  var inbounds = (typeof state !== 'undefined' && Array.isArray(state.inbounds)) ? state.inbounds : [];
  return JSON.stringify({
    version: '1.1.1',
    exported_at: new Date().toISOString(),
    inbounds: inbounds
  }, null, 2);
}

async function fapImportInboundsJSON(raw) {
  if (typeof api !== 'function') throw new Error('API client not ready');
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
    delete ib.clients;
    delete ib.users;
    delete ib.up;
    delete ib.down;

    var res = await api('POST', 'inbounds', ib);
    var inboundId = res && (res.id || (res.inbound && res.inbound.id));
    ibCount++;

    if (inboundId && clients.length) {
      for (var j = 0; j < clients.length; j++) {
        var cl = JSON.parse(JSON.stringify(clients[j] || {}));
        delete cl.id;
        delete cl.up;
        delete cl.down;
        if (!cl.uuid) cl.uuid = fapGenUUIDv4();
        cl.inbound_id = inboundId;
        await api('POST', 'clients', cl);
        clCount++;
      }
    }
  }

  if (typeof loadAll === 'function') await loadAll();
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

  document.getElementById('fapInboundPageExportBtn').onclick = function () {
    fapOpenInboundJsonModal('export', fapExportInboundsJSON());
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
