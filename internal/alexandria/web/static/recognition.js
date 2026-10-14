(() => {
  // Filter boxes hide notebook rows whose title doesn't match.
  for (const input of document.querySelectorAll('input[data-filter]')) {
    const table = document.getElementById(input.dataset.filter);
    if (!table) continue;
    const rows = Array.from(table.tBodies[0].rows);
    input.addEventListener('input', () => {
      const q = input.value.trim().toLowerCase();
      for (const row of rows) row.hidden = q !== '' && !row.cells[table.id === 'library-notebooks' ? 1 : 0].textContent.toLowerCase().includes(q);
    });
  }
  const form = document.getElementById('library-review');
  const status = document.getElementById('library-selection');
  const send = document.getElementById('library-send');
  if (!form || !status) return;
  const off = send ? send.disabled : true;
  const boxes = Array.from(form.querySelectorAll('input[name="exclude"]'));
  const update = () => {
    const included = boxes.filter(box => !box.checked);
    const pages = included.reduce((count, box) => count + Number(box.dataset.pages), 0);
    status.textContent = `${included.length.toLocaleString()} notebooks · ${pages.toLocaleString()} pages selected · ${(boxes.length - included.length).toLocaleString()} notebooks excluded.`;
    if (send) {
      send.textContent = `Send ${pages.toLocaleString()} ${pages === 1 ? 'page' : 'pages'} to ${send.dataset.model || 'recognition'}`;
      send.disabled = off || pages === 0;
    }
  };
  form.addEventListener('change', update);
  update();
})();
