// Offers the browser's time zone when the saved one differs. The setting
// works without this script; it only saves looking the zone up.
(() => {
  const select = document.getElementById("time-zone");
  const hint = document.getElementById("tz-hint");
  let zone = "";
  try { zone = Intl.DateTimeFormat().resolvedOptions().timeZone || ""; } catch (e) { return; }
  if (!select || !hint || !zone || select.value === zone) return;
  if (![...select.options].some((o) => o.value === zone)) return;
  hint.querySelector("[data-tz]").textContent = zone;
  hint.querySelector("[data-tz-use]").addEventListener("click", () => { select.value = zone; hint.hidden = true; });
  hint.hidden = false;
})();
