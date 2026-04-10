#!/usr/bin/env bash
set -euo pipefail

HTMX_VERSION="2.0.8"

refresh_htmx() {
	mkdir -p static/vendor
	curl -sL "https://unpkg.com/htmx.org@${HTMX_VERSION}/dist/htmx.min.js" \
		-o static/vendor/htmx.min.js
	echo "Downloaded htmx ${HTMX_VERSION}"
}

ensure_htmx() {
	if [ ! -f static/vendor/htmx.min.js ]; then
		refresh_htmx
	fi
}

ensure_templ() {
	if ! command -v templ >/dev/null 2>&1; then
		echo "templ not found. Install with:"
		echo "  go install github.com/a-h/templ/cmd/templ@latest"
		exit 1
	fi
}

case "${1:-build}" in
	build)
		ensure_htmx
		ensure_templ
		templ generate
		go build -o open-brain-dashboard-go .
		;;
	run)
		ensure_htmx
		ensure_templ
		templ generate
		go build -o open-brain-dashboard-go .
		./open-brain-dashboard-go serve
		;;
	test)
		ensure_templ
		templ generate
		go test ./... -v
		;;
	tidy)
		ensure_templ
		templ generate
		go mod tidy
		;;
	--refresh-htmx)
		refresh_htmx
		;;
	*)
		echo "Usage: ./build.sh [build|run|test|tidy|--refresh-htmx]"
		exit 1
		;;
esac
