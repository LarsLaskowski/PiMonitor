// Minimal canvas half-circle gauge for load-average values. Scale runs
// from 0 to `max` (typically the CPU core count, since a load average
// above the core count indicates an overloaded system) and is colored
// via the same ok/warn/crit thresholds used elsewhere in the dashboard.
//
// Canvas angles are measured clockwise from the positive x-axis (0 =
// right, PI/2 = down, PI = left, 3*PI/2 = up). The visible gauge arc runs
// over the top of the circle, i.e. clockwise from PI (left) to 2*PI
// (right) passing through 3*PI/2 (up) - so `anticlockwise` must be false.
function drawGauge(canvas, value, max, colorClass) {
  const dpr = window.devicePixelRatio || 1;
  const cssWidth = canvas.clientWidth || 80;
  const cssHeight = canvas.clientHeight || 70;

  canvas.width = cssWidth * dpr;
  canvas.height = cssHeight * dpr;

  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, cssWidth, cssHeight);

  const lineWidth = 8;
  const margin = 4;
  const cx = cssWidth / 2;
  const cy = cssHeight - margin - lineWidth / 2;
  // The radius must fit both horizontally (half the width) and vertically
  // (the full height above the baseline), always leaving room for the
  // stroke's half-width plus a small margin, so the top of the arc is
  // never clipped even when the canvas is much wider than it is tall.
  const radius = Math.max(
    4,
    Math.min(cssWidth / 2 - lineWidth / 2 - margin, cy - lineWidth / 2 - margin),
  );
  const startAngle = Math.PI; // left (9 o'clock)
  const endAngle = 2 * Math.PI; // right (3 o'clock), via the top

  const trackColor = getComputedStyle(document.documentElement).getPropertyValue('--gauge-track').trim() || 'rgba(0, 0, 0, 0.14)';
  const colorVar = colorClass === 'metric-crit' ? '--crit' : colorClass === 'metric-warn' ? '--warn' : '--ok';
  const fillColor = getComputedStyle(document.documentElement).getPropertyValue(colorVar).trim();

  // Background track (full top semicircle).
  ctx.beginPath();
  ctx.arc(cx, cy, radius, startAngle, endAngle, false);
  ctx.lineWidth = lineWidth;
  ctx.strokeStyle = trackColor;
  ctx.lineCap = 'round';
  ctx.stroke();

  // Value arc (proportional sweep from the left).
  const clamped = Math.max(0, Math.min(value / max, 1));

  ctx.beginPath();
  ctx.arc(cx, cy, radius, startAngle, startAngle + Math.PI * clamped, false);
  ctx.lineWidth = lineWidth;
  ctx.strokeStyle = fillColor;
  ctx.lineCap = 'round';
  ctx.stroke();
}
