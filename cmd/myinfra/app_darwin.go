//go:build darwin

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"myinfra/internal/boot"
)

// Opened from the Finder, MyInfra.app sits in the Dock while it runs: a click
// on its icon opens the browser, Quit stops it with its PostgreSQL. AppKit
// wants the main thread, MyInfra serves from another one.

func init() {
	// AppKit only answers on the thread main starts on.
	runtime.LockOSThread()
}

const (
	terminateLater = 2 // NSTerminateLater: MyInfra ends once PostgreSQL is stopped.
	alertFirst     = 1000
)

var (
	// dockMenu is kept for the Dock, which asks for it on each right click.
	dockMenu objc.ID

	quitMu        sync.Mutex
	quitStop      func()
	quitRequested bool
)

func inApp() bool {
	exe, err := os.Executable()
	return err == nil && strings.Contains(exe, ".app/Contents/MacOS/")
}

// appRun runs MyInfra as an app when it is one, and returns false otherwise.
// run serves in the background while the main thread answers the Dock; the
// process ends with it.
func appRun(run func()) bool {
	if !inApp() {
		return false
	}
	for _, arg := range os.Args[1:] {
		if arg == "--no-browser" {
			os.Setenv("MYINFRA_NO_BROWSER", "1")
		}
	}
	home, err := boot.Home()
	if err != nil {
		alert("MyInfra ne démarre pas", err.Error())
		return true
	}
	appPath()
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		alert("MyInfra ne démarre pas", err.Error())
		return true
	}
	nsApp := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	nsApp.Send(objc.RegisterName("setActivationPolicy:"), 0) // NSApplicationActivationPolicyRegular
	delegate, err := appDelegate()
	if err != nil {
		alert("MyInfra ne démarre pas", err.Error())
		return true
	}
	nsApp.Send(objc.RegisterName("setDelegate:"), delegate)
	nsApp.Send(objc.RegisterName("setMainMenu:"), mainMenu(delegate))
	dockMenu = newMenu(menuItem("Ouvrir MyInfra", "openMyInfra:", "", delegate))

	if offerMove(nsApp, home) {
		return true
	}
	// Another MyInfra, from a terminal or another copy of the app, already
	// answers: two would share the same PostgreSQL folder.
	if openMyInfra() {
		return true
	}
	appLog(home)
	go func() {
		run()
		os.Exit(0)
	}()
	nsApp.Send(objc.RegisterName("run"))
	return true
}

func appDelegate() (objc.ID, error) {
	class, err := objc.RegisterClass("MyInfraDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{
			// A click on the Dock icon, or a new double click.
			Cmd: objc.RegisterName("applicationShouldHandleReopen:hasVisibleWindows:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, _ bool) bool {
				go openMyInfra()
				return false
			},
		},
		{
			Cmd: objc.RegisterName("applicationShouldTerminate:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) uint {
				requestQuit()
				return terminateLater
			},
		},
		{
			Cmd: objc.RegisterName("applicationDockMenu:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) objc.ID {
				return dockMenu
			},
		},
		{
			Cmd: objc.RegisterName("openMyInfra:"),
			Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) {
				go openMyInfra()
			},
		},
	})
	if err != nil {
		return 0, err
	}
	return objc.ID(class).Send(objc.RegisterName("new")), nil
}

// mainMenu is the menu of the app once it is in front, for ⌘Q.
func mainMenu(delegate objc.ID) objc.ID {
	appMenu := newMenu(
		menuItem("Ouvrir MyInfra", "openMyInfra:", "o", delegate),
		objc.ID(objc.GetClass("NSMenuItem")).Send(objc.RegisterName("separatorItem")),
		menuItem("Quitter MyInfra", "terminate:", "q", 0),
	)
	root := objc.ID(objc.GetClass("NSMenuItem")).Send(objc.RegisterName("new"))
	root.Send(objc.RegisterName("setSubmenu:"), appMenu)
	return newMenu(root)
}

func newMenu(items ...objc.ID) objc.ID {
	menu := objc.ID(objc.GetClass("NSMenu")).Send(objc.RegisterName("new"))
	for _, item := range items {
		menu.Send(objc.RegisterName("addItem:"), item)
	}
	return menu
}

func menuItem(title, action, key string, target objc.ID) objc.ID {
	item := objc.ID(objc.GetClass("NSMenuItem")).Send(objc.RegisterName("alloc")).Send(
		objc.RegisterName("initWithTitle:action:keyEquivalent:"),
		nsString(title), objc.RegisterName(action), nsString(key))
	if target != 0 {
		item.Send(objc.RegisterName("setTarget:"), target)
	}
	return item
}

// openMyInfra opens the browser if MyInfra answers, and says whether it did.
// Still starting, MyInfra opens the browser itself.
func openMyInfra() bool {
	url := localURL(listenAddr())
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url + "/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	boot.OpenBrowser(url)
	return true
}

// onQuit gives the Dock the way to stop MyInfra once it serves.
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

