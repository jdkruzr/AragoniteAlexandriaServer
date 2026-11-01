(() => {
  // The editor's end time follows the start, an hour later, until it is
  // changed by hand. Without script the server does the same for an empty end.
  const start = document.querySelector('.cal-form [data-start]');
  const end = document.querySelector('.cal-form [data-end]');
  if (start && end) {
    const later = (value) => {
      const [h, m] = value.split(':').map(Number);
      if (Number.isNaN(h) || Number.isNaN(m)) return '';
      return h >= 23 ? '23:59' : String(h + 1).padStart(2, '0') + ':' + String(m).padStart(2, '0');
    };
    let follows = !end.value || end.value === later(start.value);
    end.addEventListener('input', () => { follows = false; });
    start.addEventListener('input', () => { if (follows && start.value) end.value = later(start.value); });
  }
})();
