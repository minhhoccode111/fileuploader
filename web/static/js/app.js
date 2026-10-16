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
