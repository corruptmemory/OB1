// dashboards/open-brain-dashboard-go/static/js/app.js
//
// Tiny client-side glue for open-brain-dashboard-go.
//
// The dashboard's source of truth is the URL. This file only handles
// ephemeral interactions that genuinely need client state: theme
// preference (localStorage), sidebar collapse (localStorage), compose
// dialog mode switching (native <dialog>), and a handful of
// single-line event handlers called from inline onclick attributes.
//
// If you're adding anything here that describes "what's on the
// screen," stop and put it in the URL instead.

(function () {
	"use strict";

	// Theme: read localStorage and apply on first load, before htmx
	// touches the DOM.
	const savedTheme = localStorage.getItem("ob-theme");
	if (savedTheme === "light") {
		document.documentElement.setAttribute("data-theme", "light");
	}

	window.obToggleTheme = function () {
		const current = document.documentElement.getAttribute("data-theme");
		if (current === "light") {
			document.documentElement.removeAttribute("data-theme");
			localStorage.setItem("ob-theme", "dark");
		} else {
			document.documentElement.setAttribute("data-theme", "light");
			localStorage.setItem("ob-theme", "light");
		}
	};
})();
