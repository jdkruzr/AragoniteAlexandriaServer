// Live regions: while a page reports ongoing work (<meta name="alexandria-live">),
// fetch the same URL in the background and swap only the elements marked
// data-live-region (each with an id). The server's HTML stays authoritative;
// without JavaScript a <noscript> meta refresh reloads the page instead.
// Scroll position, open <details>, typing and focus are left alone.
(() => {
  const meta = document.querySelector('meta[name="alexandria-live"]');
  if (!meta || !window.fetch || !window.DOMParser) return;
  const seconds = Math.max(2, Number(meta.content) || 5);
  const regions = () => Array.from(document.querySelectorAll('[data-live-region][id]'));
  if (regions().length === 0) return;
  let timer = 0;
  let busy = false;

  const swap = (old, fresh) => {
    // Keep <details> the reader opened or closed.
    const open = new Map(Array.from(old.querySelectorAll('details[id]')).map(d => [d.id, d.open]));
    old.replaceChildren(...Array.from(fresh.childNodes).map(n => document.importNode(n, true)));
    for (const d of old.querySelectorAll('details[id]')) if (open.has(d.id)) d.open = open.get(d.id);
  };

  const tick = async () => {
    timer = 0;
    if (busy) return;
    if (document.hidden) return; // resumes on visibilitychange
    busy = true;
    try {
      const res = await fetch(location.href, { credentials: 'same-origin', headers: { 'X-Alexandria-Live': '1' }, cache: 'no-store' });
      if (!res.ok || res.redirected) { busy = false; schedule(); return; }
      const doc = new DOMParser().parseFromString(await res.text(), 'text/html');
      for (const old of regions()) {
        const fresh = doc.getElementById(old.id);
        // A different stage (say, preparing → review) has a different
        // layout: reload once instead of patching.
        if (!fresh || !fresh.hasAttribute('data-live-region') || fresh.dataset.liveKey !== old.dataset.liveKey) {
          location.reload();
          return;
        }
        if (fresh.innerHTML !== old.innerHTML && !old.contains(document.activeElement)) swap(old, fresh);
      }
      const still = doc.querySelector('meta[name="alexandria-live"]');
      document.title = doc.title || document.title;
      busy = false;
      if (still) schedule(); // otherwise the work finished: stop polling
      else meta.remove();
    } catch (e) {
      busy = false;
      schedule();
    }
  };
  const schedule = () => { if (!timer) timer = setTimeout(tick, seconds * 1000); };
  document.addEventListener('visibilitychange', () => { if (!document.hidden && document.contains(meta)) schedule(); });
  schedule();
})();
