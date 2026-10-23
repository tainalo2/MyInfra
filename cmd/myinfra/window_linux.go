//go:build linux && desktop

package main

import (
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"myinfra/internal/boot"
)

// The window of the app is WebKitGTK, loaded when the app starts: the
// package works on every distribution, and without WebKitGTK the app opens
// the browser. Loading it links the executable to the C library, so only
// the packages carry it (-tags desktop): the executable alone stays static. Links to other sites open in the browser,
// downloads ask where to save; GTK opens the file picker by itself.

const (
	gtkWindowToplevel     = 0
	gtkFileChooserSave    = 1
	gtkResponseAccept     = -3
	policyNavigation      = 0 // WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION
	policyNewWindow       = 1 // WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION
	policyResponse        = 2 // WEBKIT_POLICY_DECISION_TYPE_RESPONSE
	signalConnectDefaults = 0
)

// gtk holds the functions of GTK, WebKitGTK and libsoup the window calls.
var gtk struct {
	initCheck        func(argc, argv uintptr) bool
	setPrgname       func(name string)
	windowNew        func(kind int32) uintptr
	windowSetTitle   func(window uintptr, title string)
	windowSetIcon    func(window uintptr, name string)
	windowSetSize    func(window uintptr, width, height int32)
	containerAdd     func(container, widget uintptr)
	widgetShowAll    func(widget uintptr)
	main             func()
	mainQuit         func()
	signalConnect    func(instance uintptr, signal string, handler, data, destroy uintptr, flags int32) uint64
	idleAdd          func(fn, data uintptr) uint32
	objectUnref      func(object uintptr)
	free             func(mem *byte)
	chooserNew       func(title string, parent uintptr, action int32, accept, cancel string) uintptr
	chooserSetName   func(chooser uintptr, name string)
	chooserSetFolder func(chooser uintptr, folder string) bool
	chooserOverwrite func(chooser uintptr, confirm bool)
	chooserGetURI    func(chooser uintptr) *byte
	nativeRun        func(dialog uintptr) int32

	viewNew          func() uintptr
	viewLoadURI      func(view uintptr, uri string)
	viewLoadHTML     func(view uintptr, html string, base uintptr)
	viewLoadRequest  func(view, request uintptr)
	viewContext      func(view uintptr) uintptr
	navigationAction func(decision uintptr) uintptr
	actionRequest    func(action uintptr) uintptr
	requestURI       func(request uintptr) string
	policyIgnore     func(decision uintptr)
	policyDownload   func(decision uintptr)
	mimeSupported    func(decision uintptr) bool
	policyResponse   func(decision uintptr) uintptr
	responseHeaders  func(response uintptr) uintptr
	headersGetOne    func(headers uintptr, name string) string
	downloadSetDest  func(download uintptr, uri string)
	downloadCancel   func(download uintptr)
	downloadReplace  func(download uintptr, allowed bool)
}

var (
	window  uintptr
	webView uintptr

	// dispatched runs on the GTK thread, queued by other goroutines.
	dispatchMu sync.Mutex
	dispatched []func()

	// Callbacks are made once: purego keeps a limited number of them.
	idle      uintptr
	toSave    uintptr
	callbacks sync.Once
)

// loadGTK opens WebKitGTK and binds what the window uses. 4.1 is the
// current one; 4.0 is still the one of older distributions.
func loadGTK() bool {
	var lib uintptr
	for _, name := range []string{"libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37"} {
		if handle, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			lib = handle
			break
		}
	}
	if lib == 0 {
		return false
	}
	// WebKitGTK brings GTK, GLib and libsoup: their symbols are found through it.
	bind := func(fn any, name string) {
		purego.RegisterLibFunc(fn, lib, name)
	}
	bind(&gtk.initCheck, "gtk_init_check")
	bind(&gtk.setPrgname, "g_set_prgname")
	bind(&gtk.windowNew, "gtk_window_new")
	bind(&gtk.windowSetTitle, "gtk_window_set_title")
	bind(&gtk.windowSetIcon, "gtk_window_set_icon_name")
	bind(&gtk.windowSetSize, "gtk_window_set_default_size")
	bind(&gtk.containerAdd, "gtk_container_add")
	bind(&gtk.widgetShowAll, "gtk_widget_show_all")
	bind(&gtk.main, "gtk_main")
	bind(&gtk.mainQuit, "gtk_main_quit")
	bind(&gtk.signalConnect, "g_signal_connect_data")
	bind(&gtk.idleAdd, "g_idle_add")
	bind(&gtk.objectUnref, "g_object_unref")
	bind(&gtk.free, "g_free")
	bind(&gtk.chooserNew, "gtk_file_chooser_native_new")
	bind(&gtk.chooserSetName, "gtk_file_chooser_set_current_name")
	bind(&gtk.chooserSetFolder, "gtk_file_chooser_set_current_folder")
	bind(&gtk.chooserOverwrite, "gtk_file_chooser_set_do_overwrite_confirmation")
	bind(&gtk.chooserGetURI, "gtk_file_chooser_get_uri")
	bind(&gtk.nativeRun, "gtk_native_dialog_run")
	bind(&gtk.viewNew, "webkit_web_view_new")
	bind(&gtk.viewLoadURI, "webkit_web_view_load_uri")
	bind(&gtk.viewLoadHTML, "webkit_web_view_load_html")
	bind(&gtk.viewLoadRequest, "webkit_web_view_load_request")
	bind(&gtk.viewContext, "webkit_web_view_get_context")
	bind(&gtk.navigationAction, "webkit_navigation_policy_decision_get_navigation_action")
	bind(&gtk.actionRequest, "webkit_navigation_action_get_request")
	bind(&gtk.requestURI, "webkit_uri_request_get_uri")
	bind(&gtk.policyIgnore, "webkit_policy_decision_ignore")
	bind(&gtk.policyDownload, "webkit_policy_decision_download")
	bind(&gtk.mimeSupported, "webkit_response_policy_decision_is_mime_type_supported")
	bind(&gtk.policyResponse, "webkit_response_policy_decision_get_response")
	bind(&gtk.responseHeaders, "webkit_uri_response_get_http_headers")
	bind(&gtk.headersGetOne, "soup_message_headers_get_one")
	bind(&gtk.downloadSetDest, "webkit_download_set_destination")
	bind(&gtk.downloadCancel, "webkit_download_cancel")
	bind(&gtk.downloadReplace, "webkit_download_set_allow_overwrite")
	return true
}

