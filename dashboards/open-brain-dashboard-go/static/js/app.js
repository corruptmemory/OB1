// Ephemeral client state only. The URL is the UI state; anything that
// describes "what's on the screen" belongs in the query string, not here.

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
