(function () {
  let config = {
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
    const secs = s % 60;
    const parts = [];
    if (days) parts.push(days + 'd');
    if (hours || days) parts.push(hours + 'h');
    parts.push(mins + 'm');
    parts.push(secs + 's');
    return parts.join(' ');
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

  function renderMetrics(snap) {
    const t = config.thresholds;

    document.getElementById('header-subtitle').textContent =
      'Last updated ' + new Date(snap.timestamp).toLocaleTimeString();

    // Uptime (Pi clock comes from the snapshot timestamp, which is set on
    // the Pi at collection time, not from the viewing browser's clock).
    setText('pi-time', new Date(snap.timestamp).toLocaleTimeString());
    setText('uptime-value', fmtUptime(snap.uptime_seconds));

    // CPU
    setText('cpu-overall', snap.cpu.overall_percent.toFixed(1) + ' %');
    document.getElementById('cpu-overall').className =
      'metric-value ' + levelClass(snap.cpu.overall_percent, t.cpu_warn_percent, t.cpu_crit_percent);
    if (snap.cpu.per_core_percent && snap.cpu.per_core_percent.length) {
      setText('cpu-per-core', snap.cpu.per_core_percent.map((v, i) => 'C' + i + ': ' + v.toFixed(0) + '%').join('  '));
    }
    lastCPUCount = snap.cpu_count || snap.cpu.per_core_percent.length || 1;

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

    // Memory & swap
    renderBar('mem-bar', 'mem-pct', snap.memory.used_percent, t.disk_warn_percent, t.disk_crit_percent);
    renderBar('swap-bar', 'swap-pct', snap.swap.used_percent, t.swap_warn_percent, t.swap_crit_percent);

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
        row.innerHTML =
          '<div class="bar-label"><span class="bar-name">' + n.name + '</span>' +
          '<span class="bar-pct">↓ ' + fmtBytesPerSec(n.rx_bytes_per_sec) +
          ' ↑ ' + fmtBytesPerSec(n.tx_bytes_per_sec) + '</span></div>';
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
    const list = document.getElementById('updates-list');
    list.innerHTML = '';
    if (snap.updates.packages && snap.updates.packages.length) {
      list.classList.remove('hidden');
      snap.updates.packages.forEach(p => {
        const li = document.createElement('li');
        li.textContent = p.name + ': ' + p.old_version + ' → ' + p.new_version;
        list.appendChild(li);
      });
    } else {
      list.classList.add('hidden');
    }
  }

  function renderGauge(canvasId, valueId, value) {
    const canvas = document.getElementById(canvasId);
    const cls = levelClass(value, lastCPUCount * 0.7, lastCPUCount * 1.0);
    drawGauge(canvas, value, Math.max(lastCPUCount, 1), cls);
    setText(valueId, value.toFixed(2));
  }

  function renderBar(barId, pctId, value, warn, crit) {
    const cls = levelClass(value, warn, crit);
    const bar = document.getElementById(barId);
    bar.style.width = Math.min(value, 100).toFixed(1) + '%';
    bar.className = 'bar-fill ' + cls;
    setText(pctId, value.toFixed(1) + ' %');
  }

  function barRow(name, pct, warn, crit, subText) {
    const cls = levelClass(pct, warn, crit);
    const row = document.createElement('div');
    row.className = 'bar-row';
    row.innerHTML =
      '<div class="bar-label"><span class="bar-name" title="' + name + '">' + name + '</span>' +
      '<span class="bar-pct">' + pct.toFixed(1) + '% · ' + subText + '</span></div>' +
      '<div class="bar-track"><div class="bar-fill ' + cls + '" style="width:' + Math.min(pct, 100).toFixed(1) + '%"></div></div>';
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
    await loadConfig();
    const intervalMs = Math.max(1, config.poll_interval_seconds) * 1000;

    await pollMetrics();
    await pollHistory();

    setInterval(pollMetrics, intervalMs);
    setInterval(pollHistory, Math.max(intervalMs, 60000));
  }

  main();
})();
