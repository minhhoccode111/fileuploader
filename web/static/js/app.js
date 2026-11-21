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

// One file at a time leaves the page. activeUpload is the session an abort
// beacon can still end, and uploadRun numbers the flow that owns it: closing
// the drawer bumps the number, so the loop stops instead of waking up on a
// folder the visitor has already left.
let activeUpload = null;
let uploadRun = 0;

// A part is sent again this many times after a dropped connection or a 5xx.
// 524 is the edge giving up on the whole request, so repeating it only repeats
// the wait.
const uploadPartRetries = 3;
const uploadPartTimeout = 120000;
const uploadPartRetryBaseDelay = 1000;
const uploadPartRetryMaxDelay = 30000;

// A toast stays up long enough to read one sentence.
const uploadToastMs = 4000;

let toastTimer = null;

// uploadFileWithProgress takes over the drawer form: the browser would send
// the file as a single request, which the server no longer accepts. The start
// answer plans the parts, and the file leaves one part at a time, so a broken
// connection costs a part rather than the file.
function uploadFileWithProgress(form) {
  const file = form.elements.file?.files?.[0];
  const folder = form.elements.folder?.value ?? "";
  const name = (form.elements.name?.value ?? "").trim();
  const pageFolder =
    document.getElementById("drive-entries")?.dataset.folderId ?? "";
  const progress = document.getElementById("upload-progress");
  const submit = form.querySelector('[type="submit"]');

  showUploadError("");

  if (!file) {
    showUploadError("Choose a file to upload.");
    return false;
  }

  if (activeUpload) {
    showUploadError("Another upload is still running.");
    return false;
  }

  if (progress) {
    progress.classList.remove("hidden");
    progress.value = 0;
  }

  if (submit) {
    submit.disabled = true;
  }

  const run = (uploadRun += 1);

  void uploadInParts(file, folder, name, pageFolder, run)
    .catch((error) => failUpload(error, run))
    .finally(() => finishUpload(run, submit));

  return false;
}

// uploadInParts is the flow the form starts: open the session, send every part
// the plan names, then join them. The session is recorded before the first
// part, so closing the drawer can end it with a beacon.
async function uploadInParts(file, folder, name, pageFolder, run) {
  const plan = await startUpload(file, folder, name);

  if (run !== uploadRun) {
    abortUpload(plan.uploadID);
    return;
  }

  activeUpload = { id: plan.uploadID, run, pageFolder };

  let completed = 0;

  const report = (loaded) => {
    const size = file.size || 1;
    setUploadProgress(((completed + loaded) / size) * 100);
  };

  for (let number = 1; number <= plan.partCount; number += 1) {
    if (run !== uploadRun) return;

    const offset = (number - 1) * plan.partSize;
    const blob = file.slice(offset, offset + plan.partSize);

    await uploadPart(
      "/htmx/uploads/" + plan.uploadID + "/parts/" + number,
      blob,
      report,
    );

    completed += blob.size;
    report(0);
  }

  if (run !== uploadRun) return;

  await completeUpload(plan.uploadID, run);
}

// startUpload asks for the session and for the part plan. The size travels up
// front, because the server reserves it against the quota before a byte
// arrives.
async function startUpload(file, folder, name) {
  const response = await fetch("/htmx/uploads", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      folder: folder,
      name: name,
      size: file.size,
      content_type: file.type,
    }),
  });

  if (!response.ok) {
    throw new Error(await uploadMessage(response));
  }

  // A refused session answers with the sign-in page, which is HTML with a 200
  // status. Reading it as JSON would report a parser error instead.
  const type = response.headers.get("Content-Type") || "";

  if (response.redirected || !type.includes("application/json")) {
    throw new Error("The upload could not start. Sign in again and retry.");
  }

  const plan = await response.json();
  const partSize = Number(plan.part_size);
  const partCount = Number(plan.part_count);
  const expectedCount =
    file.size === 0 ? 0 : Math.ceil(file.size / partSize);

  if (
    typeof plan.upload_id !== "string" ||
    plan.upload_id === "" ||
    !Number.isFinite(partSize) ||
    partSize <= 0 ||
    !Number.isInteger(partCount) ||
    partCount < 0 ||
    partCount !== expectedCount
  ) {
    throw new Error("The upload plan is invalid. Sign in again and retry.");
  }

  return {
    uploadID: plan.upload_id,
    partSize: partSize,
    partCount: partCount,
  };
}