// offerMove proposes to move the app into Applications, where it stays and
// updates itself. It returns true when the moved copy takes over.
func offerMove(nsApp objc.ID, home string) bool {
	bundle, ok := bundlePath()
	if !ok || inApplications(bundle) {
		return false
	}
	never := filepath.Join(home, "no-move")
	if _, err := os.Stat(never); err == nil {
		return false
	}
	alertView := objc.ID(objc.GetClass("NSAlert")).Send(objc.RegisterName("new"))
	alertView.Send(objc.RegisterName("setMessageText:"), nsString("Déplacer MyInfra dans Applications ?"))
	alertView.Send(objc.RegisterName("setInformativeText:"), nsString("MyInfra s'y retrouve dans le Launchpad et Spotlight, et peut se mettre à jour."))
	alertView.Send(objc.RegisterName("addButtonWithTitle:"), nsString("Déplacer"))
	alertView.Send(objc.RegisterName("addButtonWithTitle:"), nsString("Pas maintenant"))
	alertView.Send(objc.RegisterName("setShowsSuppressionButton:"), true)
	alertView.Send(objc.RegisterName("suppressionButton")).Send(objc.RegisterName("setTitle:"), nsString("Ne plus demander"))
	nsApp.Send(objc.RegisterName("activateIgnoringOtherApps:"), true)
	answer := objc.Send[int](alertView, objc.RegisterName("runModal"))
	if answer != alertFirst {
		if objc.Send[int](alertView.Send(objc.RegisterName("suppressionButton")), objc.RegisterName("state")) == 1 {
			_ = os.WriteFile(never, nil, 0o600)
		}
		return false
	}
	dest, err := moveApp(bundle)
	if err == nil {
		err = relaunchApp(dest)
	}
	if err != nil {
		alert("MyInfra ne se déplace pas", err.Error())
		return false
	}
	return true
}

// moveApp copies the app into Applications, then puts the downloaded one in
// the Trash. It returns where the app now is.
func moveApp(bundle string) (string, error) {
	dir := "/Applications"
	if syscall.Access(dir, 2) != nil { // W_OK
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, "Applications")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	dest := filepath.Join(dir, "MyInfra.app")
	if _, err := os.Stat(dest); err == nil && !trash(dest) {
		if err := os.RemoveAll(dest); err != nil {
			return "", err
		}
	}
	if out, err := exec.Command("ditto", bundle, dest).CombinedOutput(); err != nil {
		return "", errorText(out, err)
	}
	// Allowed once already: macOS would otherwise run the copy from a read-only place again.
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", dest).Run()
	if original := originalPath(bundle); original != "" && original != dest {
		trash(original)
	}
	return dest, nil
}

// relaunchApp opens the app at bundle once this process has ended: for
// macOS, the app is this process until then, and would only come forward.
func relaunchApp(bundle string, args ...string) error {
	script := `while kill -0 "$1" 2>/dev/null; do sleep 0.2; done; shift; exec open "$@"`
	cmd := exec.Command("/bin/sh", append([]string{"-c", script, "sh", strconv.Itoa(os.Getpid()), bundle}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// restartApp opens the updated app in place of this process.
func restartApp() error {
	bundle, ok := bundlePath()
	if !ok {
		return os.ErrNotExist
	}
	return relaunchApp(bundle, "--args", "--no-browser")
}

func bundlePath() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	return bundle, strings.HasSuffix(bundle, ".app")
}

func inApplications(bundle string) bool {
	if strings.HasPrefix(bundle, "/Applications/") {
		return true
	}
	home, err := os.UserHomeDir()
	return err == nil && strings.HasPrefix(bundle, filepath.Join(home, "Applications")+"/")
}

// originalPath is where the app was downloaded, when macOS runs it from a
// read-only copy; empty if unknown.
func originalPath(bundle string) string {
	if !strings.Contains(bundle, "/AppTranslocation/") {
		return bundle
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW)
	if err != nil {
		return ""
	}
	sym, err := purego.Dlsym(lib, "SecTranslocateCreateOriginalPathForURL")
	if err != nil {
		return ""
	}
	var original func(url objc.ID, errOut uintptr) objc.ID
	purego.RegisterFunc(&original, sym)
	url := original(fileURL(bundle), 0)
	if url == 0 {
		return ""
	}
	return goString(url.Send(objc.RegisterName("path")))
}

func trash(path string) bool {
	manager := objc.ID(objc.GetClass("NSFileManager")).Send(objc.RegisterName("defaultManager"))
	return objc.Send[bool](manager, objc.RegisterName("trashItemAtURL:resultingItemURL:error:"), fileURL(path), 0, 0)
}

// appLog keeps the output of the app, which has no terminal, in myinfra.log.
func appLog(home string) {
	path := filepath.Join(home, "myinfra.log")
	_ = os.Rename(path, path+".1")
	logs, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_ = syscall.Dup2(int(logs.Fd()), 1)
	_ = syscall.Dup2(int(logs.Fd()), 2)
}

// appPath adds the folders of Homebrew: an app opened from the Finder only
// gets the system ones.
func appPath() {
	path := os.Getenv("PATH")
	for _, dir := range []string{"/usr/local/bin", "/opt/homebrew/bin"} {
		if !strings.Contains(path, dir) {
			path += string(os.PathListSeparator) + dir
		}
	}
	os.Setenv("PATH", path)
}

// reportFatal shows the error of the app: it has no terminal.
func reportFatal(text string) {
	if !inApp() {
		return
	}
	if home, err := boot.Home(); err == nil {
		text += "\n\nJournal : " + filepath.Join(home, "myinfra.log")
	}
	alert("MyInfra s'est arrêté", text)
}

func alert(title, message string) {
	script := "display alert " + appleString(title) + " message " + appleString(message) + " as critical"
	_ = exec.Command("osascript", "-e", script).Run()
}

func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func errorText(out []byte, err error) error {
	if text := strings.TrimSpace(string(out)); text != "" {
		return fmt.Errorf("%s : %w", text, err)
	}
	return err
}

func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), unsafe.Pointer(&b[0]))
	runtime.KeepAlive(b)
	return str
}

func goString(str objc.ID) string {
	p := objc.Send[*byte](str, objc.RegisterName("UTF8String"))
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

func fileURL(path string) objc.ID {
	return objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("fileURLWithPath:"), nsString(path))
}
