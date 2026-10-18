// Copy remains optional: readonly text fields work with script or clipboard disabled.
document.querySelectorAll('[data-copy]').forEach(button => {
  if (button.dataset.bound) return;
  button.dataset.bound = 'true';
  button.hidden = false;
  button.addEventListener('click', async () => {
    const input = button.parentElement.querySelector('input');
    const status = button.closest('details').querySelector('[data-copy-status]');
    try {
      await navigator.clipboard.writeText(input.value);
      status.textContent = status.dataset.success;
    } catch (_) {
      input.focus(); input.select();
      status.textContent = status.dataset.failure;
    }
  });
});
