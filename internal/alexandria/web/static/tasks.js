(() => {
  // Today's forms all work without script; this only tidies them up.
  const bar = document.querySelector('[data-selection-bar]');
  if (bar) {
    // getAttribute: the boxes are named "id", so bar.id is the boxes, not the form's id.
    const boxes = Array.from(document.querySelectorAll(`input[type="checkbox"][name="id"][form="${bar.getAttribute('id')}"]`));
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
      bar.hidden = n === 0;
    };
    if (all) all.addEventListener('change', () => { for (const b of boxes) b.checked = all.checked; sync(); });
    for (const b of boxes) b.addEventListener('change', sync);
    bar.addEventListener('reset', () => setTimeout(sync));
    sync();
  }
  // The sort menu applies on change; its button is for browsers without script.
  for (const select of document.querySelectorAll('select[data-autosubmit]')) {
    const button = select.form.querySelector('[data-sort-submit]');
    if (button) button.hidden = true;
    select.addEventListener('change', () => select.form.requestSubmit());
  }
  // Permanent deletion waits for its checkbox (the browser also requires it).
  for (const arm of document.querySelectorAll('input[data-arm]')) {
    const button = arm.form.querySelector('[data-armed]');
    const sync = () => { button.disabled = !arm.checked; };
    arm.addEventListener('change', sync);
    sync();
  }
  // The task editor: "No due date" empties the date; Save needs a title.
  for (const form of document.querySelectorAll('.task-editor')) {
    const due = form.querySelector('[data-due]');
    const clearDue = form.querySelector('[data-clear-due]');
    if (due && clearDue) {
      clearDue.hidden = false;
      clearDue.addEventListener('click', () => { due.value = ''; due.focus(); });
    }
    const title = form.querySelector('[data-title]');
    const save = form.querySelector('[data-save]');
    if (title && save) {
      const sync = () => { save.disabled = title.value.trim() === ''; };
      title.addEventListener('input', sync);
      sync();
    }
  }
})();
