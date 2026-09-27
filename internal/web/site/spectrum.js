// The top bar's spectrum, as the app draws it (Model.spectrum in
// internal/tui/view.go): eight bars in eighths, stepped every 150ms, the low
// bands riding higher.

const BARS = 8;
const STEP = 150; // ms, the app's animTick

const el = document.querySelector(".spectrum");
const bars = Array.from({ length: BARS }, () => el.appendChild(document.createElement("i")));

// The app seeds each song's shape from its URI with FNV-1a.
let seed = 0x811c9dc5;
for (const c of "sptui") seed = Math.imul(seed ^ c.charCodeAt(0), 0x01000193) >>> 0;

function draw(step) {
  bars.forEach((bar, i) => {
    const phase = ((seed >>> (i * 4)) & 15) / 2.5;
    const v = 0.7 - (0.35 * i) / BARS + 0.3 * Math.sin(step * 0.9 + phase) + 0.2 * Math.sin(step * 2.3 + phase * 1.7);
    const level = Math.min(Math.max(Math.floor(v * 8), 0), 7);
    bar.style.height = `${((level + 1) / 8) * 100}%`;
  });
}

if (matchMedia("(prefers-reduced-motion: reduce)").matches) {
  draw(0);
} else {
  let last = -1;
  const tick = (now) => {
    const step = Math.floor(now / STEP);
    if (step !== last) draw((last = step));
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}
