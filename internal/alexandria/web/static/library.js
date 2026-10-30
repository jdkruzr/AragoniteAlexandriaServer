(() => {
  // Library rows: a click selects the row into the preview (its title is a
  // plain link that does the same without script); a double-click opens it.
  // The single click waits a moment so a double-click isn't taken as one.
  // One inline action at a time: opening one form closes another.
  const actions = document.querySelectorAll('details.inline-action');
  for (const d of actions) {
    d.addEventListener('toggle', () => {
      if (d.open) for (const other of actions) if (other !== d) other.open = false;
    });
  }
  let timer = null;
  for (const row of document.querySelectorAll('[data-select]')) {
    const go = (href) => { if (href) window.location.href = href; };
    row.addEventListener('click', (e) => {
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      const link = e.target.closest('a');
      if (e.target.closest('input, button, label, select, summary') || (link && !link.hasAttribute('data-select-link'))) return;
      e.preventDefault();
      clearTimeout(timer);
      // An empty data-select means: go where the row's own link goes.
      const href = row.dataset.select || (row.querySelector('a[data-select-link]') || {}).href;
      timer = setTimeout(() => go(href), row.dataset.open ? 250 : 0);
    });
    if (row.dataset.open) {
      row.addEventListener('dblclick', (e) => {
        if (e.target.closest('input, button, label, select, summary')) return;
        clearTimeout(timer);
        go(row.dataset.open);
      });
    }
  }
})();
