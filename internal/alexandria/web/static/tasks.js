(() => {
  // "Select all" ticks every task box that belongs to the bulk form.
  for (const all of document.querySelectorAll('input[data-select-all]')) {
    const boxes = Array.from(document.querySelectorAll(`input[type="checkbox"][form="${all.dataset.selectAll}"]`));
    if (!boxes.length) continue;
    all.closest('label').hidden = false;
    const sync = () => {
      const n = boxes.filter(b => b.checked).length;
      all.checked = n === boxes.length;
      all.indeterminate = n > 0 && n < boxes.length;
    };
    all.addEventListener('change', () => { for (const b of boxes) b.checked = all.checked; sync(); });
    for (const b of boxes) b.addEventListener('change', sync);
    sync();
  }
})();
