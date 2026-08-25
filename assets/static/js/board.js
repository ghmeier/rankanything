// Board editing: native HTML5 drag-and-drop for reordering items (within and
// between tiers, and back to the unranked tray) and for reordering tiers.
// That is all this file is for. Adding, renaming and deleting tiers and items
// are htmx attributes on the markup, and edit-tiers mode is a checkbox that
// CSS reads (see the #edit-tiers rules in input.css), so with this script
// blocked the board loses dragging and keeps everything else.
//
// Every listener is delegated to #board rather than attached per card or row,
// so it survives a persist replacing a container's contents with the server's
// fragment — there is nothing to re-bind.
(function () {
  "use strict";

  const board = document.getElementById("board");
  if (!board) return;

  const rankingUUID = board.dataset.rankingUuid;
  const versionUUID = board.dataset.versionUuid;
  const editToggle = document.getElementById("edit-tiers");

  // CSS owns edit-tiers mode, so the only thing left to do when it flips is
  // close the menu the toggle lives in — what it reveals is the board behind
  // that menu. This waits for "change" rather than the label's click so it
  // can't unmount the checkbox mid-activation.
  if (editToggle) {
    editToggle.addEventListener("change", () => {
      const menu = editToggle.closest("details");
      if (menu) menu.open = false;
    });
  }

  function isEditing() {
    return editToggle != null && editToggle.checked;
  }

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

  // Finds the sibling a dragged element should land before, by comparing the
  // pointer's Y position against each candidate's vertical midpoint. Looking
  // only at Y is exactly right for the single-column tier-row stack, and
  // within a wrapped row of item cards it treats the whole wrap as one line —
  // an approximation these list lengths don't make anyone notice.
  function elementAfter(container, y, selector) {
    const candidates = [...container.querySelectorAll(selector)].filter(
      (el) => el !== dragging,
    );
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

  // Each persist only has to reconcile the one container whose contents
  // actually changed in the database: dragover has already moved the dragged
  // node in the live DOM, including out of the container it came from.
  //
  // The request goes through htmx rather than fetch so the response is
  // handled the way every other board mutation's is. That matters twice over:
  // htmx applies the out-of-band publish-action fragment that unrank and item
  // reorder carry, and it processes what it inserts, so the hx-delete and
  // hx-post attributes on the swapped-in buttons come back live instead of
  // inert. Passing #board as the source is what attaches the session CSRF
  // token, which htmx resolves from <body>'s hx-headers by walking up.
  function persist(target, path, values = {}) {
    htmx.ajax("POST", `/r/${rankingUUID}/v/${versionUUID}${path}`, {
      source: board,
      target,
      swap: "outerHTML",
      values,
    });
  }

  function persistItemDrop(item) {
    const zone = item.closest(".tier-dropzone");
    if (!zone) return;

    const itemID = item.dataset.itemId;
    // The tray is a dropzone with no tier of its own, so landing there means
    // the item left the ranking rather than moved within it.
    const tierID = zone.dataset.tierId;
    if (!tierID) {
      persist(`#ranking-item-${itemID}`, `/items/${itemID}/unrank`);
      return;
    }

    const itemIDs = [...zone.querySelectorAll("[data-item-id]")].map(
      (el) => el.dataset.itemId,
    );
    persist(`#tier-items-${tierID}`, `/tiers/${tierID}/items/reorder`, {
      item_id: itemIDs,
    });
  }

  function persistTierOrder() {
    const tierIDs = [...document.querySelectorAll("#tier-rows .tier-row")].map(
      (row) => row.dataset.tierId,
    );
    persist("#tier-rows", "/tiers/reorder", { tier_id: tierIDs });
  }

  // A rejected or dropped persist leaves the optimistic DOM move drifted from
  // the server's idea of the board, and a reload is the simplest correct
  // recovery. htmx fires these on whichever element made the request, so the
  // target check keeps an unrelated failure from a button inside the board
  // from reloading the page out from under it.
  for (const event of ["htmx:responseError", "htmx:sendError"]) {
    board.addEventListener(event, (e) => {
      if (e.target === board) window.location.reload();
    });
  }
})();
