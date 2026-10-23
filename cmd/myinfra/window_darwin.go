//go:build darwin

package main

import (
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// MyInfra.app shows the interface in its own window through WebKit, with no
// address bar nor tabs. Closing the window leaves MyInfra running: the Dock
// opens it again. Links to other sites open in the browser.

const (
	policyCancel   = 0
	policyAllow    = 1
	policyDownload = 2 // macOS 11.3
)

var (
	window   objc.ID
	webView  objc.ID
	delegate objc.ID
	waiting  atomic.Bool
)

const startingPage = `<!doctype html><meta charset="utf-8"><style>
:root { color-scheme: light dark; }
body { margin: 0; height: 100vh; display: grid; place-items: center; font: 15px -apple-system, system-ui, sans-serif; background: #fafaf9; color: #57534e; }
@media (prefers-color-scheme: dark) { body { background: #1c1917; color: #a8a29e; } }
.spin { width: 22px; height: 22px; margin: 0 auto 14px; border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%; animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style><div><div class="spin"></div>Démarrage de MyInfra…</div>`

func loadWebKit() error {
	_, err := purego.Dlopen("/System/Library/Frameworks/WebKit.framework/WebKit", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	return err
}

// showWindow brings the window forward, made on first use. Main thread only.
func showWindow() {
	if window == 0 {
		newWindow()
		loadMyInfra()
	}
	window.Send(objc.RegisterName("makeKeyAndOrderFront:"), 0)
	objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication")).
		Send(objc.RegisterName("activateIgnoringOtherApps:"), true)
}

func newWindow() {
	webView = objc.ID(objc.GetClass("WKWebView")).Send(objc.RegisterName("new"))
	webView.Send(objc.RegisterName("setNavigationDelegate:"), delegate)
	webView.Send(objc.RegisterName("setUIDelegate:"), delegate)
	webView.Send(objc.RegisterName("setAllowsBackForwardNavigationGestures:"), true)
	if responds(webView, "setInspectable:") {
		webView.Send(objc.RegisterName("setInspectable:"), true)
	}
	controller := objc.ID(objc.GetClass("NSViewController")).Send(objc.RegisterName("new"))
	controller.Send(objc.RegisterName("setView:"), webView)
	window = objc.ID(objc.GetClass("NSWindow")).Send(objc.RegisterName("windowWithContentViewController:"), controller)
	window.Send(objc.RegisterName("setTitle:"), nsString("MyInfra"))
	window.Send(objc.RegisterName("setReleasedWhenClosed:"), false)
	// The window comes back where it was left.
	if !objc.Send[bool](window, objc.RegisterName("setFrameUsingName:"), nsString("MyInfra")) {
		window.Send(objc.RegisterName("setFrameFromString:"), nsString("0 0 1280 820"))
		window.Send(objc.RegisterName("center"))
	}
	window.Send(objc.RegisterName("setFrameAutosaveName:"), nsString("MyInfra"))
}

// loadMyInfra shows the interface, or a waiting page until MyInfra answers.
// Main thread only.
func loadMyInfra() {
	if answers() {
		url := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("URLWithString:"), nsString(localURL(listenAddr())))
		request := objc.ID(objc.GetClass("NSURLRequest")).Send(objc.RegisterName("requestWithURL:"), url)
		webView.Send(objc.RegisterName("loadRequest:"), request)
		return
	}
	webView.Send(objc.RegisterName("loadHTMLString:baseURL:"), nsString(startingPage), 0)
	if waiting.Swap(true) {
		return
	}
	go func() {
		for !answers() {
			time.Sleep(300 * time.Millisecond)
		}
		waiting.Store(false)
		delegate.Send(objc.RegisterName("performSelectorOnMainThread:withObject:waitUntilDone:"),
			objc.RegisterName("loadMyInfra:"), 0, false)
	}()
}

func answers() bool {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(localURL(listenAddr()) + "/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// windowMethods answer WebKit for the window: what stays inside, what goes
// to the browser, files to send and to save.
func windowMethods() []objc.MethodDef {
	return []objc.MethodDef{
		{
			Cmd: objc.RegisterName("loadMyInfra:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) {
				loadMyInfra()
			},
		},
		{
			Cmd: objc.RegisterName("webView:decidePolicyForNavigationAction:decisionHandler:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, action objc.ID, handler objc.Block) {
				url := requestURL(action)
				switch {
				case !inside(url):
					openURL(url)
					callBlock(handler, policyCancel)
				case responds(action, "shouldPerformDownload") && objc.Send[bool](action, objc.RegisterName("shouldPerformDownload")):
					callBlock(handler, policyDownload)
				default:
					callBlock(handler, policyAllow)
				}
			},
		},
		{
			// A file the window cannot show, or sent as one, is saved.
			Cmd: objc.RegisterName("webView:decidePolicyForNavigationResponse:decisionHandler:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, navigation objc.ID, handler objc.Block) {
				response := navigation.Send(objc.RegisterName("response"))
				attachment := false
				if responds(response, "valueForHTTPHeaderField:") {
					disposition := goString(response.Send(objc.RegisterName("valueForHTTPHeaderField:"), nsString("Content-Disposition")))
					attachment = strings.HasPrefix(strings.ToLower(disposition), "attachment")
				}
				if attachment || !objc.Send[bool](navigation, objc.RegisterName("canShowMIMEType")) {
					callBlock(handler, policyDownload)
					return
				}
				callBlock(handler, policyAllow)
			},
		},
		{
			Cmd: objc.RegisterName("webView:navigationAction:didBecomeDownload:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, _ objc.ID, download objc.ID) {
				download.Send(objc.RegisterName("setDelegate:"), self)
			},
		},
		{
			Cmd: objc.RegisterName("webView:navigationResponse:didBecomeDownload:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, _ objc.ID, download objc.ID) {
				download.Send(objc.RegisterName("setDelegate:"), self)
			},
		},
		{
			Cmd: objc.RegisterName("download:decideDestinationUsingResponse:suggestedFilename:completionHandler:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, _ objc.ID, filename objc.ID, handler objc.Block) {
				panel := objc.ID(objc.GetClass("NSSavePanel")).Send(objc.RegisterName("savePanel"))
				panel.Send(objc.RegisterName("setNameFieldStringValue:"), filename)
				if home, err := os.UserHomeDir(); err == nil {
					panel.Send(objc.RegisterName("setDirectoryURL:"), fileURL(home+"/Downloads"))
				}
				if objc.Send[int](panel, objc.RegisterName("runModal")) != 1 {
					callBlock(handler, 0)
					return
				}
				url := panel.Send(objc.RegisterName("URL"))
				// The panel asked to replace it; WebKit would not.
				_ = os.Remove(goString(url.Send(objc.RegisterName("path"))))
				callBlock(handler, uintptr(url))
			},
		},
		{
			// target="_blank" and window.open: MyInfra itself stays in the
			// window, a download among others; another site opens in the browser.
			Cmd: objc.RegisterName("webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:"),
			Fn: func(_ objc.ID, _ objc.SEL, view objc.ID, _ objc.ID, action objc.ID, _ objc.ID) objc.ID {
				url := requestURL(action)
				if inside(url) {
					view.Send(objc.RegisterName("loadRequest:"), action.Send(objc.RegisterName("request")))
				} else {
					openURL(url)
				}
				return 0
			},
		},
		{
			Cmd: objc.RegisterName("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, params objc.ID, _ objc.ID, handler objc.Block) {
				panel := objc.ID(objc.GetClass("NSOpenPanel")).Send(objc.RegisterName("openPanel"))
				panel.Send(objc.RegisterName("setCanChooseFiles:"), true)
				panel.Send(objc.RegisterName("setAllowsMultipleSelection:"),
					objc.Send[bool](params, objc.RegisterName("allowsMultipleSelection")))
				if objc.Send[int](panel, objc.RegisterName("runModal")) != 1 {
					callBlock(handler, 0)
					return
				}
				callBlock(handler, uintptr(panel.Send(objc.RegisterName("URLs"))))
			},
		},
	}
}

func requestURL(action objc.ID) objc.ID {
	return action.Send(objc.RegisterName("request")).Send(objc.RegisterName("URL"))
}

// inside says whether url belongs in the window: MyInfra, or what it makes.
func inside(url objc.ID) bool {
	if url == 0 {
		return true
	}
	switch goString(url.Send(objc.RegisterName("scheme"))) {
	case "about", "blob", "data":
		return true
	}
	return strings.HasPrefix(goString(url.Send(objc.RegisterName("absoluteString"))), localURL(listenAddr()))
}

func openURL(url objc.ID) {
	if url == 0 {
		return
	}
	objc.ID(objc.GetClass("NSWorkspace")).Send(objc.RegisterName("sharedWorkspace")).
		Send(objc.RegisterName("openURL:"), url)
}

// blockLiteral is how a block starts in memory, after the Block ABI of clang.
type blockLiteral struct {
	isa      uintptr
	flags    int32
	reserved int32
	invoke   uintptr
}

// callBlock calls a block WebKit gave, with one argument: purego only calls
// the blocks it made.
func callBlock(block objc.Block, arg uintptr) {
	literal := *(**blockLiteral)(unsafe.Pointer(&block))
	purego.SyscallN(literal.invoke, uintptr(block), arg)
}

func responds(id objc.ID, selector string) bool {
	return objc.Send[bool](id, objc.RegisterName("respondsToSelector:"), objc.RegisterName(selector))
}
