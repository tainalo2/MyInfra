//go:build darwin || windows || linux

package main

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"myinfra/internal/boot"
)

// What the app does the same way on every system: its window waits for
// MyInfra, keeps MyInfra inside and sends other sites to the browser, and
// stops MyInfra with its PostgreSQL.

var (
	quitMu        sync.Mutex
	quitStop      func()
	quitRequested bool
)

const startingPage = `<!doctype html><meta charset="utf-8"><style>
:root { color-scheme: light dark; }
body { margin: 0; height: 100vh; display: grid; place-items: center; font: 15px -apple-system, system-ui, "Segoe UI", sans-serif; background: #fafaf9; color: #57534e; }
@media (prefers-color-scheme: dark) { body { background: #1c1917; color: #a8a29e; } }
.spin { width: 22px; height: 22px; margin: 0 auto 14px; border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%; animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style><div><div class="spin"></div>Démarrage de MyInfra…</div>`

// onQuit gives the app the way to stop MyInfra once it serves.
func onQuit(stop func()) {
	quitMu.Lock()
	quitStop = stop
	asked := quitRequested
	quitMu.Unlock()
	if asked {
		stop()
	}
}

// requestQuit stops MyInfra, or will as soon as it serves: stopped while
// starting, its PostgreSQL would keep running.
func requestQuit() {
	quitMu.Lock()
	stop := quitStop
	quitRequested = true
	quitMu.Unlock()
	if stop != nil {
		stop()
	}
}

// answers says whether MyInfra serves on its address.
func answers() bool {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(localURL(listenAddr()) + "/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// openMyInfra opens the browser if MyInfra answers, and says whether it did.
func openMyInfra() bool {
	if !answers() {
		return false
	}
	boot.OpenBrowser(localURL(listenAddr()))
	return true
}

// insideURL says whether url belongs in the window: MyInfra, or what it makes.
func insideURL(url string) bool {
	switch {
	case url == "",
		strings.HasPrefix(url, "about:"),
		strings.HasPrefix(url, "blob:"),
		strings.HasPrefix(url, "data:"):
		return true
	}
	return strings.HasPrefix(url, localURL(listenAddr()))
}

// attachment says whether a Content-Disposition asks to save the file.
func attachment(disposition string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(disposition)), "attachment")
}
