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

// themeColor returns the usable color of the custom property `name`.
function themeColor(name) {
  throw new Error('not implemented: themeColor(' + name + ')');
}

// themeNumber returns the number of the custom property `name` within [min, max].
function themeNumber(name, min, max) {
  throw new Error('not implemented: themeNumber(' + name + ', ' + min + ', ' + max + ')');
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
})();
