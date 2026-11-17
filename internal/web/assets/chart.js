// Minimal canvas sparkline renderer. Deliberately hand-rolled instead of
// vendoring a charting library: the dashboard only needs a handful of
// small trend lines, not zoom/pan/tooltip interactivity.
function drawSparkline(canvas, points, opts) {
  opts = opts || {};
  const color = opts.color || getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#2563eb';
  const dpr = window.devicePixelRatio || 1;
  const cssWidth = canvas.clientWidth || 260;
  const cssHeight = canvas.clientHeight || 48;

  canvas.width = cssWidth * dpr;
  canvas.height = cssHeight * dpr;

  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, cssWidth, cssHeight);

  if (!points || points.length < 2) {
    return;
  }

  const values = points.map(p => p.v);
  let min = opts.min !== undefined ? opts.min : Math.min(...values);
  let max = opts.max !== undefined ? opts.max : Math.max(...values);
  if (max - min < 1e-9) {
    // Flat line: pad the range so it renders as a straight line at
    // mid-height instead of collapsing to zero height.
    min -= 1;
    max += 1;
  }

  const pad = 3;
  const stepX = (cssWidth - pad * 2) / (points.length - 1);

  const yFor = v => {
    const t = (v - min) / (max - min);
    return cssHeight - pad - t * (cssHeight - pad * 2);
  };

  ctx.beginPath();
  points.forEach((p, i) => {
    const x = pad + i * stepX;
    const y = yFor(p.v);
    if (i === 0) {
      ctx.moveTo(x, y);
    } else {
      ctx.lineTo(x, y);
    }
  });
  ctx.strokeStyle = color;
  ctx.lineWidth = 1.5;
  ctx.stroke();

  ctx.lineTo(pad + (points.length - 1) * stepX, cssHeight);
  ctx.lineTo(pad, cssHeight);
  ctx.closePath();
  ctx.fillStyle = color + '26'; // ~15% alpha fill under the line
  ctx.fill();
}

// Time-scaled line chart for series whose points are not evenly spaced
// (drawSparkline spaces them by index). lines is [{ points: [{ t, v }], color }]
// with t in milliseconds; opts is { from, to, gapMs, markers, markerColor } in
// milliseconds. The y scale runs from 0 to the largest value, a line is broken
// where neighboring points are more than gapMs apart, and each marker is a
// dashed vertical line.
function drawTimeChart(canvas, lines, opts) {
  const dpr = window.devicePixelRatio || 1;
  const cssWidth = canvas.clientWidth || 260;
  const cssHeight = canvas.clientHeight || 64;

  canvas.width = cssWidth * dpr;
  canvas.height = cssHeight * dpr;

  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, cssWidth, cssHeight);

  const values = [];
  lines.forEach(line => line.points.forEach(p => values.push(p.v)));
  if (!values.length) {
    return;
  }
  const yMin = 0;
  let yMax = Math.max(...values);
  if (yMax - yMin < 1e-9) {
    yMax = yMin + 1;
  }

  const pad = 3;
  const span = opts.to - opts.from || 1;
  const xFor = t => pad + ((t - opts.from) / span) * (cssWidth - pad * 2);
  const yFor = v => cssHeight - pad - ((v - yMin) / (yMax - yMin)) * (cssHeight - pad * 2);

  ctx.lineWidth = 1;
  ctx.strokeStyle = opts.markerColor;
  ctx.setLineDash([3, 3]);
  (opts.markers || []).forEach(t => {
    const x = Math.round(xFor(t)) + 0.5;
    ctx.beginPath();
    ctx.moveTo(x, 0);
    ctx.lineTo(x, cssHeight);
    ctx.stroke();
  });
  ctx.setLineDash([]);

  ctx.lineWidth = 1.5;
  lines.forEach(line => {
    ctx.beginPath();
    let prev = null;
    line.points.forEach(p => {
      const x = xFor(p.t);
      const y = yFor(p.v);
      if (prev === null || p.t - prev.t > opts.gapMs) {
        ctx.moveTo(x, y);
      } else {
        ctx.lineTo(x, y);
      }
      prev = p;
    });
    ctx.strokeStyle = line.color;
    ctx.stroke();
  });
}
