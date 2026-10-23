//go:build windows

package main

import (
	"path/filepath"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"

	"myinfra/internal/boot"
)

// The window of the app is WebView2, the engine of Edge, shipped with
// Windows 10 and 11. Links to other sites open in the browser; WebView2
// itself opens the file picker and saves downloads.

// iconID is the icon the resources of the executable carry.
const iconID = 1

// outside sends to Go the links and window.open calls that leave MyInfra,
// and keeps MyInfra in the window when a link asks for a new one.
const outside = `(() => {
  const parse = (url) => { try { return new URL(url, location.href) } catch { return null } };
  const leave = (url) => {
    const u = parse(url);
    if (!u || u.origin === location.origin || !/^(https?|mailto):$/.test(u.protocol)) return false;
    window.myinfraOpen(u.href);
    return true;
  };
  window.addEventListener("click", (e) => {
    const a = e.target instanceof Element && e.target.closest("a[href]");
    if (!a || e.defaultPrevented || a.hasAttribute("download")) return;
    if (leave(a.href)) {
      e.preventDefault();
    } else if (a.target === "_blank") {
      e.preventDefault();
      location.href = a.href;
    }
  });
  const open = window.open;
  window.open = (url, ...rest) => {
    if (url && leave(url)) return null;
    return open.call(window, url, ...rest);
  };
})()`

// showWindow shows MyInfra until the window is closed. It returns false,
// with no window, when WebView2 is missing.
func showWindow(home string) bool {
	if _, err := webviewloader.GetInstalledVersion(); err != nil {
		return false
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(home, "webview"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "MyInfra",
			Width:  1280,
			Height: 820,
			IconId: iconID,
			Center: true,
		},
	})
	if w == nil {
		return false
	}
	_ = w.Bind("myinfraOpen", func(url string) {
		if !insideURL(url) {
			boot.OpenBrowser(url)
		}
	})
	w.Init(outside)
	if answers() {
		w.Navigate(localURL(listenAddr()))
	} else {
		w.SetHtml(startingPage)
		go func() {
			for !answers() {
				sleep()
			}
			w.Dispatch(func() { w.Navigate(localURL(listenAddr())) })
		}()
	}
	w.Run()
	return true
}
