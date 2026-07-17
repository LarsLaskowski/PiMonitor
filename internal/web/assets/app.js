(function () {
  let config = {
    version: 'dev',
    poll_interval_seconds: 5,
    network_enabled: true,
    thresholds: {
      temperature_warn_c: 60,
      temperature_crit_c: 75,
      cpu_warn_percent: 80,
      cpu_crit_percent: 95,
      disk_warn_percent: 80,
      disk_crit_percent: 95,
      swap_warn_percent: 50,
      swap_crit_percent: 90,
    },
  };
  let lastCPUCount = 1;
  let latestPackages = [];
  // Retained so a theme toggle can immediately repaint the canvas-based
  // widgets (gauges/sparklines read their colors from CSS variables at draw
  // time, so they need an explicit redraw when the palette changes).
  let latestSnapshot = null;
  let latestHistory = null;

  const THEME_KEY = 'pimonitor-theme';
  const API_KEY_STORAGE = 'pimonitor-api-key';

  function storedAPIKey() {
    try {
      return localStorage.getItem(API_KEY_STORAGE) || '';
    } catch (e) {
      return '';
    }
  }

  function persistAPIKey(key) {
    try {
      localStorage.setItem(API_KEY_STORAGE, key);
    } catch (e) {
      // Private browsing or blocked storage: the key still works for this
      // page load via the in-memory fallback below.
      console.warn('failed to persist API key', e);
    }
  }

  // Fallback when localStorage is unavailable, so an entered key at least
  // survives until the next full page load.
  let sessionAPIKey = '';

  function storedTheme() {
    try {
      const v = localStorage.getItem(THEME_KEY);
      return v === 'light' || v === 'dark' ? v : null;
    } catch (e) {
      return null;
    }
  }

  function effectiveTheme() {
    const stored = storedTheme();
    if (stored) return stored;
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
      ? 'dark' : 'light';
  }

  function updateThemeToggle() {
    const btn = document.getElementById('theme-toggle');
    if (!btn) return;
    const dark = effectiveTheme() === 'dark';
    // Show the icon of the mode the button switches to.
    btn.textContent = dark ? '☀️' : '🌙';
    btn.setAttribute('aria-label', dark ? 'Switch to light theme' : 'Switch to dark theme');
    btn.setAttribute('aria-pressed', String(dark));
  }

  function applyTheme(theme) {
    if (theme) {
      document.documentElement.setAttribute('data-theme', theme);
    } else {
      document.documentElement.removeAttribute('data-theme');
    }
    updateThemeToggle();
    // Repaint canvas widgets that cached the previous palette's colors.
    if (latestSnapshot) renderMetrics(latestSnapshot);
    if (latestHistory) renderHistory(latestHistory);
  }

  function toggleTheme() {
    const next = effectiveTheme() === 'dark' ? 'light' : 'dark';
    try {
      localStorage.setItem(THEME_KEY, next);
    } catch (e) {
      console.warn('failed to persist theme choice', e);
    }
    applyTheme(next);
  }

  function wireThemeToggle() {
    updateThemeToggle();
    const btn = document.getElementById('theme-toggle');
    if (btn) btn.addEventListener('click', toggleTheme);
    // Follow live OS changes only while the user has made no explicit choice.
    if (window.matchMedia) {
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (!storedTheme()) applyTheme(null);
      });
    }
  }

  function levelClass(value, warn, crit) {
    if (value >= crit) return 'metric-crit';
    if (value >= warn) return 'metric-warn';
    return 'metric-ok';
  }

  function setText(id, text) {
    const el = document.getElementById(id);
    if (el) el.textContent = text;
  }

  function fmtBytes(bytes) {
    if (bytes === undefined || bytes === null) return '–';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let v = bytes;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024;
      i++;
    }
    return v.toFixed(v >= 10 || i === 0 ? 0 : 1) + ' ' + units[i];
  }

  function fmtBytesPerSec(v) {
    return fmtBytes(v) + '/s';
  }

  function fmtUptime(seconds) {
    if (seconds === undefined || seconds === null) return '–';
    const s = Math.floor(seconds);
    const days = Math.floor(s / 86400);
    const hours = Math.floor((s % 86400) / 3600);
    const mins = Math.floor((s % 3600) / 60);
    const parts = [];
    if (days) parts.push(days + 'd');
    if (hours || days) parts.push(hours + 'h');
    parts.push(mins + 'm');
    return parts.join(' ');
  }

  function fmtClock(date) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  // Fetch a JSON API response, sending the API key (if any) as X-Api-Key.
  // `overrideKey` lets the key prompt validate a candidate key before it is
  // persisted. Non-2xx responses throw an Error carrying `status`, so
  // callers can tell "API key required" (401) apart from other failures.
  async function fetchJSON(path, overrideKey) {
    const key = overrideKey !== undefined ? overrideKey : (storedAPIKey() || sessionAPIKey);
    const res = await fetch(path, key ? { headers: { 'X-Api-Key': key } } : undefined);
    if (!res.ok) {
      const err = new Error(path + ': HTTP ' + res.status);
      err.status = res.status;
      throw err;
    }
    return res.json();
  }

  async function loadConfig() {
    try {
      config = await fetchJSON('/api/v1/config');
    } catch (e) {
      console.warn('failed to load config, using defaults', e);
    }
  }

  function renderVersion() {
    // A release is tagged "vX.Y.Z"; show it without the leading "v".
    // Unversioned local builds report "dev", which is displayed as-is.
    const raw = (config && config.version) || 'dev';
    setText('app-version', raw.replace(/^v(?=\d)/, ''));
  }

  function renderMetrics(snap) {
    latestSnapshot = snap;
    const t = config.thresholds;

    document.getElementById('header-subtitle').textContent =
      'Last updated ' + new Date(snap.timestamp).toLocaleTimeString();

    // Uptime (Pi clock comes from the snapshot timestamp, which is set on
    // the Pi at collection time, not from the viewing browser's clock).
    setText('pi-time', fmtClock(new Date(snap.timestamp)));
    setText('uptime-value', fmtUptime(snap.uptime_seconds));

    // CPU
    setText('cpu-overall', snap.cpu.overall_percent.toFixed(1) + ' %');
    document.getElementById('cpu-overall').className =
      'metric-value ' + levelClass(snap.cpu.overall_percent, t.cpu_warn_percent, t.cpu_crit_percent);
    if (snap.cpu.per_core_percent && snap.cpu.per_core_percent.length) {
      setText('cpu-per-core', snap.cpu.per_core_percent.map((v, i) => 'C' + i + ': ' + v.toFixed(0) + '%').join('  '));
    }
    lastCPUCount = snap.cpu_count || (snap.cpu.per_core_percent || []).length || 1;

    // CPU details: core count plus model name where the kernel exposes it.
    const cpuModel = snap.system && snap.system.cpu_model;
    setText('cpu-info', lastCPUCount + (lastCPUCount === 1 ? ' core' : ' cores') + (cpuModel ? ' · ' + cpuModel : ''));

    // Load average gauges
    renderGauge('gauge-load1', 'load1-value', snap.load_average.load1);
    renderGauge('gauge-load5', 'load5-value', snap.load_average.load5);
    renderGauge('gauge-load15', 'load15-value', snap.load_average.load15);

    // Temperature
    const tempEl = document.getElementById('temp-value');
    if (snap.temperature && snap.temperature.celsius) {
      setText('temp-value', snap.temperature.celsius.toFixed(1) + ' °C');
      tempEl.className = 'metric-value ' + levelClass(snap.temperature.celsius, t.temperature_warn_c, t.temperature_crit_c);
    } else {
      setText('temp-value', 'n/a');
      tempEl.className = 'metric-value';
    }
    setText('temp-gpu', snap.gpu_temperature ? 'GPU: ' + snap.gpu_temperature.celsius.toFixed(1) + ' °C' : '');

    // Memory & swap (show absolute sizes alongside the percentage, like
    // the filesystem rows).
    const memUsed = Math.max(0, (snap.memory.total_bytes || 0) - (snap.memory.available_bytes || 0));
    renderBar('mem-bar', 'mem-pct', snap.memory.used_percent, t.disk_warn_percent, t.disk_crit_percent,
      fmtBytes(memUsed) + ' / ' + fmtBytes(snap.memory.total_bytes));
    renderBar('swap-bar', 'swap-pct', snap.swap.used_percent, t.swap_warn_percent, t.swap_crit_percent,
      fmtBytes(snap.swap.used_bytes) + ' / ' + fmtBytes(snap.swap.total_bytes));

    // Disks
    renderList('disks-list', (snap.disks || []), d =>
      barRow(d.mountpoint, d.used_percent, t.disk_warn_percent, t.disk_crit_percent,
        fmtBytes(d.used_bytes) + ' / ' + fmtBytes(d.total_bytes))
    );

    // Network
    const networkCard = document.getElementById('card-network');
    if (config.network_enabled && snap.network && snap.network.length) {
      networkCard.classList.remove('hidden');
      renderList('network-list', snap.network, n => {
        const row = document.createElement('div');
        row.className = 'bar-row';
        const label = document.createElement('div');
        label.className = 'bar-label';
        const name = document.createElement('span');
        name.className = 'bar-name';
        name.textContent = n.name;
        const rates = document.createElement('span');
        rates.className = 'bar-pct';
        rates.textContent =
          '↓ ' + fmtBytesPerSec(n.rx_bytes_per_sec) +
          ' ↑ ' + fmtBytesPerSec(n.tx_bytes_per_sec);
        label.append(name, rates);
        row.appendChild(label);
        return row;
      });
    } else {
      networkCard.classList.add('hidden');
    }

    // System
    setText('sys-kernel', snap.system.kernel_version || 'unknown');
    setText('sys-distro', snap.system.distribution || 'unknown');
    setText('sys-model', snap.system.pi_model || 'unknown');

    // Updates
    setText('updates-count', String(snap.updates.count));
    if (snap.updates.checked_at) {
      setText('updates-checked', 'Checked ' + new Date(snap.updates.checked_at).toLocaleTimeString());
    }
    document.getElementById('updates-stale').classList.toggle('hidden', !snap.updates.stale);

    latestPackages = (snap.updates.packages || []);
    const showBtn = document.getElementById('updates-show');
    showBtn.classList.toggle('hidden', latestPackages.length === 0);
    showBtn.textContent = latestPackages.length === 1
      ? 'Show 1 update' : 'Show all ' + latestPackages.length + ' updates';
    // Keep the open modal's contents in sync with fresh data.
    if (!document.getElementById('updates-modal').classList.contains('hidden')) {
      renderUpdatesTable();
    }
  }

  function renderUpdatesTable() {
    const body = document.getElementById('updates-table-body');
    body.innerHTML = '';
    latestPackages.forEach(p => {
      const tr = document.createElement('tr');
      const name = document.createElement('td');
      name.className = 'pkg-name';
      name.textContent = p.name;
      const oldV = document.createElement('td');
      oldV.className = 'pkg-old';
      oldV.textContent = p.old_version || '–';
      const newV = document.createElement('td');
      newV.className = 'pkg-new';
      newV.textContent = p.new_version || '–';
      tr.append(name, oldV, newV);
      body.appendChild(tr);
    });
  }

  // Shared modal focus handling. The element focused before a modal opened is
  // remembered so focus can return to it on close (e.g. back to the card that
  // opened the detail view), and focus is moved into the dialog on open.
  let modalReturnFocus = null;

  function focusablesIn(el) {
    return Array.from(
      el.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')
    ).filter(node => node.offsetParent !== null);
  }

  function openModal(backdrop, initialFocus) {
    modalReturnFocus = document.activeElement;
    backdrop.classList.remove('hidden');
    const target = initialFocus || backdrop.querySelector('.modal-close');
    if (target) target.focus();
  }

  function closeModal(backdrop) {
    backdrop.classList.add('hidden');
    if (modalReturnFocus && typeof modalReturnFocus.focus === 'function') {
      modalReturnFocus.focus();
    }
    modalReturnFocus = null;
  }

  // The one visible modal backdrop, if any (only ever one is open at a time).
  function visibleModal() {
    return document.querySelector('.modal-backdrop:not(.hidden)');
  }

  // Route a dismiss request to the matching close function so its side effects
  // (e.g. clearing the open detail metric) still run. The API key prompt is
  // deliberately not dismissible: without a valid key every card stays empty,
  // so closing it would just leave a broken-looking page.
  function dismissModal(backdrop) {
    if (backdrop.id === 'apikey-modal') return;
    if (backdrop.id === 'detail-modal') closeDetailModal();
    else if (backdrop.id === 'updates-modal') closeUpdatesModal();
    else closeModal(backdrop);
  }

  // Keep Tab focus inside the open dialog, as an aria-modal dialog should.
  function trapTab(backdrop, e) {
    const focusables = focusablesIn(backdrop);
    if (!focusables.length) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }

  // A single document-level handler serves whichever modal is open, rather
  // than one Escape listener per modal.
  function wireModalKeys() {
    document.addEventListener('keydown', e => {
      const modal = visibleModal();
      if (!modal) return;
      if (e.key === 'Escape') {
        e.preventDefault();
        dismissModal(modal);
      } else if (e.key === 'Tab') {
        trapTab(modal, e);
      }
    });
  }

  function openUpdatesModal() {
    renderUpdatesTable();
    openModal(document.getElementById('updates-modal'));
  }

  function closeUpdatesModal() {
    closeModal(document.getElementById('updates-modal'));
  }

  // API key prompt: shown when the server answers 401 (an api_key is
  // configured). The entered key is validated against GET /api/v1/config
  // before being persisted, then all data is reloaded with it.
  function openAPIKeyModal() {
    const modal = document.getElementById('apikey-modal');
    if (!modal.classList.contains('hidden')) return;
    setText('header-subtitle', 'API key required');
    document.getElementById('apikey-error').classList.add('hidden');
    openModal(modal, document.getElementById('apikey-input'));
  }

  async function submitAPIKey(e) {
    e.preventDefault();
    const input = document.getElementById('apikey-input');
    const errEl = document.getElementById('apikey-error');
    const key = input.value.trim();
    if (!key) return;
    try {
      await fetchJSON('/api/v1/config', key);
    } catch (err) {
      errEl.textContent = err.status === 401
        ? 'Invalid API key' : 'Could not verify the key: connection error';
      errEl.classList.remove('hidden');
      return;
    }
    persistAPIKey(key);
    sessionAPIKey = key;
    input.value = '';
    errEl.classList.add('hidden');
    closeModal(document.getElementById('apikey-modal'));
    await reloadAll();
  }

  function wireAPIKeyModal() {
    document.getElementById('apikey-form').addEventListener('submit', submitAPIKey);
  }

  function wireUpdatesModal() {
    document.getElementById('updates-show').addEventListener('click', openUpdatesModal);
    document.getElementById('updates-modal-close').addEventListener('click', closeUpdatesModal);
    document.getElementById('updates-modal').addEventListener('click', e => {
      // Close when clicking the backdrop, but not the dialog itself.
      if (e.target === e.currentTarget) closeUpdatesModal();
    });
  }

  // Metric detail view: clicking a card opens a modal with a larger chart of
  // that metric's history plus range buttons. Each entry maps a card's
  // data-metric attribute to the matching History series and how to render it.
  const DETAIL_METRICS = {
    cpu: {
      title: 'CPU Usage',
      historyKey: 'cpu_percent',
      opts: { min: 0, max: 100 },
      fmt: v => v.toFixed(1) + ' %',
    },
    load: {
      title: 'Load Average (1 min)',
      historyKey: 'load1',
      opts: { min: 0 },
      fmt: v => v.toFixed(2),
    },
    temperature: {
      title: 'Temperature',
      historyKey: 'temperature',
      opts: {},
      fmt: v => v.toFixed(1) + ' °C',
    },
    memory: {
      title: 'Memory Usage',
      historyKey: 'memory_used_percent',
      opts: { min: 0, max: 100 },
      fmt: v => v.toFixed(1) + ' %',
    },
  };

  let openDetailMetric = null;
  // Default span; bounded in practice by however much history the server
  // retains (history_window_minutes), since points beyond that aren't returned.
  let detailRangeMinutes = 15;

  function detailSeries(metricKey) {
    const meta = DETAIL_METRICS[metricKey];
    if (!meta || !latestHistory) return [];
    return latestHistory[meta.historyKey] || [];
  }

  // Keep only the points within the last `minutes`, measured back from the
  // most recent sample's timestamp (the Pi clock), not the browser's clock.
  function pointsWithinRange(points, minutes) {
    if (!points || !points.length) return [];
    const latest = new Date(points[points.length - 1].t).getTime();
    const cutoff = latest - minutes * 60000;
    return points.filter(p => new Date(p.t).getTime() >= cutoff);
  }

  function updateRangeButtons() {
    document.querySelectorAll('#detail-ranges .range-button').forEach(b => {
      const active = Number(b.dataset.minutes) === detailRangeMinutes;
      b.classList.toggle('active', active);
      b.setAttribute('aria-pressed', String(active));
    });
  }

  function renderDetailChart() {
    if (!openDetailMetric) return;
    const meta = DETAIL_METRICS[openDetailMetric];
    const points = pointsWithinRange(detailSeries(openDetailMetric), detailRangeMinutes);
    drawSparkline(document.getElementById('detail-chart'), points, meta.opts);

    const stats = document.getElementById('detail-stats');
    if (!points.length) {
      stats.textContent = 'No history for the selected range yet';
      return;
    }
    // drawSparkline needs at least two points to draw a line, so with a single
    // sample the chart is intentionally blank; say so rather than showing a
    // full stats line next to an empty chart.
    if (points.length < 2) {
      stats.textContent = 'Now ' + meta.fmt(points[0].v) + ' · collecting more samples to plot…';
      return;
    }
    const vals = points.map(p => p.v);
    const cur = vals[vals.length - 1];
    const min = Math.min(...vals);
    const max = Math.max(...vals);
    const avg = vals.reduce((a, b) => a + b, 0) / vals.length;
    stats.textContent =
      'Now ' + meta.fmt(cur) + ' · min ' + meta.fmt(min) +
      ' · avg ' + meta.fmt(avg) + ' · max ' + meta.fmt(max) +
      ' · ' + vals.length + ' samples';
  }

  function openDetailModal(metricKey) {
    if (!DETAIL_METRICS[metricKey]) return;
    openDetailMetric = metricKey;
    setText('detail-modal-title', DETAIL_METRICS[metricKey].title);
    updateRangeButtons();
    openModal(document.getElementById('detail-modal'));
    // Draw after the modal is visible so the canvas has a measurable size.
    renderDetailChart();
  }

  function closeDetailModal() {
    openDetailMetric = null;
    closeModal(document.getElementById('detail-modal'));
  }

  function wireDetailModal() {
    document.querySelectorAll('[data-metric]').forEach(card => {
      card.addEventListener('click', () => openDetailModal(card.dataset.metric));
      card.addEventListener('keydown', e => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          openDetailModal(card.dataset.metric);
        }
      });
    });
    document.getElementById('detail-modal-close').addEventListener('click', closeDetailModal);
    document.getElementById('detail-modal').addEventListener('click', e => {
      if (e.target === e.currentTarget) closeDetailModal();
    });
    document.querySelectorAll('#detail-ranges .range-button').forEach(b => {
      b.addEventListener('click', () => {
        detailRangeMinutes = Number(b.dataset.minutes);
        updateRangeButtons();
        renderDetailChart();
      });
    });
  }

  function renderGauge(canvasId, valueId, value) {
    const canvas = document.getElementById(canvasId);
    const cls = levelClass(value, lastCPUCount * 0.7, lastCPUCount * 1.0);
    drawGauge(canvas, value, Math.max(lastCPUCount, 1), cls);
    setText(valueId, value.toFixed(2));
  }

  function renderBar(barId, pctId, value, warn, crit, subText) {
    const cls = levelClass(value, warn, crit);
    const bar = document.getElementById(barId);
    bar.style.width = Math.min(value, 100).toFixed(1) + '%';
    bar.className = 'bar-fill ' + cls;
    setText(pctId, value.toFixed(1) + ' %' + (subText ? ' · ' + subText : ''));
  }

  function barRow(name, pct, warn, crit, subText) {
    const cls = levelClass(pct, warn, crit);
    const row = document.createElement('div');
    row.className = 'bar-row';

    const label = document.createElement('div');
    label.className = 'bar-label';
    const nameEl = document.createElement('span');
    nameEl.className = 'bar-name';
    nameEl.title = name;
    nameEl.textContent = name;
    const pctEl = document.createElement('span');
    pctEl.className = 'bar-pct';
    pctEl.textContent = pct.toFixed(1) + '% · ' + subText;
    label.append(nameEl, pctEl);

    const track = document.createElement('div');
    track.className = 'bar-track';
    const fill = document.createElement('div');
    fill.className = 'bar-fill ' + cls;
    fill.style.width = Math.min(pct, 100).toFixed(1) + '%';
    track.appendChild(fill);

    row.append(label, track);
    return row;
  }

  function renderList(containerId, items, renderItem) {
    const container = document.getElementById(containerId);
    container.innerHTML = '';
    if (!items.length) {
      const empty = document.createElement('div');
      empty.className = 'metric-sub';
      empty.textContent = 'No data';
      container.appendChild(empty);
      return;
    }
    items.forEach(item => container.appendChild(renderItem(item)));
  }

  function renderHistory(hist) {
    latestHistory = hist;
    if (hist.cpu_percent) drawSparkline(document.getElementById('cpu-sparkline'), hist.cpu_percent, { min: 0, max: 100 });
    if (hist.temperature) drawSparkline(document.getElementById('temp-sparkline'), hist.temperature);
    // Keep the open detail modal in sync with freshly polled history (and
    // repaint it after a theme change, which re-calls renderHistory).
    renderDetailChart();
  }

  async function pollMetrics() {
    try {
      const snap = await fetchJSON('/api/v1/metrics');
      renderMetrics(snap);
    } catch (e) {
      console.error('failed to fetch metrics', e);
      if (e.status === 401) {
        // An api_key is configured and we have no (valid) key: ask for one
        // instead of pretending the server is unreachable. Also covers a key
        // rotated server-side while the dashboard is open.
        openAPIKeyModal();
      } else {
        document.getElementById('header-subtitle').textContent = 'Connection error';
      }
    }
  }

  async function pollHistory() {
    try {
      const hist = await fetchJSON('/api/v1/metrics/history');
      renderHistory(hist);
    } catch (e) {
      console.error('failed to fetch history', e);
    }
  }

  let metricsTimer = null;
  let historyTimer = null;

  function startPolling() {
    if (metricsTimer) clearInterval(metricsTimer);
    if (historyTimer) clearInterval(historyTimer);
    const intervalMs = Math.max(1, config.poll_interval_seconds) * 1000;
    metricsTimer = setInterval(pollMetrics, intervalMs);
    historyTimer = setInterval(pollHistory, Math.max(intervalMs, 60000));
  }

  // Initial load, re-run after an API key is accepted (the first attempt may
  // have fallen back to default config values on 401, and the poll cadence
  // may change once the real config is readable).
  async function reloadAll() {
    await loadConfig();
    renderVersion();
    await pollMetrics();
    await pollHistory();
    startPolling();
  }

  async function main() {
    wireThemeToggle();
    wireModalKeys();
    wireUpdatesModal();
    wireDetailModal();
    wireAPIKeyModal();
    await reloadAll();
  }

  main();
})();
