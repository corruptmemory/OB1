// Ephemeral client state only. The URL is the UI state; anything that
// describes "what's on the screen" belongs in the query string, not here.
// Exception: layout preferences (theme, pane widths) live in localStorage
// because they're display concerns, not navigation state.

(function () {
	"use strict";

	if (localStorage.getItem("ob-theme") === "light") {
		document.documentElement.setAttribute("data-theme", "light");
	}

	window.obToggleTheme = function () {
		if (document.documentElement.getAttribute("data-theme") === "light") {
			document.documentElement.removeAttribute("data-theme");
			localStorage.removeItem("ob-theme");
		} else {
			document.documentElement.setAttribute("data-theme", "light");
			localStorage.setItem("ob-theme", "light");
		}
	};

	var saved = localStorage.getItem("ob-detail-width");
	if (saved) {
		document.querySelector(".app").style.setProperty("--detail-width", saved);
	}
})();

// ---- resize handle between list and detail panes ----

(function () {
	"use strict";
	var handle = document.getElementById("resize-handle");
	var app = document.querySelector(".app");
	if (!handle || !app) return;

	var startX, startWidth;

	handle.addEventListener("mousedown", function (e) {
		e.preventDefault();
		var detail = document.getElementById("detail-pane");
		if (!detail) return;
		startX = e.clientX;
		startWidth = detail.offsetWidth;
		handle.classList.add("resizing");
		document.addEventListener("mousemove", onMove);
		document.addEventListener("mouseup", onUp);
	});

	function onMove(e) {
		var delta = startX - e.clientX;
		var max = window.innerWidth * 0.6;
		var w = Math.max(200, Math.min(max, startWidth + delta));
		app.style.setProperty("--detail-width", w + "px");
	}

	function onUp() {
		handle.classList.remove("resizing");
		document.removeEventListener("mousemove", onMove);
		document.removeEventListener("mouseup", onUp);
		var w = getComputedStyle(app).getPropertyValue("--detail-width");
		localStorage.setItem("ob-detail-width", w.trim());
	}
})();

// ---- v1.5 compose dialog ----

// Compose dialog mode togglers. Called from inline onclick attributes
// in compose.templ. Each one calls dialog.close() then show()/showModal()
// so the browser re-triggers its open behavior. Task 10b adds Esc and
// backdrop handling.
window.obComposeExpand = function () {
	const d = document.getElementById("compose-dialog");
	if (!d) return;
	d.classList.remove("compose-minimized");
	d.close();
	d.classList.add("compose-modal");
	d.showModal();
};

window.obComposeContract = function () {
	const d = document.getElementById("compose-dialog");
	if (!d) return;
	d.close();
	d.classList.remove("compose-modal");
	d.show();
};

window.obComposeMinimize = function () {
	const d = document.getElementById("compose-dialog");
	if (!d) return;
	d.classList.toggle("compose-minimized");
};

// htmx event listeners for the compose save chain. When /capture returns
// HX-Trigger: refresh-list, the list pane re-fetches itself. When it
// also triggers focus-thought, we load the new thought into the detail
// pane and push the URL so refresh-on-F5 shows the right state.
document.body.addEventListener("refresh-list", function () {
	if (window.htmx) {
		htmx.ajax("GET", "/partials/list" + window.location.search, "#list-pane");
	}
});

document.body.addEventListener("focus-thought", function (e) {
	if (!e.detail || !e.detail.id) return;
	const id = e.detail.id;
	if (window.htmx) {
		htmx.ajax("GET", "/partials/detail/" + id, "#detail-pane");
	}
	const url = new URL(window.location.href);
	url.searchParams.set("id", id);
	window.history.pushState({}, "", url);
});

// refresh-row is handled by the row's own hx-trigger listener (added
// in Task 9). This no-op exists to avoid a console warning if htmx
// ever bubbles the event through body.
document.body.addEventListener("refresh-row", function () {});

