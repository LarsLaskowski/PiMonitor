// Registry of the selectable dashboard themes; the first entry is the default
// (Clear). A theme's id is its `data-style` value and the stored choice.
const PIMONITOR_THEMES = Object.freeze([
  Object.freeze({ id: 'clear', name: 'Clear' }),
]);

// Clear's light values of the custom properties the canvas renderers read.
const DRAWING_FALLBACKS = Object.freeze({
  '--accent': '#2563eb',
  '--muted': '#5f6876',
  '--card-bg': '#ffffff',
  '--track': '#e8eaef',
  '--ok': '#16a34a',
  '--warn': '#d97706',
  '--crit': '#dc2626',
  '--spark-fill-alpha': '0.28',
});

// themeValue returns the trimmed computed value of the custom property `name`.
function themeValue(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

// themeColor returns the usable color of the custom property `name`: its
// computed value when that is a color, else Clear's value.
function themeColor(name) {
  const v = themeValue(name);
  const supported = window.CSS?.supports ? CSS.supports('color', v) : true;
  return v !== '' && supported ? v : DRAWING_FALLBACKS[name];
}

// themeNumber returns the number of the custom property `name` when it is
// finite and within [min, max], else Clear's value.
function themeNumber(name, min, max) {
  const v = themeValue(name);
  const n = Number(v);
  return v !== '' && Number.isFinite(n) && n >= min && n <= max ? n : Number(DRAWING_FALLBACKS[name]);
}

// Apply a previously chosen theme before the first paint to avoid a flash
// of the wrong palette. With no stored choice we leave `data-theme` unset
// so the CSS `prefers-color-scheme` default takes over.
//
// Loaded as a synchronous script in <head> (rather than inline) so the
// strict Content-Security-Policy (script-src 'self') applies; being
// render-blocking, it still runs before the first paint.
(function () {
  try {
    const stored = localStorage.getItem('pimonitor-theme');
    if (stored === 'light' || stored === 'dark') {
      document.documentElement.dataset.theme = stored;
    }
  } catch (e) {
    // Private browsing or blocked storage: leave data-theme unset, same as
    // when nothing was ever stored; the CSS prefers-color-scheme default
    // still applies.
  }
  // The stored theme is applied only when it is a registered id other than
  // the default (Clear, which is the absence of data-style); any other value
  // is ignored and left in storage untouched.
  try {
    const style = localStorage.getItem('pimonitor-style');
    if (style !== PIMONITOR_THEMES[0].id && PIMONITOR_THEMES.some(t => t.id === style)) {
      document.documentElement.dataset.style = style;
    }
  } catch (e) {
    // Blocked storage: leave data-style unset, which is Clear.
  }
})();
