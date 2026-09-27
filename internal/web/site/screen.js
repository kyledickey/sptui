// Draws sptui on the page: the startup animation, then the app itself.
// Both come from the server, made by the app's own code: the intros frame
// by frame (/intros/{name}.json) and screens of sptui -demo
// (/screens/{name}.json). This paints them as a terminal would.

const COLS = 112; // the screens' size, in cells
const ROWS = 34;
const FONT = '"Commit Mono", ui-monospace, monospace';

const figure = document.querySelector("#app");
const canvas = figure.querySelector("canvas");
const caption = figure.querySelector("[data-caption]");
const buttons = [...document.querySelectorAll(".views button")];
const ctx = canvas.getContext("2d");
const still = matchMedia("(prefers-reduced-motion: reduce)").matches;

const cache = new Map();
const get = (url) => {
  if (!cache.has(url)) cache.set(url, fetch(url).then((r) => r.json()));
  return cache.get(url);
};

// Painting cells

const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
const mix = (a, b, f) => {
  if (f <= 0) return a;
  const [ar, ag, ab] = rgb(a);
  const [br, bg, bb] = rgb(b);
  const m = (x, y) => Math.round(x + (y - x) * f);
  return `#${[m(ar, br), m(ag, bg), m(ab, bb)].map((v) => v.toString(16).padStart(2, "0")).join("")}`;
};

// Block characters as rectangles in cell units (x, y, w, h), so they meet
// their neighbours without seams.
const blocks = {
  "█": [[0, 0, 1, 1]],
  "▀": [[0, 0, 1, 0.5]],
  "▄": [[0, 0.5, 1, 0.5]],
  "▌": [[0, 0, 0.5, 1]],
  "▐": [[0.5, 0, 0.5, 1]],
  "▔": [[0, 0, 1, 0.125]],
  "▘": [[0, 0, 0.5, 0.5]],
  "▝": [[0.5, 0, 0.5, 0.5]],
  "▖": [[0, 0.5, 0.5, 0.5]],
  "▗": [[0.5, 0.5, 0.5, 0.5]],
  "▚": [[0, 0, 0.5, 0.5], [0.5, 0.5, 0.5, 0.5]],
  "▞": [[0.5, 0, 0.5, 0.5], [0, 0.5, 0.5, 0.5]],
};
for (let i = 1; i <= 7; i++) {
  blocks[String.fromCharCode(0x2580 + i)] = [[0, 1 - i / 8, 1, i / 8]]; // ▁ to ▇
  blocks[String.fromCharCode(0x2590 - i)] = [[0, 0, i / 8, 1]]; // ▏ to ▉
}
const shades = { "░": 0.25, "▒": 0.5, "▓": 0.75 };

// Box lines from the centre of the cell out to the named sides.
const boxes = {
  "─": "lr",
  "│": "tb",
  "┈": "lr",
  "┊": "tb",
  "╭": "rb",
  "╮": "lb",
  "╰": "rt",
  "╯": "lt",
};

// paint draws cells, each {x, y, ch, fg, bg, bold}, on a grid cols × rows
// centred in the canvas, over bg.
function paint(cols, rows, bg, cells) {
  const cw = canvas.width / COLS;
  const chh = canvas.height / ROWS;
  const ox = Math.floor((COLS - cols) / 2);
  const oy = Math.floor((ROWS - rows) / 2);
  const X = (x) => Math.round((ox + x) * cw);
  const Y = (y) => Math.round((oy + y) * chh);
  const rect = (x, y, w, h) => ctx.fillRect(X(x), Y(y), X(x + w) - X(x), Y(y + h) - Y(y));
  const line = Math.max(1, Math.round(cw * 0.13));

  ctx.fillStyle = bg;
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  for (const c of cells) {
    if (c.bg) {
      ctx.fillStyle = c.bg;
      rect(c.x, c.y, 1, 1);
    }
    if (!c.ch || c.ch === " ") continue;
    ctx.fillStyle = c.fg;
    if (blocks[c.ch]) {
      for (const [x, y, w, h] of blocks[c.ch]) rect(c.x + x, c.y + y, w, h);
    } else if (shades[c.ch]) {
      ctx.globalAlpha = shades[c.ch];
      rect(c.x, c.y, 1, 1);
      ctx.globalAlpha = 1;
    } else if (boxes[c.ch]) {
      const sides = boxes[c.ch];
      const mx = Math.round((ox + c.x + 0.5) * cw - line / 2);
      const my = Math.round((oy + c.y + 0.5) * chh - line / 2);
      const x0 = X(c.x), x1 = X(c.x + 1), y0 = Y(c.y), y1 = Y(c.y + 1);
      if (c.ch === "┈" || c.ch === "┊") ctx.globalAlpha = 0.6;
      if (sides.includes("l")) ctx.fillRect(x0, my, mx - x0 + line, line);
      if (sides.includes("r")) ctx.fillRect(mx, my, x1 - mx, line);
      if (sides.includes("t")) ctx.fillRect(mx, y0, line, my - y0 + line);
      if (sides.includes("b")) ctx.fillRect(mx, my, line, y1 - my);
      ctx.globalAlpha = 1;
    } else {
      ctx.font = `${c.bold ? 600 : 400} ${cw / 0.6}px ${FONT}`;
      ctx.fillText(c.ch, (ox + c.x + 0.5) * cw, (oy + c.y + 0.55) * chh);
    }
  }
}

