(() => {
  // Today's task editor; forms.js has the selection bar, sorting and
  // confirmations. Everything works without script.
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
