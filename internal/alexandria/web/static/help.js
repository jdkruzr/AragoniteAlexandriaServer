// Marks the Help section being read in the side menu. Without script the
// menu is plain links to the sections.
(() => {
  'use strict';
  const links = new Map([...document.querySelectorAll('.help-nav a[href^="#"]')].map(a => [a.getAttribute('href').slice(1), a]));
  const sections = [...links.keys()].map(id => document.getElementById(id)).filter(Boolean);
  if (!sections.length || !('IntersectionObserver' in window)) return;
  const mark = id => { for (const [key, a] of links) { if (key === id) a.setAttribute('aria-current', 'location'); else a.removeAttribute('aria-current'); } };
  const visible = new Set();
  const observer = new IntersectionObserver(entries => {
    for (const e of entries) { if (e.isIntersecting) visible.add(e.target); else visible.delete(e.target); }
    const first = sections.find(s => visible.has(s));
    if (first) mark(first.id);
  }, {rootMargin: '0px 0px -60% 0px'});
  for (const s of sections) observer.observe(s);
  mark(sections[0].id);
})();
