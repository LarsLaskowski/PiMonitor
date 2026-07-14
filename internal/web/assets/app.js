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

  async function fetchJSON(path) {
    const res = await fetch(path);
    if (!res.ok) throw new Error(path + ': HTTP ' + res.status);
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

  function openUpdatesModal() {
    renderUpdatesTable();
    document.getElementById('updates-modal').classList.remove('hidden');
  }

  function closeUpdatesModal() {
    document.getElementById('updates-modal').classList.add('hidden');
  }

  function wireUpdatesModal() {
    document.getElementById('updates-show').addEventListener('click', openUpdatesModal);
    document.getElementById('updates-modal-close').addEventListener('click', closeUpdatesModal);
    document.getElementById('updates-modal').addEventListener('click', e => {
      // Close when clicking the backdrop, but not the dialog itself.
      if (e.target === e.currentTarget) closeUpdatesModal();
    });
    document.addEventListener('keydown', e => {
      if (e.key === 'Escape') closeUpdatesModal();
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
  }

  async function pollMetrics() {
    try {
      const snap = await fetchJSON('/api/v1/metrics');
      renderMetrics(snap);
    } catch (e) {
      console.error('failed to fetch metrics', e);
      document.getElementById('header-subtitle').textContent = 'Connection error';
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

  async function main() {
    wireThemeToggle();
    wireUpdatesModal();
    await loadConfig();
    renderVersion();
    const intervalMs = Math.max(1, config.poll_interval_seconds) * 1000;

    await pollMetrics();
    await pollHistory();

    setInterval(pollMetrics, intervalMs);
    setInterval(pollHistory, Math.max(intervalMs, 60000));
  }

  main();
})();
