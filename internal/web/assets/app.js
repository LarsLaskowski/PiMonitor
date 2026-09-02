(function () {
  let config = {
    version: 'dev',
    poll_interval_seconds: 5,
    history_window_minutes: 60,
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
      memory_warn_percent: 80,
      memory_crit_percent: 95,
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
    } catch {
      // Private browsing or blocked storage: fall back to the in-memory key.
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
    } catch {
      // Private browsing or blocked storage: behave as if nothing was
      // stored, falling back to the OS prefers-color-scheme setting.
      return null;
    }
  }

  function effectiveTheme() {
    const stored = storedTheme();
    if (stored) return stored;
    return window.matchMedia?.('(prefers-color-scheme: dark)')?.matches
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
      document.documentElement.dataset.theme = theme;
    } else {
      delete document.documentElement.dataset.theme;
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

  // Severity ordering for alert.Level values ("ok" < "warn" < "crit"), so
  // the worst of several states for the same card/banner can be picked with
  // a plain comparison.
  function levelRank(level) {
    if (level === 'crit') return 2;
    if (level === 'warn') return 1;
    return 0;
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
    const raw = config?.version || 'dev';
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
    if (snap.cpu.per_core_percent?.length) {
      setText('cpu-per-core', snap.cpu.per_core_percent.map((v, i) => 'C' + i + ': ' + v.toFixed(0) + '%').join('  '));
    }
    lastCPUCount = snap.cpu_count || (snap.cpu.per_core_percent || []).length || 1;

    // CPU details: core count plus model name where the kernel exposes it.
    const cpuModel = snap.system?.cpu_model;
    setText('cpu-info', lastCPUCount + (lastCPUCount === 1 ? ' core' : ' cores') + (cpuModel ? ' · ' + cpuModel : ''));

    // Load average gauges
    renderGauge('gauge-load1', 'load1-value', snap.load_average.load1, t.cpu_warn_percent, t.cpu_crit_percent);
    renderGauge('gauge-load5', 'load5-value', snap.load_average.load5, t.cpu_warn_percent, t.cpu_crit_percent);
    renderGauge('gauge-load15', 'load15-value', snap.load_average.load15, t.cpu_warn_percent, t.cpu_crit_percent);

    // Temperature
    const tempEl = document.getElementById('temp-value');
    if (snap.temperature?.zone) {
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
    renderBar('mem-bar', 'mem-pct', snap.memory.used_percent, t.memory_warn_percent, t.memory_crit_percent,
      fmtBytes(memUsed) + ' / ' + fmtBytes(snap.memory.total_bytes));
    renderBar('swap-bar', 'swap-pct', snap.swap.used_percent, t.swap_warn_percent, t.swap_crit_percent,
      fmtBytes(snap.swap.used_bytes) + ' / ' + fmtBytes(snap.swap.total_bytes));

    // Disks
    renderList('disks-list', (snap.disks || []), d =>
      barRow(d.mountpoint, d.used_percent, t.disk_warn_percent, t.disk_crit_percent,
        fmtBytes(d.used_bytes) + ' / ' + fmtBytes(d.total_bytes))
    );

    // Network. Hidden state combines three independent things: the layout
    // preference (issue #10), the server capability flag, and whether this
    // snapshot actually has data — the server flag alone never lets a
    // disabled network card appear regardless of the stored layout.
    const networkCard = document.getElementById('card-network');
    const networkPref = layout.find(e => e.id === 'network')?.visible !== false;
    if (networkPref && config.network_enabled && snap.network?.length) {
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
    if (document.getElementById('updates-modal').open) {
      renderUpdatesTable();
    }
  }

  // Maps each alert.Report metric name to the badge element(s) it lights up.
  // "memory" and "swap" share the Memory & Swap card's badge; every "disk"
  // state (one per mountpoint) rolls up into the Filesystems card's badge,
  // matching how the issue asks for a per-card badge rather than a
  // per-mountpoint one.
  const ALERT_BADGE_IDS = {
    cpu: ['badge-cpu'],
    temperature: ['badge-temperature'],
    memory: ['badge-memory'],
    swap: ['badge-memory'],
    disk: ['badge-disks'],
  };

  const ALERT_METRIC_LABELS = {
    cpu: 'CPU',
    temperature: 'Temperature',
    memory: 'Memory',
    swap: 'Swap',
    disk: 'Disk',
  };

  // Worst level per badge id and per metric name, derived from the current
  // (already debounced) states only — cleared states are simply absent from
  // `metricLevels` once their level returns to "ok". Split out of
  // renderAlerts to keep each function's cognitive complexity low.
  function computeAlertLevels(report) {
    const badgeLevels = {};
    const metricLevels = {};
    if (!report?.enabled) return { badgeLevels, metricLevels };
    for (const st of report.states || []) {
      if (st.level === 'ok') continue;
      if (levelRank(st.level) > levelRank(metricLevels[st.metric] || 'ok')) {
        metricLevels[st.metric] = st.level;
      }
      for (const id of ALERT_BADGE_IDS[st.metric] || []) {
        if (levelRank(st.level) > levelRank(badgeLevels[id] || 'ok')) {
          badgeLevels[id] = st.level;
        }
      }
    }
    return { badgeLevels, metricLevels };
  }

  // Renders the current GET /api/v1/alerts report: a badge on each affected
  // card's header plus a header banner summarizing the worst active level.
  // Called on every alerts poll, so a cleared condition removes its
  // badge/banner on the very next poll after the server reports it ok.
  function renderAlerts(report) {
    const { badgeLevels, metricLevels } = computeAlertLevels(report);

    const badgeIds = new Set(Object.values(ALERT_BADGE_IDS).flat());
    badgeIds.forEach(id => {
      const el = document.getElementById(id);
      if (!el) return;
      const level = badgeLevels[id];
      if (level) {
        el.textContent = level;
        el.className = 'alert-badge metric-' + level;
      } else {
        el.textContent = '';
        el.className = 'alert-badge hidden';
      }
      updateBadgeAccessibleLabel(el, level);
    });

    renderAlertBanner(metricLevels);
  }

  // A clickable card's accessible name comes entirely from its button's
  // aria-label (the badge span's own text isn't otherwise exposed to
  // assistive tech), so fold the alert level into that label. The base
  // label is cached on first call so repeated renders don't keep
  // re-appending to their own previous suffix.
  function updateBadgeAccessibleLabel(badgeEl, level) {
    const btn = badgeEl.closest('button[aria-label]');
    if (!btn) return;
    if (btn.dataset.baseLabel === undefined) {
      btn.dataset.baseLabel = btn.getAttribute('aria-label');
    }
    const base = btn.dataset.baseLabel;
    btn.setAttribute('aria-label', level ? base + ' — alert: ' + level : base);
  }

  function renderAlertBanner(metricLevels) {
    const banner = document.getElementById('alert-banner');
    const textEl = document.getElementById('alert-banner-text');
    const metrics = Object.keys(metricLevels);
    if (!metrics.length) {
      banner.className = 'alert-banner hidden';
      textEl.textContent = '';
      return;
    }
    metrics.sort((a, b) => levelRank(metricLevels[b]) - levelRank(metricLevels[a]));
    const worst = metricLevels[metrics[0]];
    // Unhide before updating the text: a live region's content changing
    // while it is still display:none is not announced by screen readers.
    banner.className = 'alert-banner metric-' + worst;
    textEl.textContent = metrics
      .map(m => (ALERT_METRIC_LABELS[m] || m) + ' ' + metricLevels[m])
      .join(', ');
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

  // Shared modal focus handling. A native <dialog> shown via showModal()
  // handles top-layer promotion, focus trapping, the ::backdrop, and
  // Escape-to-dismiss on its own; we only need to remember and restore the
  // triggering element's focus, and route each dialog's side effects (e.g.
  // clearing the open detail metric) through its native "close" event so
  // they run no matter how it was dismissed (button, Escape, or backdrop
  // click).
  let modalReturnFocus = null;

  function openModal(dialog, initialFocus) {
    modalReturnFocus = document.activeElement;
    dialog.showModal();
    const target = initialFocus || dialog.querySelector('.modal-close');
    if (target) target.focus();
  }

  // Restore focus once a dialog actually closes, regardless of how.
  function wireModalFocusReturn(dialog) {
    dialog.addEventListener('close', () => {
      if (modalReturnFocus && typeof modalReturnFocus.focus === 'function') {
        modalReturnFocus.focus();
      }
      modalReturnFocus = null;
    });
  }

  // Close when a click lands on the dialog element itself rather than its
  // content, i.e. the ::backdrop area — a native <dialog> has no separate
  // backdrop element to attach a listener to.
  function wireBackdropDismiss(dialog) {
    dialog.addEventListener('click', e => {
      if (e.target === dialog) dialog.close();
    });
  }

  // Dashboard layout customization (issue #10): show/hide and reorder
  // cards, persisted in localStorage — no server-side state. `domId` is the
  // card's existing element id; `id` is the stable identifier stored in
  // localStorage, independent of any future DOM id changes.
  const CARD_DEFS = [
    { id: 'system', domId: 'card-system', label: 'System' },
    { id: 'updates', domId: 'card-updates', label: 'Updates' },
    { id: 'uptime', domId: 'card-uptime', label: 'Uptime' },
    { id: 'cpu', domId: 'card-cpu', label: 'CPU Usage' },
    { id: 'load', domId: 'card-load', label: 'Load Average' },
    { id: 'temperature', domId: 'card-temperature', label: 'Temperature' },
    { id: 'memory', domId: 'card-memory', label: 'Memory & Swap' },
    { id: 'disks', domId: 'card-disks', label: 'Filesystems' },
    { id: 'network', domId: 'card-network', label: 'Network' },
  ];
  const DEFAULT_CARD_ORDER = CARD_DEFS.map(c => c.id);
  const LAYOUT_KEY = 'pimonitor-layout';

  function storedLayout() {
    try {
      const raw = localStorage.getItem(LAYOUT_KEY);
      const parsed = raw ? JSON.parse(raw) : null;
      return Array.isArray(parsed) ? parsed : null;
    } catch {
      // Private/blocked storage, or corrupt JSON: fall back to the default.
      return null;
    }
  }

  function persistLayout(l) {
    try {
      localStorage.setItem(LAYOUT_KEY, JSON.stringify(l));
    } catch (e) {
      console.warn('failed to persist layout', e);
    }
  }

  // Reconciles a stored layout against the known card ids: unknown entries
  // (a card removed in a later release) are dropped and any known card
  // missing from the stored layout (one added in a later release) is
  // appended visible, so a stale localStorage value never silently hides or
  // loses a card.
  function normalizeLayout(stored) {
    const known = new Set(DEFAULT_CARD_ORDER);
    const seen = new Set();
    const result = [];
    (stored || []).forEach(entry => {
      if (!entry || !known.has(entry.id) || seen.has(entry.id)) return;
      seen.add(entry.id);
      result.push({ id: entry.id, visible: entry.visible !== false });
    });
    DEFAULT_CARD_ORDER.forEach(id => {
      if (!seen.has(id)) result.push({ id, visible: true });
    });
    return result;
  }

  let layout = normalizeLayout(storedLayout());

  // Reorders the card elements to match `layout` and applies each card's
  // visibility preference — except "network", whose hidden state also
  // depends on the server capability flag and current data, so it is
  // decided solely in renderMetrics (issue #10's "metrics disabled on the
  // server never appear, regardless of stored layout" acceptance criterion).
  function applyLayout() {
    const main = document.querySelector('main');
    if (!main) return;
    layout.forEach(entry => {
      const def = CARD_DEFS.find(c => c.id === entry.id);
      const el = def && document.getElementById(def.domId);
      if (!el) return;
      main.appendChild(el);
      if (def.id !== 'network') {
        el.classList.toggle('hidden', !entry.visible);
      }
    });
  }

  function setCardVisible(id, visible) {
    const entry = layout.find(e => e.id === id);
    if (!entry) return;
    entry.visible = visible;
    persistLayout(layout);
    applyLayout();
  }

  function moveCard(id, delta) {
    const idx = layout.findIndex(e => e.id === id);
    const target = idx + delta;
    if (idx === -1 || target < 0 || target >= layout.length) return;
    const [entry] = layout.splice(idx, 1);
    layout.splice(target, 0, entry);
    persistLayout(layout);
    applyLayout();
    buildLayoutList();
    // Keep keyboard focus on the card the user just moved, on whichever of
    // its two move buttons is still enabled after the rebuild.
    const li = document.querySelector('.layout-item[data-card-id="' + id + '"]');
    const buttons = li ? li.querySelectorAll('.layout-move') : [];
    const preferred = delta < 0 ? buttons[0] : buttons[1];
    const focusTarget = preferred && !preferred.disabled ? preferred : li?.querySelector('input[type="checkbox"]');
    focusTarget?.focus();
  }

  function resetLayout() {
    layout = normalizeLayout(null);
    persistLayout(layout);
    applyLayout();
    buildLayoutList();
  }

  // Builds the modal's card list from scratch on every open/change — the
  // list is small (one entry per card) so a full rebuild is simpler than
  // patching individual rows, and it keeps the up/down disabled state at
  // the list's ends correct without extra bookkeeping.
  function buildLayoutList() {
    const list = document.getElementById('layout-list');
    if (!list) return;
    list.innerHTML = '';
    layout.forEach((entry, idx) => {
      const def = CARD_DEFS.find(c => c.id === entry.id);
      if (!def) return;
      const li = document.createElement('li');
      li.className = 'layout-item';
      li.dataset.cardId = def.id;

      const label = document.createElement('label');
      label.className = 'layout-item-label';
      const checkbox = document.createElement('input');
      checkbox.type = 'checkbox';
      // A card the server has no data for (currently only network, via
      // network_enabled) can still be reordered, but showing it would never
      // have any effect, so its checkbox is disabled rather than silently
      // ignored.
      const capabilityDisabled = def.id === 'network' && !config.network_enabled;
      checkbox.checked = entry.visible;
      checkbox.disabled = capabilityDisabled;
      checkbox.addEventListener('change', () => setCardVisible(def.id, checkbox.checked));
      const text = document.createElement('span');
      text.textContent = def.label + (capabilityDisabled ? ' (disabled on server)' : '');
      label.append(checkbox, text);

      const controls = document.createElement('div');
      controls.className = 'layout-item-controls';
      const upBtn = document.createElement('button');
      upBtn.type = 'button';
      upBtn.className = 'layout-move';
      upBtn.textContent = '↑';
      upBtn.setAttribute('aria-label', 'Move ' + def.label + ' up');
      upBtn.disabled = idx === 0;
      upBtn.addEventListener('click', () => moveCard(def.id, -1));
      const downBtn = document.createElement('button');
      downBtn.type = 'button';
      downBtn.className = 'layout-move';
      downBtn.textContent = '↓';
      downBtn.setAttribute('aria-label', 'Move ' + def.label + ' down');
      downBtn.disabled = idx === layout.length - 1;
      downBtn.addEventListener('click', () => moveCard(def.id, 1));
      controls.append(upBtn, downBtn);

      li.append(label, controls);
      list.appendChild(li);
    });
  }

  function openLayoutModal() {
    buildLayoutList();
    openModal(document.getElementById('layout-modal'));
  }

  function wireLayoutModal() {
    const dialog = document.getElementById('layout-modal');
    document.getElementById('layout-toggle').addEventListener('click', openLayoutModal);
    document.getElementById('layout-modal-close').addEventListener('click', () => dialog.close());
    document.getElementById('layout-reset').addEventListener('click', resetLayout);
    wireBackdropDismiss(dialog);
    wireModalFocusReturn(dialog);
  }

  function openUpdatesModal() {
    renderUpdatesTable();
    openModal(document.getElementById('updates-modal'));
  }

  // API key prompt: shown when the server answers 401 (an api_key is
  // configured). The entered key is validated against GET /api/v1/config
  // before being persisted, then all data is reloaded with it.
  function openAPIKeyModal() {
    const modal = document.getElementById('apikey-modal');
    if (modal.open) return;
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
    document.getElementById('apikey-modal').close();
    await reloadAll();
  }

  function wireAPIKeyModal() {
    const dialog = document.getElementById('apikey-modal');
    document.getElementById('apikey-form').addEventListener('submit', submitAPIKey);
    // Deliberately not dismissible: without a valid key every card stays
    // empty, so allowing Escape to close it would just leave a
    // broken-looking page.
    dialog.addEventListener('cancel', e => e.preventDefault());
    wireModalFocusReturn(dialog);
  }

  function wireUpdatesModal() {
    const dialog = document.getElementById('updates-modal');
    document.getElementById('updates-show').addEventListener('click', openUpdatesModal);
    document.getElementById('updates-modal-close').addEventListener('click', () => dialog.close());
    wireBackdropDismiss(dialog);
    wireModalFocusReturn(dialog);
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
    if (!points?.length) return [];
    const latest = new Date(points.at(-1).t).getTime();
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
    const cur = vals.at(-1);
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

  function wireDetailModal() {
    const dialog = document.getElementById('detail-modal');
    document.querySelectorAll('[data-metric]').forEach(card => {
      card.addEventListener('click', () => openDetailModal(card.dataset.metric));
    });
    document.getElementById('detail-modal-close').addEventListener('click', () => dialog.close());
    wireBackdropDismiss(dialog);
    wireModalFocusReturn(dialog);
    // Clear the open metric whenever the dialog closes, however it was
    // dismissed, so a stale metric doesn't linger for the next open.
    dialog.addEventListener('close', () => { openDetailMetric = null; });
    document.querySelectorAll('#detail-ranges .range-button').forEach(b => {
      b.addEventListener('click', () => {
        detailRangeMinutes = Number(b.dataset.minutes);
        updateRangeButtons();
        renderDetailChart();
      });
    });
  }

  function renderGauge(canvasId, valueId, value, warnPercent, critPercent) {
    const canvas = document.getElementById(canvasId);
    const cls = levelClass(value, lastCPUCount * warnPercent / 100, lastCPUCount * critPercent / 100);
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

  // History is fetched incrementally: after an initial full-window fetch the
  // dashboard asks only for the points newer than the newest one it already
  // holds (?since=, see docs/API.md) and appends them locally. At the
  // default settings a full window is several thousand points that the Pi
  // would otherwise copy, serialise and gzip once a minute per open
  // dashboard, almost all of it data this browser already has.
  const HISTORY_SCALAR_KEYS = [
    'cpu_percent', 'load1', 'load5', 'load15',
    'temperature', 'memory_used_percent', 'swap_used_percent',
  ];
  const HISTORY_DEVICE_KEYS = [
    'disk_used_percent', 'network_rx_bytes_per_sec', 'network_tx_bytes_per_sec',
  ];

  // Timestamp of the newest point held locally, in the exact string form the
  // server sent it, or null when there is nothing to append to and the next
  // poll has to ask for the whole window.
  let historySince = null;
  // A delta cannot express a history that was replaced rather than appended
  // to (a restart restoring a persisted — possibly reordered — window, a
  // clock step on the Pi), so the full window is re-fetched periodically
  // regardless. Ten polls is ten minutes at the history poll cadence: still
  // an order of magnitude fewer full windows, while any local drift the gap
  // check below fails to notice is corrected within minutes.
  const HISTORY_RESYNC_EVERY_POLLS = 10;
  let historyPollsSinceResync = 0;

  function historyPath(since) {
    return since
      ? '/api/v1/metrics/history?since=' + encodeURIComponent(since)
      : '/api/v1/metrics/history';
  }

  function pointTime(p) {
    return new Date(p?.t).getTime();
  }

  function forEachHistorySeries(hist, fn) {
    for (const key of HISTORY_SCALAR_KEYS) fn(hist?.[key] || []);
    for (const key of HISTORY_DEVICE_KEYS) {
      for (const series of Object.values(hist?.[key] || {})) fn(series);
    }
  }

  // Oldest and newest point across every series, how many points there are
  // in total, and whether any timestamp failed to parse. newestPoint is the
  // point itself rather than just its time: its `t` is passed back verbatim
  // as the next ?since=, since the server's timestamps carry sub-millisecond
  // precision that Date truncates — a re-formatted, truncated `since` would
  // ask for a point we already have and get it back on every poll.
  function historyBounds(hist) {
    let oldest = Infinity;
    let newest = -Infinity;
    let newestPoint = null;
    let points = 0;
    let invalid = false;
    forEachHistorySeries(hist, series => {
      for (const p of series) {
        points++;
        const t = pointTime(p);
        if (!Number.isFinite(t)) { invalid = true; continue; }
        if (t < oldest) oldest = t;
        if (t > newest) { newest = t; newestPoint = p; }
      }
    });
    return { oldest, newest, newestPoint, points, invalid };
  }

  // How large a jump between the newest point held and the oldest point in a
  // delta is still explainable by a late or skipped collector tick. Anything
  // beyond that means points were evicted from the server's window while
  // this tab wasn't looking, so appending would leave a hole in the series.
  function historyGapToleranceMs() {
    return Math.max(3 * Math.max(1, config.poll_interval_seconds) * 1000, 15000);
  }

  // Appends a ?since= delta to the history already held, trimmed back to the
  // retained window. Returns null when the delta cannot be appended safely —
  // a gap, points that are not strictly newer (a restarted server serving
  // restored, reordered history), or an unparseable timestamp — in which
  // case the caller re-fetches the full window instead of charting a series
  // that has silently lost or duplicated points.
  function mergeHistory(prev, delta) {
    const held = historyBounds(prev);
    const incoming = historyBounds(delta);
    if (held.invalid || held.points === 0 || incoming.invalid) return null;
    if (incoming.points > 0) {
      if (incoming.oldest <= held.newest) return null;
      if (incoming.oldest - held.newest > historyGapToleranceMs()) return null;
    }

    const merged = {};
    for (const key of HISTORY_SCALAR_KEYS) {
      merged[key] = (prev[key] || []).concat(delta?.[key] || []);
    }
    for (const key of HISTORY_DEVICE_KEYS) {
      const devices = {};
      for (const [name, series] of Object.entries(prev[key] || {})) devices[name] = series;
      for (const [name, series] of Object.entries(delta?.[key] || {})) {
        devices[name] = (devices[name] || []).concat(series);
      }
      merged[key] = devices;
    }
    return trimHistoryWindow(merged);
  }

  // Drops what the server would have dropped anyway: everything older than
  // history_window_minutes, measured back from the newest point held (the
  // Pi's clock, not the browser's). Without it the locally accumulated
  // window would keep growing for as long as the dashboard stays open. A
  // device left with no points is removed, matching how the server omits
  // devices it has no data for.
  function trimHistoryWindow(hist) {
    const windowMs = Math.max(0, config.history_window_minutes || 0) * 60000;
    const { newest, points } = historyBounds(hist);
    if (!windowMs || points === 0) return hist;
    const cutoff = newest - windowMs;
    const keep = series => series.filter(p => pointTime(p) >= cutoff);

    const out = {};
    for (const key of HISTORY_SCALAR_KEYS) out[key] = keep(hist[key] || []);
    for (const key of HISTORY_DEVICE_KEYS) {
      const devices = {};
      for (const [name, series] of Object.entries(hist[key] || {})) {
        const kept = keep(series);
        if (kept.length) devices[name] = kept;
      }
      out[key] = devices;
    }
    return out;
  }

  function renderHistory(hist) {
    latestHistory = hist;
    const { newestPoint } = historyBounds(hist);
    historySince = newestPoint ? newestPoint.t : null;
    if (hist.cpu_percent) drawSparkline(document.getElementById('cpu-sparkline'), hist.cpu_percent, { min: 0, max: 100 });
    if (hist.temperature) drawSparkline(document.getElementById('temp-sparkline'), hist.temperature);
    // Keep the open detail modal in sync with freshly polled history (and
    // repaint it after a theme change, which re-calls renderHistory).
    renderDetailChart();
  }

  // In-flight guards: setInterval fires on a fixed schedule regardless of
  // whether the previous request finished. Without these, a slow response
  // (loaded Pi, flaky Wi-Fi) lets requests pile up, each adding load to an
  // already-struggling server, with no guarantee responses arrive in order.
  let metricsInFlight = false;
  let historyInFlight = false;
  let alertsInFlight = false;

  async function pollMetrics() {
    if (metricsInFlight) return;
    metricsInFlight = true;
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
    } finally {
      metricsInFlight = false;
    }
  }

  async function pollAlerts() {
    if (alertsInFlight) return;
    alertsInFlight = true;
    try {
      const report = await fetchJSON('/api/v1/alerts');
      renderAlerts(report);
    } catch (e) {
      console.error('failed to fetch alerts', e);
      // Leave the existing badges/banner as they are on a transient
      // failure — a fetch error is not the same as the server reporting
      // the condition cleared, so don't guess.
      if (e.status === 401) {
        openAPIKeyModal();
      }
    } finally {
      alertsInFlight = false;
    }
  }

  async function pollHistory() {
    if (historyInFlight) return;
    historyInFlight = true;
    try {
      const incremental = historySince !== null && latestHistory !== null
        && historyPollsSinceResync < HISTORY_RESYNC_EVERY_POLLS;
      let hist = await fetchJSON(historyPath(incremental ? historySince : null));
      let resynced = !incremental;
      if (incremental) {
        const merged = mergeHistory(latestHistory, hist);
        if (merged) {
          hist = merged;
        } else {
          // The delta didn't line up with what we hold: throw the local
          // window away and start again from the server's.
          hist = await fetchJSON(historyPath(null));
          resynced = true;
        }
      }
      historyPollsSinceResync = resynced ? 0 : historyPollsSinceResync + 1;
      renderHistory(hist);
    } catch (e) {
      console.error('failed to fetch history', e);
      // Whatever failed, the local window may now be missing points (and a
      // rejected ?since= would keep failing): fetch everything next time.
      historySince = null;
    } finally {
      historyInFlight = false;
    }
  }

  let metricsTimer = null;
  let historyTimer = null;
  let alertsTimer = null;

  function startPolling() {
    if (metricsTimer) clearInterval(metricsTimer);
    if (historyTimer) clearInterval(historyTimer);
    if (alertsTimer) clearInterval(alertsTimer);
    const intervalMs = Math.max(1, config.poll_interval_seconds) * 1000;
    metricsTimer = setInterval(pollMetrics, intervalMs);
    historyTimer = setInterval(pollHistory, Math.max(intervalMs, 60000));
    // Same cadence as pollMetrics: alert states should track the dashboard
    // as promptly as the metrics that drive them.
    alertsTimer = setInterval(pollAlerts, intervalMs);
  }

  // Polling is suspended while the tab is hidden: nobody is looking at the
  // data, and every poll costs the Pi a full metrics snapshot (and, once a
  // minute, a full history serialisation). Browsers only throttle background
  // timers to ~1/min rather than stopping them, so a forgotten tab would
  // otherwise poll the device indefinitely.
  function stopPolling() {
    if (metricsTimer) { clearInterval(metricsTimer); metricsTimer = null; }
    if (historyTimer) { clearInterval(historyTimer); historyTimer = null; }
    if (alertsTimer) { clearInterval(alertsTimer); alertsTimer = null; }
  }

  function wireVisibilityPolling() {
    document.addEventListener('visibilitychange', () => {
      if (document.hidden) {
        stopPolling();
        return;
      }
      // Refresh immediately on return so the user never looks at a stale
      // card while waiting for the first interval to elapse.
      pollMetrics();
      pollHistory();
      pollAlerts();
      startPolling();
    });
  }

  // Initial load, re-run after an API key is accepted (the first attempt may
  // have fallen back to default config values on 401, and the poll cadence
  // may change once the real config is readable).
  async function reloadAll() {
    await loadConfig();
    renderVersion();
    await pollMetrics();
    await pollHistory();
    await pollAlerts();
    startPolling();
  }

  async function main() {
    wireThemeToggle();
    wireUpdatesModal();
    wireDetailModal();
    wireAPIKeyModal();
    wireLayoutModal();
    wireVisibilityPolling();
    applyLayout();
    await reloadAll();
  }

  main();
})();
