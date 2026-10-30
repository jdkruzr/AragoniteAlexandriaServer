(() => {
  // Shared by Today and the Library. Every form works without script; this
  // only tidies them up.
  // A selection bar acts on the ticked rows: it shows while any are ticked,
  // with their count. Its boxes are the checkboxes tied to it by form=.
  for (const bar of document.querySelectorAll('[data-selection-bar]')) {
    // getAttribute: boxes named "id" shadow the form's own id property.
    const boxes = Array.from(document.querySelectorAll(`input[type="checkbox"][form="${bar.getAttribute('id')}"]:not([data-arm])`));
    const all = bar.querySelector('input[data-select-all]');
    const count = bar.querySelector('[data-selection-count]');
    const clear = bar.querySelector('[data-selection-clear]');
    const label = count ? (n) => (bar.dataset.countTemplate || '{n} selected').replace('{n}', n) : null;
    if (all && boxes.length) all.closest('label').hidden = false;
    if (count) count.hidden = false;
    if (clear) clear.hidden = false;
    const sync = () => {
      const n = boxes.filter(b => b.checked).length;
      if (all) { all.checked = n > 0 && n === boxes.length; all.indeterminate = n > 0 && n < boxes.length; }
      if (count) count.textContent = label(n);
      for (const el of bar.querySelectorAll('[data-one]')) el.hidden = n !== 1;
      bar.hidden = n === 0;
    };
    if (all) all.addEventListener('change', () => { for (const b of boxes) b.checked = all.checked; sync(); });
    for (const b of boxes) b.addEventListener('change', sync);
    bar.addEventListener('reset', () => setTimeout(sync));
    if (clear && clear.type !== 'reset') clear.addEventListener('click', () => { for (const b of boxes) b.checked = false; sync(); });
    sync();
  }
  // Menus apply on change; their buttons are for browsers without script.
  for (const select of document.querySelectorAll('select[data-autosubmit]')) {
    for (const button of select.form.querySelectorAll('[data-sort-submit]')) button.hidden = true;
    select.addEventListener('change', () => select.form.requestSubmit());
  }
  // A destructive button waits for its checkbox (the browser also requires
  // it). The pair shares a <details> or, failing that, a form.
  for (const arm of document.querySelectorAll('input[data-arm]')) {
    const button = (arm.closest('details') || arm.form).querySelector('[data-armed]');
    if (!button) continue;
    const sync = () => { button.disabled = !arm.checked; };
    arm.addEventListener('change', sync);
    sync();
  }
})();
