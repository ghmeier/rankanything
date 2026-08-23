// Board editing: the "edit tiers" mode toggle, and native HTML5
// drag-and-drop for reordering items (within and between tiers, and back to
// the unranked tray) and reordering tiers. No frameworks, no build step —
// just the DOM APIs a modern browser already has.
//
// Every listener below is delegated to #board rather than attached per
// card/row, so it keeps working after a drag's persist step replaces a
// container's contents with the server's fragment — no re-binding needed.
(function () {
  "use strict";

  const board = document.getElementById("board");
  if (!board) return;

  const rankingUUID = board.dataset.rankingUuid;
  const versionUUID = board.dataset.versionUuid;

  // --- CSRF -----------------------------------------------------------
  // The layout puts the session's CSRF token in <body>'s hx-headers so
  // every htmx mutation carries it automatically; a plain fetch() has to
  // read it out itself.
function csrfToken() {
    try {
      const headers = JSON.parse(document.body.getAttribute("hx-headers") || "{}");
      return headers["X-CSRF-Token"] || "";
    } catch (e) {
      return "";
    }
  }

  // --- Edit-tiers mode --------------------------------------------------
  // Purely a CSS toggle: input.css hides [data-tier-edit-only] elements
  // unless #board carries the "editing" class, so this is the only line
  // that needs to run, regardless of how the DOM churns afterward.
  const editToggle = document.getElementById("edit-tiers-toggle");
  if (editToggle) {
    editToggle.addEventListener("click", () => {
      const editing = board.classList.toggle("editing");
      editToggle.textContent = editing ? "Done editing" : "Edit tiers";
    });
  }

  function isEditing() {
    return board.classList.contains("editing");
  }

  // --- Drag state ---------------------------------------------------
  let dragging = null; // the element currently being dragged
  let dragKind = null; // "item" | "tier"

  board.addEventListener("dragstart", (e) => {
    const item = e.target.closest("[data-item-id]");
    const tierRow = e.target.closest(".tier-row");

    if (item) {
      dragging = item;
      dragKind = "item";
    } else if (tierRow && isEditing()) {
      dragging = tierRow;
      dragKind = "tier";
    } else {
      e.preventDefault();
      return;
    }

    dragging.classList.add("opacity-50");
    e.dataTransfer.effectAllowed = "move";
  });

  board.addEventListener("dragend", () => {
    if (dragging) dragging.classList.remove("opacity-50");
    dragging = null;
    dragKind = null;
  });

  // Finds the sibling a dragged element should land before, by comparing
  // the pointer's Y position against each candidate's vertical midpoint.
  // This only looks at Y, so within a wrapped row of item cards it treats
  // the row as a single line rather than accounting for horizontal
  // position too — a reasonable approximation for how small these lists
  // are, and it's exactly right for the single-column tier-row stack.
  function elementAfter(container, y, selector) {
    const candidates = [...container.querySelectorAll(selector)].filter((el) => el !== dragging);
    let after = null;
    let closestOffset = Number.NEGATIVE_INFINITY;
    for (const el of candidates) {
      const box = el.getBoundingClientRect();
      const offset = y - box.top - box.height / 2;
      if (offset < 0 && offset > closestOffset) {
        closestOffset = offset;
        after = el;
      }
    }
    return after;
  }

  board.addEventListener("dragover", (e) => {
    if (!dragging) return;

    if (dragKind === "item") {
      const zone = e.target.closest(".tier-dropzone");
      if (!zone) return;
      e.preventDefault();
      const after = elementAfter(zone, e.clientY, "[data-item-id]");
      if (after == null) zone.appendChild(dragging);
      else zone.insertBefore(dragging, after);
      return;
    }

    if (dragKind === "tier") {
      const rows = document.getElementById("tier-rows");
      if (!rows || !rows.contains(e.target)) return;
      e.preventDefault();
      const after = elementAfter(rows, e.clientY, ".tier-row");
      if (after == null) rows.appendChild(dragging);
      else rows.insertBefore(dragging, after);
    }
  });

  board.addEventListener("drop", (e) => {
    if (!dragging) return;
    e.preventDefault();

    if (dragKind === "item") persistItemDrop(dragging);
    else if (dragKind === "tier") persistTierOrder();
  });

  // --- Persisting a drop ------------------------------------------------
  // Every persist call only has to fix up the one container whose contents
  // actually changed in the database: dragover has already moved the
  // dragged node in the live DOM, including out of its old container, so
  // there's nothing left to reconcile there.

  function persistItemDrop(item) {
    const zone = item.closest(".tier-dropzone");
    if (!zone) return;

    const tierId = zone.dataset.tierId;
    if (!tierId) {
      replaceWithResponse(`ranking-item-${item.dataset.itemId}`, () =>
        postForm(`/r/${rankingUUID}/v/${versionUUID}/items/${item.dataset.itemId}/unrank`, null),
      );
      return;
    }

    const itemIds = [...zone.querySelectorAll("[data-item-id]")].map((el) => el.dataset.itemId);
    const body = new URLSearchParams();
    itemIds.forEach((id) => body.append("item_id", id));
    replaceWithResponse(`tier-items-${tierId}`, () =>
      postForm(`/r/${rankingUUID}/v/${versionUUID}/tiers/${tierId}/items/reorder`, body),
    );
  }

  function persistTierOrder() {
    const tierIds = [...document.querySelectorAll("#tier-rows .tier-row")].map((row) => row.dataset.tierId);
    const body = new URLSearchParams();
    tierIds.forEach((id) => body.append("tier_id", id));
    replaceWithResponse("tier-rows", () => postForm(`/r/${rankingUUID}/v/${versionUUID}/tiers/reorder`, body));
  }

  function postForm(url, body) {
    const headers = { "X-CSRF-Token": csrfToken() };
    if (body) headers["Content-Type"] = "application/x-www-form-urlencoded";
    return fetch(url, { method: "POST", headers, body });
  }

  async function replaceWithResponse(targetId, request) {
    let res;
    try {
      res = await request();
    } catch (e) {
      window.location.reload();
      return;
    }
    if (!res.ok) {
      // The optimistic DOM move has already drifted from the server's
      // idea of the board; reloading is the simplest correct recovery.
      window.location.reload();
      return;
    }

    const html = await res.text();
    const template = document.createElement("template");
    template.innerHTML = html.trim();
    const replacement = template.content.firstElementChild;
    const target = document.getElementById(targetId);
    if (target && replacement) target.replaceWith(replacement);

    applyOOBSwaps(template.content);
  }

  // Mirrors htmx's hx-swap-oob handling for the persist calls above, which
  // bypass htmx entirely by using fetch() directly: any element left in the
  // parsed response after the primary swap target was pulled out (unrank
  // and tier-item reorder both carry the publish-action fragment this way,
  // since either can flip the publish gate) replaces the live element with
  // the same id.
  function applyOOBSwaps(content) {
    for (const node of content.querySelectorAll("[hx-swap-oob]")) {
      const existing = document.getElementById(node.id);
      if (existing) existing.replaceWith(node);
    }
  }
})();
