// The install box's two ways: each copies what it shows, and the one built by
// hand has a tab per system, remembered.

for (const box of document.querySelectorAll(".install section")) {
  const copy = box.querySelector(".copy");
  copy.addEventListener("click", async () => {
    const shown = [...box.querySelectorAll("pre")].find((pre) => !pre.hidden);
    await navigator.clipboard.writeText(shown.textContent.trim());
    copy.textContent = "copied";
    setTimeout(() => (copy.textContent = "copy"), 1500);
  });

  const tabs = [...box.querySelectorAll("[role=tab]")];
  if (!tabs.length) continue;
  const pick = (os) => {
    const current = box.querySelector(`pre[data-os="${os}"]`) ?? box.querySelector("pre");
    for (const t of tabs) t.setAttribute("aria-selected", t.dataset.os === current.dataset.os);
    for (const pre of box.querySelectorAll("pre")) pre.hidden = pre !== current;
    try {
      localStorage.setItem("os", current.dataset.os);
    } catch {}
  };
  for (const t of tabs) t.addEventListener("click", () => pick(t.dataset.os));

  let saved = null;
  try {
    saved = localStorage.getItem("os");
  } catch {}
  pick(saved ?? (/Linux/.test(navigator.userAgent) ? "debian" : "macos"));
}
