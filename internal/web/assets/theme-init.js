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
