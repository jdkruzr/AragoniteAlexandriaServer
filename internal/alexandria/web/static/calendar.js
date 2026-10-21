// Native disclosures still work when JavaScript or browser storage is disabled.
(() => {
  document.querySelectorAll('details.calendar-disclosure').forEach(section => {
    const key = 'alexandria.calendar.' + section.id + '.collapsed';
    try {
      const collapsed = localStorage.getItem(key);
      if (collapsed !== null) section.open = collapsed !== 'true';
    } catch (_) { /* Storage may be blocked by browser policy. */ }
    section.addEventListener('toggle', () => {
      try {
        localStorage.setItem(key, String(!section.open));
      } catch (_) { /* Collapsing does not require persistence. */ }
    });
  });
})();
