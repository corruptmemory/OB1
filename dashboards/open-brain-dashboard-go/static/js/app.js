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