// showWindow shows MyInfra until the window is closed. It returns false,
// with no window, when WebKitGTK or a display is missing.
func showWindow(string) (shown bool) {
	// A symbol missing from an old WebKitGTK panics while binding.
	defer func() {
		if recover() != nil {
			shown = false
		}
	}()
	if !loadGTK() || !gtk.initCheck(0, 0) {
		return false
	}
	// The name the desktop matches with myinfra.desktop, for its icon.
	gtk.setPrgname("myinfra")
	callbacks.Do(func() {
		idle = purego.NewCallback(runDispatched)
		toSave = purego.NewCallback(decideDestination)
	})

	window = gtk.windowNew(gtkWindowToplevel)
	gtk.windowSetTitle(window, "MyInfra")
	gtk.windowSetIcon(window, "myinfra")
	gtk.windowSetSize(window, 1280, 820)
	webView = gtk.viewNew()
	gtk.containerAdd(window, webView)
	connect(window, "destroy", purego.NewCallback(func(_, _ uintptr) uintptr {
		gtk.mainQuit()
		return 0
	}))
	connect(webView, "decide-policy", purego.NewCallback(decidePolicy))
	connect(gtk.viewContext(webView), "download-started", purego.NewCallback(func(_, download, _ uintptr) uintptr {
		connect(download, "decide-destination", toSave)
		return 0
	}))
	gtk.widgetShowAll(window)

	if answers() {
		gtk.viewLoadURI(webView, localURL(listenAddr()))
	} else {
		gtk.viewLoadHTML(webView, startingPage, 0)
		go func() {
			for !answers() {
				sleep()
			}
			dispatch(func() { gtk.viewLoadURI(webView, localURL(listenAddr())) })
		}()
	}
	gtk.main()
	return true
}

func connect(instance uintptr, signal string, callback uintptr) {
	gtk.signalConnect(instance, signal, callback, 0, 0, signalConnectDefaults)
}

// decidePolicy keeps MyInfra in the window, sends other sites to the
// browser, and saves what the window cannot show.
func decidePolicy(view, decision, kind, _ uintptr) uintptr {
	switch kind {
	case policyNavigation, policyNewWindow:
		request := gtk.actionRequest(gtk.navigationAction(decision))
		url := gtk.requestURI(request)
		if !insideURL(url) {
			boot.OpenBrowser(url)
			gtk.policyIgnore(decision)
			return 1
		}
		if kind == policyNewWindow {
			// target="_blank" and window.open stay in the window.
			gtk.viewLoadRequest(view, request)
			gtk.policyIgnore(decision)
			return 1
		}
	case policyResponse:
		disposition := gtk.headersGetOne(gtk.responseHeaders(gtk.policyResponse(decision)), "Content-Disposition")
		if attachment(disposition) || !gtk.mimeSupported(decision) {
			gtk.policyDownload(decision)
			return 1
		}
	}
	return 0
}

// decideDestination asks where to save a download, in Downloads first.
func decideDestination(download uintptr, filename *byte, _ uintptr) uintptr {
	chooser := gtk.chooserNew("Enregistrer", window, gtkFileChooserSave, "_Enregistrer", "_Annuler")
	defer gtk.objectUnref(chooser)
	gtk.chooserSetName(chooser, goChars(filename))
	gtk.chooserOverwrite(chooser, true)
	if home, err := os.UserHomeDir(); err == nil {
		gtk.chooserSetFolder(chooser, filepath.Join(home, "Downloads"))
	}
	if gtk.nativeRun(chooser) != gtkResponseAccept {
		gtk.downloadCancel(download)
		return 1
	}
	uri := gtk.chooserGetURI(chooser)
	defer gtk.free(uri)
	// The picker asked to replace it already.
	gtk.downloadReplace(download, true)
	gtk.downloadSetDest(download, goChars(uri))
	return 1
}

// dispatch runs f on the GTK thread.
func dispatch(f func()) {
	dispatchMu.Lock()
	dispatched = append(dispatched, f)
	dispatchMu.Unlock()
	gtk.idleAdd(idle, 0)
}

func runDispatched(uintptr) uintptr {
	dispatchMu.Lock()
	queue := dispatched
	dispatched = nil
	dispatchMu.Unlock()
	for _, f := range queue {
		f()
	}
	return 0 // G_SOURCE_REMOVE
}

// goChars copies a C string GTK owns.
func goChars(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}