function paintScreen(s) {
  const color = (i) => (i ? s.colors[i - 1] : null);
  paint(
    s.w,
    s.h,
    s.bg,
    s.cells.map(([x, y, ch, fg, bg, bold]) => ({ x, y, ch, fg: color(fg) ?? "#e4e6eb", bg: color(bg), bold })),
  );
}

// An intro frame is pixels, two to a cell, over characters; it becomes
// cells the way the app draws it, with half blocks.
function paintIntro(a, f) {
  const color = (i) => mix(a.colors[i - 1], a.bg, f.fade);
  const cells = [];
  const covered = new Set(); // cells with pixels, which hide characters
  for (let y = 0; y < a.h; y++) {
    for (let x = 0; x < a.w; x++) {
      const top = f.px[2 * y * a.w + x];
      const bottom = f.px[(2 * y + 1) * a.w + x];
      if (!top && !bottom) continue;
      covered.add(y * a.w + x);
      cells.push(
        top
          ? { x, y, ch: "▀", fg: color(top), bg: bottom ? color(bottom) : null }
          : { x, y, ch: "▄", fg: color(bottom) },
      );
    }
  }
  for (const [x, y, ch, c] of f.cells) {
    if (!covered.has(y * a.w + x)) cells.push({ x, y, ch, fg: color(c) });
  }
  paint(a.w, a.h, a.bg, cells);
}

// What's showing

let intros = [];
let nextIntro = 0;
let view = "home";
let run = 0; // bumped to stop an intro that's playing
let redraw = () => {};

function select(name) {
  for (const b of buttons) b.setAttribute("aria-pressed", b.dataset.show === name);
}

async function showScreen(name) {
  run++;
  view = name;
  select(name);
  caption.textContent = "sptui -demo";
  const s = await get(`/screens/${name}.json`);
  if (view !== name) return;
  redraw = () => paintScreen(s);
  redraw();
}

async function playIntro() {
  const id = ++run;
  const name = intros[nextIntro++ % intros.length];
  select("intro");
  caption.textContent = `starting up · ${name}`;
  const a = await get(`/intros/${name}.json`);
  get(`/screens/${view}.json`);
  const start = performance.now();
  const tick = (now) => {
    if (id !== run) return;
    const n = Math.max(0, Math.floor((now - start) / a.frame)); // rAF can stamp a frame just before start
    if (n >= a.frames.length) {
      showScreen(view);
      return;
    }
    redraw = () => paintIntro(a, a.frames[n]);
    redraw();
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

function resize() {
  const dpr = devicePixelRatio || 1;
  canvas.width = Math.round(canvas.clientWidth * dpr);
  canvas.height = Math.round(canvas.clientHeight * dpr);
  redraw();
}

async function main() {
  const [names] = await Promise.all([get("/intros.json"), document.fonts.load(`16px ${FONT}`)]);
  intros = names;
  const show = (b) => (b.dataset.show === "intro" ? playIntro() : showScreen(b.dataset.show));
  for (const b of buttons) b.addEventListener("click", () => show(b));
  // The footer's keys, as in the app.
  addEventListener("keydown", (e) => {
    if (e.ctrlKey || e.metaKey || e.altKey || e.target.closest("input, textarea")) return;
    const b = buttons.find((b) => b.dataset.key === e.key);
    if (b) show(b);
    else if (e.key === "g") location.href = "https://github.com/kyledickey/sptui";
  });
  new ResizeObserver(resize).observe(canvas);
  resize();
  if (still) showScreen("home");
  else playIntro();
}

main();
