// Package pause is the page a paused application shows instead of a proxy
// error. A small web server sends it while the application is stopped.
package pause

// Image serves the page. It is small and has a web server built in.
const Image = "busybox:1.36"

// Port is where the page answers inside the container.
const Port = 8080

// Script writes the page given in $PAGE and serves it. The page has no single
// quote, so the script can sit between single quotes in a shell.
const Script = `mkdir -p /www && printf %s "$PAGE" > /www/index.html && exec httpd -f -p 8080 -h /www`

// Page is shown in French and in English.
const Page = `<!doctype html><html lang="fr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>En pause</title></head>` +
	`<body style="margin:0;min-height:100vh;display:grid;place-items:center;background:#FAFAF9;color:#1C1917;font-family:system-ui,sans-serif">` +
	`<main style="max-width:32rem;padding:24px"><h1 style="font-size:24px;font-weight:500;margin:0 0 16px">En pause</h1>` +
	`<p style="margin:0 0 8px;line-height:1.5">Ce service est en pause. Il reviendra dès qu’il sera relancé.</p>` +
	`<p style="margin:0;color:#78716C;line-height:1.5">This service is paused. It will be back once it is started again.</p></main></body></html>`
