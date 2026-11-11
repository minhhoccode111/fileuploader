// Skip a navigation request when the link points at the page already open: the
// content is on screen, so a round trip adds nothing.
//
// htmx has already cancelled the link's default navigation by the time
// before:request fires, so preventing this event turns the click into a no-op
// instead of an empty request/response cycle. Only GETs from anchors are
// affected; form posts and buttons still run.
document.addEventListener("htmx:before:request", (event) => {
  const ctx = event.detail.ctx;
  if (ctx.request.method !== "GET") return;
  if (ctx.sourceElement?.tagName !== "A") return;
  if (ctx.request.action !== window.location.pathname + window.location.search) return;
  event.preventDefault();
});

// Show the top bar while a request is in flight. The whole page swaps in one
// step, so without this a slow response looks like a dead click. The bar is
// hidden by htmx's own .htmx-indicator rule until the class is added.
const progressBar = document.getElementById("nav-progress");

document.addEventListener("htmx:before:request", (event) => {
  if (event.defaultPrevented) return;
  progressBar.classList.add("htmx-request");
});

document.addEventListener("htmx:finally:request", () => {
  progressBar.classList.remove("htmx-request");
});

// The drive drawer closes itself once a create, rename, upload or delete
// succeeded. The response carries the event, so an error keeps the form open
// with what the visitor typed still in it.
document.addEventListener("entrySaved", () => {
  const drawer = document.getElementById("entry-drawer");
  if (drawer) {
    drawer.checked = false;
  }
});

// Long drive names keep their head and tail and lose the middle, so the part
// that tells two similar names apart survives the cut. The full name stays in
// the title and in data-full-name, and the measurement runs again after every
// htmx swap and window resize.
const measureCanvas = document.createElement("canvas");
const measureContext = measureCanvas.getContext("2d");

function truncateMiddle(element) {
  const full = element.dataset.fullName;
  if (!full || !measureContext) return;

  element.textContent = full;

  const width = element.clientWidth;
  if (!width) return;

  const style = window.getComputedStyle(element);
  measureContext.font = `${style.fontStyle} ${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;

  if (measureContext.measureText(full).width <= width) return;

  const chars = Array.from(full);

  let low = 0;
  let high = chars.length;

  while (low < high) {
    const keep = Math.ceil((low + high) / 2);
    const head = Math.ceil(keep / 2);
    const tail = Math.floor(keep / 2);
    const candidate =
      chars.slice(0, head).join("") +
      "…" +
      (tail ? chars.slice(-tail).join("") : "");

    if (measureContext.measureText(candidate).width <= width) {
      low = keep;
    } else {
      high = keep - 1;
    }
  }

  const head = Math.ceil(low / 2);
  const tail = Math.floor(low / 2);

  element.textContent =
    chars.slice(0, head).join("") +
    "…" +
    (tail ? chars.slice(-tail).join("") : "");
}

function truncateAllMiddle(root = document) {
  for (const element of root.querySelectorAll("[data-full-name]")) {
    truncateMiddle(element);
  }
}

document.addEventListener("htmx:after:swap", () => truncateAllMiddle());
window.addEventListener("resize", () => truncateAllMiddle());
truncateAllMiddle();

function uploadFileWithProgress(form) {
  const progress = document.getElementById("upload-progress");
  const entries = document.getElementById("drive-entries");
  const errorBox = document.getElementById("upload-error");
  const submit = form.querySelector('[type="submit"]');

  if (progress) {
    progress.classList.remove("hidden");
    progress.value = 0;
  }
  if (errorBox) {
    errorBox.textContent = "";
  }
  if (submit) {
    submit.disabled = true;
  }

  const xhr = new XMLHttpRequest();
  xhr.open(form.method || "POST", form.action);
  xhr.upload.onprogress = (event) => {
    if (event.lengthComputable && progress) {
      progress.value = (event.loaded / event.total) * 100;
    }
  };
  xhr.onload = () => {
    if (progress) {
      progress.classList.add("hidden");
    }
    if (submit) {
      submit.disabled = false;
    }
    if (xhr.status >= 200 && xhr.status < 300) {
      if (entries) {
        entries.innerHTML = xhr.responseText;
        if (window.htmx) {
          window.htmx.process(entries);
        }
        truncateAllMiddle(entries);
      }
      document.dispatchEvent(new CustomEvent("entrySaved"));
    } else if (errorBox) {
      errorBox.textContent = xhr.responseText || "Upload failed.";
    }
  };
  xhr.onerror = () => {
    if (progress) {
      progress.classList.add("hidden");
    }
    if (submit) {
      submit.disabled = false;
    }
    if (errorBox) {
      errorBox.textContent = "Upload failed.";
    }
  };
  xhr.send(new FormData(form));

  return false;
}

async function downloadWithProgress(url, filename) {
  const progress = document.getElementById("download-progress");
  if (progress) {
    progress.classList.remove("hidden");
    progress.value = 0;
  }

  try {
    const response = await fetch(url);
    const contentType = response.headers.get("Content-Type") || "";
    if (
      !response.ok ||
      !response.body ||
      response.redirected ||
      contentType.includes("text/html")
    ) {
      window.location.href = url;
      return false;
    }

    const total = Number(response.headers.get("Content-Length")) || 0;
    if (progress && !total) {
      progress.removeAttribute("value");
    }

    const reader = response.body.getReader();
    const chunks = [];
    let loaded = 0;

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      chunks.push(value);
      loaded += value.length;
      if (total && progress) {
        progress.value = (loaded / total) * 100;
      }
    }

    const blob = new Blob(chunks, {
      type: contentType || "application/octet-stream",
    });
    const objectUrl = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = objectUrl;
    anchor.download = filename || "download";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(objectUrl), 5000);
  } catch (error) {
    window.location.href = url;
  } finally {
    if (progress) {
      progress.classList.add("hidden");
      progress.setAttribute("value", "0");
    }
  }

  return false;
}