// uploadPart sends one part with XHR, because only XHR reports how much of the
// body has left the browser. Each attempt reports from zero, so a retry does
// not add the lost bytes to the progress of the new attempt.
function uploadPart(url, blob, onProgress) {
  return new Promise((resolve, reject) => {
    let attempt = 0;

    const retryLater = () => {
      attempt += 1;
      window.setTimeout(send, retryDelay(attempt));
    };

    const send = () => {
      onProgress(0);

      const part = new XMLHttpRequest();

      part.open("PUT", url);
      part.setRequestHeader("Content-Type", "application/octet-stream");
      part.timeout = uploadPartTimeout;

      part.upload.addEventListener("progress", (event) => {
        if (event.lengthComputable) {
          onProgress(event.loaded);
        }
      });

      part.addEventListener("load", () => {
        if (part.status >= 200 && part.status < 300) {
          resolve();
          return;
        }

        if (retryPart(part.status) && attempt < uploadPartRetries) {
          retryLater();
          return;
        }

        reject(new Error(partMessage(part)));
      });

      part.addEventListener("error", () => {
        if (attempt < uploadPartRetries) {
          retryLater();
          return;
        }

        reject(new Error("The connection dropped during the upload."));
      });

      part.addEventListener("timeout", () => {
        if (attempt < uploadPartRetries) {
          retryLater();
          return;
        }

        reject(new Error("The connection stalled during the upload."));
      });

      part.send(blob);
    };

    send();
  });
}

// retryDelay spaces retries out and adds jitter, so a struggling edge is not
// hit by every client at the same moment.
function retryDelay(attempt) {
  const delay = Math.min(
    uploadPartRetryMaxDelay,
    uploadPartRetryBaseDelay * 2 ** (attempt - 1),
  );

  return delay + Math.random() * 500;
}

// completeUpload joins the parts. The answer carries the folder the file
// landed in, so a page that still shows that folder swaps the new row in. Any
// other page gets the toast: a late answer never rewrites rows the visitor is
// not looking at.
async function completeUpload(id, run) {
  const pageFolder = activeUpload?.pageFolder ?? "";

  const response = await fetch("/htmx/uploads/" + id + "/complete", {
    method: "POST",
  });

  if (!response.ok) {
    throw new Error(await uploadMessage(response));
  }

  // Only a joined file names a folder. A refused session answers with the
  // sign-in page instead, and that page must not pass for a finished upload.
  const landedFolder = response.headers.get("X-Upload-Folder") ?? "";

  if (!landedFolder) {
    throw new Error("The upload could not finish. Sign in again and retry.");
  }

  const html = await response.text();

  if (run !== uploadRun) return;

  const entries = document.getElementById("drive-entries");
  const stillHere =
    pageFolder !== "" &&
    entries?.dataset.folderId === pageFolder &&
    entries.dataset.folderId === landedFolder;

  if (entries && stillHere) {
    entries.innerHTML = html;

    if (window.htmx) {
      window.htmx.process(entries);
    }

    truncateAllMiddle(entries);
  } else {
    showToast("Upload complete");
  }

  // The drawer closes on entrySaved, and that close must not send an abort for
  // an upload that already finished.
  activeUpload = null;
  document.dispatchEvent(new CustomEvent("entrySaved"));
}

// failUpload ends the session of a flow that cannot continue, tells the
// visitor why, and leaves the form usable. A flow that was already superseded
// stays quiet: its message would describe an upload nobody is waiting for.
function failUpload(error, run) {
  if (run !== uploadRun) return;

  if (activeUpload && activeUpload.run === run) {
    abortUpload(activeUpload.id);
    activeUpload = null;
  }

  showUploadError(error?.message || "Upload failed.");
}

// finishUpload puts the drawer back in its resting state, unless another flow
// has taken over the page in the meantime.
function finishUpload(run, submit) {
  if (run !== uploadRun) return;

  if (submit) {
    submit.disabled = false;
  }

  const progress = document.getElementById("upload-progress");

  if (progress) {
    progress.classList.add("hidden");
    progress.value = 0;
  }
}

