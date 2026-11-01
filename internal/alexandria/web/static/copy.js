// Copy buttons for addresses and tokens. Without JavaScript or a clipboard
// the value stays on the page to select by hand.
document.querySelectorAll('button[data-copy]').forEach(button => {
  if (!navigator.clipboard) return;
  button.hidden = false;
  const label = button.textContent;
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(button.dataset.copy);
      button.textContent = button.dataset.copied;
      setTimeout(() => { button.textContent = label; }, 2000);
    } catch (_) {
      const range = document.createRange();
      range.selectNodeContents(button.previousElementSibling);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
    }
  });
});
