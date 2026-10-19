// Older bookmarks may have only a fragment. New links also carry ?section= so
// the server opens their category without JavaScript. Never persist open tiles.
(() => {
  const reveal = () => {
    let id;
    try { id = decodeURIComponent(location.hash.slice(1)); } catch (_) { return; }
    const target = id && document.getElementById(id);
    if (!target) return;
    const tile = target.closest('details.settings-tile');
    if (!tile) return;
    for (let el = target; el; el = el.parentElement) {
      if (el instanceof HTMLDetailsElement) el.open = true;
    }
    target.scrollIntoView({ block: 'start' });
  };
  reveal();
  window.addEventListener('hashchange', reveal);
  // Browser validation can target a field in a category the user has closed.
  document.addEventListener('invalid', event => {
    for (let el = event.target.parentElement; el; el = el.parentElement) {
      if (el instanceof HTMLDetailsElement) el.open = true;
    }
  }, true);
})();
