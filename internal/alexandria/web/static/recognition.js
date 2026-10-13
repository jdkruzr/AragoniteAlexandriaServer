(() => {
  const form = document.getElementById('library-review');
  if (!form) return;
  const status = document.getElementById('library-selection');
  if (!status) return;
  const boxes = Array.from(form.querySelectorAll('input[name="exclude"]'));
  const update = () => {
    const included = boxes.filter(box => !box.checked);
    const pages = included.reduce((count, box) => count + Number(box.dataset.pages), 0);
    status.textContent = `${included.length.toLocaleString()} notebooks · ${pages.toLocaleString()} pages selected · ${(boxes.length - included.length).toLocaleString()} notebooks excluded.`;
  };
  form.addEventListener('change', update);
  update();
})();