// Backdrop click in modal mode contracts to compact rather than
// closing, so the user's draft survives an accidental click outside
// the dialog. Only fires in modal mode; compact mode has no backdrop.
document.addEventListener("click", function (e) {
	const d = document.getElementById("compose-dialog");
	if (!d || !d.open) return;
	if (!d.classList.contains("compose-modal")) return;
	// A click directly on the dialog element (not a child) means the
	// user hit the backdrop. Children bubble their target up.
	if (e.target === d) {
		obComposeContract();
	}
});

// Esc in modal mode contracts to compact (preserves draft). Esc in
// compact mode minimizes to the title bar. Neither closes the
// dialog — close is explicit via the × button.
document.addEventListener("keydown", function (e) {
	if (e.key !== "Escape") return;
	const d = document.getElementById("compose-dialog");
	if (!d || !d.open) return;
	e.preventDefault();
	if (d.classList.contains("compose-modal")) {
		obComposeContract();
	} else {
		d.classList.toggle("compose-minimized");
	}
});

// Belt-and-suspenders: the native <dialog> cancel event fires on Esc
// in modal mode and would close the dialog by default, losing the
// draft. Our keydown handler above should preventDefault the Esc
// press first, but in case the browser dispatches cancel through a
// different code path, swallow it here too. Delegated from body
// since the dialog element is inserted into the DOM via htmx after
// this script runs.
document.body.addEventListener("cancel", function (e) {
	if (e.target && e.target.id === "compose-dialog") {
		e.preventDefault();
	}
});

// ---- sidebar filter inputs ----
// Client-side filter for topic/people lists. Prefix matches sort above
// contains matches so typing "art" shows "Artix Linux" before
// "learning-stuff." Items that don't match at all get hidden.

window.obFilterList = function (input, listId) {
	var q = input.value.toLowerCase();
	var ul = document.getElementById(listId);
	if (!ul) return;
	var items = Array.from(ul.querySelectorAll("li"));

	if (!q) {
		items.forEach(function (li) {
			li.style.display = "";
			li.style.order = "";
		});
		return;
	}

	items.forEach(function (li) {
		var label = li.querySelector(".filter-label");
		var text = label ? label.textContent.toLowerCase() : "";
		if (text.indexOf(q) === -1) {
			li.style.display = "none";
			li.style.order = "";
		} else {
			li.style.display = "";
			li.style.order = text.indexOf(q) === 0 ? "0" : "1";
		}
	});
};

// ---- v1.5 bulk delete helpers ----

window.obSelectAll = function (el) {
	document.querySelectorAll(".row-checkbox").forEach(function (c) { c.checked = el.checked; });
	obUpdateBulkCount();
};

window.obClearSelection = function () {
	document.querySelectorAll(".row-checkbox").forEach(function (c) { c.checked = false; });
	const selAll = document.getElementById("select-all");
	if (selAll) selAll.checked = false;
	obUpdateBulkCount();
	// Also exit confirm mode if we were in it.
	const toolbar = document.querySelector(".list-toolbar");
	if (toolbar) toolbar.removeAttribute("data-confirming");
};

window.obUpdateBulkCount = function () {
	const n = document.querySelectorAll(".row-checkbox:checked").length;
	document.querySelectorAll(".bulk-count").forEach(function (el) {
		el.setAttribute("data-count", String(n));
	});
};

document.addEventListener("change", function (e) {
	if (e.target && e.target.matches && e.target.matches(".row-checkbox")) {
		obUpdateBulkCount();
	}
});

// clear-detail listener: fired from handleBulkDelete when the deleted
// set included the currently-open thought. Swaps the detail pane to
// the empty state.
document.body.addEventListener("clear-detail", function () {
	if (window.htmx) {
		htmx.ajax("GET", "/partials/detail/empty", "#detail-pane");
	}
});

// ---- htmx error handling ----
// Show errors from coalesce (and other POST endpoints) inline rather
// than silently failing. htmx:responseError fires when the server
// returns a non-2xx status.
document.body.addEventListener("htmx:responseError", function (e) {
	var errEl = document.getElementById("coalesce-error");
	if (errEl && e.detail && e.detail.xhr) {
		errEl.textContent = e.detail.xhr.responseText || "Request failed";
		errEl.style.display = "inline";
		setTimeout(function () { errEl.style.display = ""; errEl.textContent = ""; }, 8000);
	}
});
