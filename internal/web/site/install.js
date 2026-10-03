// The install box's two ways: each copies what it shows, and each has a tab
// per system. Picking one picks it in both, and is remembered.

// In a block, since the page's scripts share one global scope (screen.js has
// its own boxes).
{
  const boxes = [...document.querySelectorAll(".install section")];

  // systems lists the systems a tab or command is for, from its data-os.
  const systems = (el) => el.dataset.os.split(" ");

  const pick = (os) => {
    for (const box of boxes) {
      const pres = [...box.querySelectorAll("pre")];
      const current = pres.find((pre) => systems(pre).includes(os)) ?? pres[0];
      for (const pre of pres) pre.hidden = pre !== current;
      for (const t of box.querySelectorAll("[role=tab]")) {
        t.setAttribute("aria-selected", systems(t).some((s) => systems(current).includes(s)));
      }
    }
    try {
      localStorage.setItem("os", os);
    } catch {}
  };

  let os = null;
  try {
    os = localStorage.getItem("os");
  } catch {}
  os ??= /Windows/.test(navigator.userAgent) ? "windows" : /Linux/.test(navigator.userAgent) ? "debian" : "macos";

  for (const box of boxes) {
    const copy = box.querySelector(".copy");
    copy.addEventListener("click", async () => {
      const shown = [...box.querySelectorAll("pre")].find((pre) => !pre.hidden);
      await navigator.clipboard.writeText(shown.textContent.trim());
      copy.textContent = "copied";
      setTimeout(() => (copy.textContent = "copy"), 1500);
    });

    for (const t of box.querySelectorAll("[role=tab]")) {
      // A tab for several systems keeps the one already picked, if it's
      // among them: macOS / Linux doesn't turn Arch back into macOS.
      t.addEventListener("click", () => {
        if (!systems(t).includes(os)) os = systems(t)[0];
        pick(os);
      });
    }
  }
  pick(os);
}