// abortActiveUpload ends the open session when the drawer closes or the page
// goes away. A beacon is the only request the browser still delivers on unload.
function abortActiveUpload() {
  uploadRun += 1;

  if (!activeUpload) return;

  const { id } = activeUpload;

  abortUpload(id);
  activeUpload = null;
}

function abortUpload(id) {
  if (!id) return;

  navigator.sendBeacon("/htmx/uploads/" + id + "/abort");
}

// retryPart reports whether a part is worth sending again. A 4xx names a
// request the server will refuse again, except 429 which asks the client to
// slow down; 524 is the edge timing out.
function retryPart(status) {
  return status === 429 || (status >= 500 && status !== 524);
}

function partMessage(part) {
  return messageFrom(part.responseText, "That part of the upload failed.");
}

async function uploadMessage(response) {
  return messageFrom(await response.text(), "Upload failed.");
}

// messageFrom keeps a plain-text answer as the message and ignores anything
// that looks like a page, because a swapped page is not a sentence.
function messageFrom(text, fallback) {
  const trimmed = (text || "").trim();

  if (!trimmed || trimmed.startsWith("<")) return fallback;

  return trimmed;
}

function setUploadProgress(percent) {
  const progress = document.getElementById("upload-progress");

  if (!progress) return;

  progress.classList.remove("hidden");
  progress.value = Math.min(100, Math.max(0, percent));
}

function showUploadError(message) {
  const errorBox = document.getElementById("upload-error");

  if (errorBox) {
    errorBox.textContent = message;
    return;
  }

  if (message) {
    showToast(message, "error");
  }
}

// showToast speaks after the drawer is gone: the upload finished on another
// folder, or a failure happened on a page that no longer carries an error box.
function showToast(message, tone = "success") {
  const container = document.getElementById("app-toast");

  if (!container) return;

  const alert = document.createElement("div");
  alert.className =
    tone === "error" ? "alert alert-error" : "alert alert-success";
  alert.setAttribute("role", "status");
  alert.textContent = message;

  container.replaceChildren(alert);
  container.classList.remove("hidden");

  if (toastTimer) {
    window.clearTimeout(toastTimer);
  }

  toastTimer = window.setTimeout(() => {
    container.classList.add("hidden");
    container.replaceChildren();
    toastTimer = null;
  }, uploadToastMs);
}

// Closing the drawer cancels the upload: the visitor has stopped watching, and
// a file nobody waits for should not keep the connection busy. The listener
// sits on the document because a folder swap replaces the drawer element.
document.addEventListener("change", (event) => {
  if (event.target.id !== "entry-drawer") return;
  if (event.target.checked) return;

  abortActiveUpload();
});

window.addEventListener("pagehide", abortActiveUpload);

// Copy a share link to the clipboard. The link in the markup is relative, so
// the absolute URL is built from the page's origin — that is what a recipient
// needs. The button confirms the copy for a moment, then returns to its label.
async function copyShareLink(button) {
  const link = button.dataset.shareLink;
  if (!link) return;

  const url = window.location.origin + link;

  try {
    await navigator.clipboard.writeText(url);
  } catch (error) {
    const textarea = document.createElement("textarea");
    textarea.value = url;
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.select();
    document.execCommand("copy");
    textarea.remove();
  }

  const label = button.querySelector("[data-share-label]");
  if (!label) return;

  label.textContent = "Copied!";
  button.disabled = true;

  setTimeout(() => {
    label.textContent = "Copy link";
    button.disabled = false;
  }, 1500);
}

// A download above this size goes straight to the browser: buffering it as a
// Blob would hold the whole file in memory first. A folder archive carries no
// size at all, because it is written as it streams, so it takes the same path
// and the browser shows its own real progress instead of a made-up bar.
const directDownloadBytes = 32 * 1024 * 1024;

async function downloadWithProgress(url, filename, size) {
  const bytes = Number(size) || 0;
  if (!bytes || bytes > directDownloadBytes) {
    window.location.href = url;
    return false;
  }

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