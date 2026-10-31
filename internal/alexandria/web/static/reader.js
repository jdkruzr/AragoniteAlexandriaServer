(() => {
  // Reader: keep the current page in view in the filmstrip, and turn pages
  // with the arrow keys. The page links work the same without script.
  const current = document.querySelector('.filmstrip [aria-current="page"]');
  if (current) {
    const strip = current.parentElement;
    strip.scrollLeft = current.offsetLeft - (strip.clientWidth - current.offsetWidth) / 2;
  }
  document.addEventListener('keydown', (e) => {
    if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
    if (e.target.closest('input, textarea, select, [contenteditable], audio, details[open]')) return;
    const rel = e.key === 'ArrowLeft' ? 'prev' : e.key === 'ArrowRight' ? 'next' : '';
    const link = rel && document.querySelector('.reader a[rel="' + rel + '"]');
    if (link) {
      e.preventDefault();
      window.location.href = link.href;
    }
  });
  // One inline action at a time.
  const actions = document.querySelectorAll('details.inline-action');
  for (const d of actions) {
    d.addEventListener('toggle', () => {
      if (d.open) for (const other of actions) if (other !== d) other.open = false;
    });
  }
})();
