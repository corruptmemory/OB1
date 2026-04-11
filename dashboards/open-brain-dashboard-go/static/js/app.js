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
