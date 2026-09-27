// The install box: a tab per system, remembered, and a copy button.

const box = document.querySelector(".install");
const tabs = [...box.querySelectorAll("[role=tab]")];
const copy = box.querySelector(".copy");
let current;

function pick(os) {
  current = box.querySelector(`pre[data-os="${os}"]`) ?? box.querySelector("pre");
  for (const t of tabs) t.setAttribute("aria-selected", t.dataset.os === current.dataset.os);
  for (const pre of box.querySelectorAll("pre")) pre.hidden = pre !== current;
  try {
    localStorage.setItem("os", current.dataset.os);
  } catch {}
}

for (const t of tabs) t.addEventListener("click", () => pick(t.dataset.os));

copy.addEventListener("click", async () => {
  await navigator.clipboard.writeText(current.textContent.trim());
  copy.textContent = "copied";
  setTimeout(() => (copy.textContent = "copy"), 1500);
});

let saved = null;
try {
  saved = localStorage.getItem("os");
} catch {}
pick(saved ?? (/Linux/.test(navigator.userAgent) ? "debian" : "macos"));
