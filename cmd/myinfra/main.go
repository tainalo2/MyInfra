package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"myinfra/internal/api"
	"myinfra/internal/boot"
	"myinfra/internal/box"
	"myinfra/internal/library"
	"myinfra/internal/registry"
	"myinfra/internal/store"
	"myinfra/internal/tools"
	"myinfra/internal/verify"
)

var (
	version = "dev"
	image   = ""
	// postgres is the major version of the embedded PostgreSQL, empty without one.
	postgres = ""
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println(version)
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if appRun(run) {
		return
	}
	run()
}

// run serves until MyInfra stops, then starts the new executable after an update.
func run() {
	tools.PreparePath()
	// Read before an update renames the running file: the new one takes this path.
	exe, exeErr := os.Executable()
	if serve() {
		if exeErr != nil {
			fatal("redémarrage", "err", exeErr)
		}
		// Every deferred step ran: PostgreSQL is stopped, the port is free.
		if err := restart(exe); err != nil {
			fatal("redémarrage", "err", err)
		}
	}
}

// serve runs MyInfra until it is stopped. It returns true when an update
// asked for the new executable to start in its place.
func serve() bool {
	// The library leaves out what asks for a newer MyInfra than this one.
	library.Engine = version
	rt, err := boot.Start()
	if err != nil {
		fatal("démarrage", "err", err)
	}
	defer rt.Close()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = rt.DatabaseURL
	}
	var master []byte
	if rt.Local {
		master = rt.Master
	} else {
		master, err = masterKey()
		if err != nil {
			fatal("clé maîtresse", "err", err)
		}
	}
	seal, err := box.New(master)
	if err != nil {
		fatal("coffre", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()

	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		fatal("base", "err", err)
	}
	defer db.Close()

	publicURL := os.Getenv("MYINFRA_PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://127.0.0.1:8080"
	}
	if inst, err := db.Installation(ctx); err == nil && inst.PublicURL != "" {
		publicURL = inst.PublicURL
	}
	addr := listenAddr()
	app := &api.API{
		Store: db,
		Box:   seal,
		Verify: verify.Client{
			AllowTest: os.Getenv("MYINFRA_DEV") == "1",
		},
		PublicURL:    strings.TrimRight(publicURL, "/"),
		AppsDomain:   os.Getenv("MYINFRA_APPS_DOMAIN"),
		SecureCookie: strings.HasPrefix(publicURL, "https://"),
		Image:        imageOf(),
		Version:      version,
		Postgres:     postgres,
		Local:        rt.Local,
		Master:       master,
		Bootstrap:    strings.TrimSpace(os.Getenv("MYINFRA_BOOTSTRAP_TOKEN")),
		Replica:      replicaOf(ctx, db),
		RoleFixed:    os.Getenv("MYINFRA_REPLICA") == "0" || os.Getenv("MYINFRA_REPLICA") == "1",
	}
	// The versions of private images are read with the logins kept for their registries.
	registry.Credentials = app.RegistryLogin
	if app.Replica {
		slog.Info("réplique, les machines restent pilotées par l'autre copie")
	} else {
		// With two copies on one database — the pods of MyInfra in a
		// cluster — one drives the machines; the other takes over if it stops.
		go func() {
			if !db.Lead(ctx) {
				return
			}
			slog.Info("cette copie pilote les machines")
			app.RecoverDeploys(ctx)
			go app.Deliver(ctx)
			go app.Publish(ctx)
			go app.Destructions(ctx)
			go app.Templates(ctx)
			go app.Updates(ctx)
		}()
	}
	go app.Sync(ctx)
	go app.Library(ctx)
	go app.WatchRole(ctx)
	go app.WatchReach(ctx)
	go app.WatchBudget(ctx)
	go app.WarmSamples(ctx)
	server := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	app.Shutdown = server.Shutdown
	var restarting atomic.Bool
	app.Restart = func() {
		restarting.Store(true)
		stop()
	}
	if inApp() {
		// No terminal to stop it from: the Dock and the interface offer to.
		app.Stop = stop
		onQuit(stop)
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("écoute", "addr", addr, "version", version)
	if rt.Local {
		fmt.Fprintf(os.Stdout, "\nMyInfra : %s\n\n", localURL(addr))
		if os.Getenv("MYINFRA_NO_BROWSER") != "1" {
			go boot.OpenBrowser(localURL(addr))
		}
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatal("serveur", "err", err)
	}
	return restarting.Load()
}

// replicaOf says whether this copy drives no machine: MYINFRA_REPLICA=1, or
// a copy a transfer kept beside the new one. MYINFRA_REPLICA=0 makes a kept
// copy drive again.
func replicaOf(ctx context.Context, db *store.Store) bool {
	switch os.Getenv("MYINFRA_REPLICA") {
	case "1":
		return true
	case "0":
		return false
	}
	role, err := db.CopyRole(ctx)
	return err == nil && role.Replica
}

// imageOf is the published image of this copy, the one a transfer starts on
// a server. A release carries it; a copy built from source takes
// MYINFRA_IMAGE, to try a transfer with a published image.
func imageOf() string {
	if image != "" {
		return image
	}
	return strings.TrimSpace(os.Getenv("MYINFRA_IMAGE"))
}

func listenAddr() string {
	if addr := os.Getenv("MYINFRA_ADDR"); addr != "" {
		return addr
	}
	return ":8080"
}

func localURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	return "http://" + addr
}

func masterKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("MYINFRA_MASTER_KEY"))
	if raw == "" {
		if os.Getenv("MYINFRA_DEV") == "1" {
			sum := sha256.Sum256([]byte("myinfra-dev-master-key"))
			slog.Warn("clé maîtresse de développement, à ne pas utiliser pour de vrais jetons")
			return sum[:], nil
		}
		return nil, errString("MYINFRA_MASTER_KEY manquant")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, errString("MYINFRA_MASTER_KEY doit être 32 octets en base64")
	}
	return key, nil
}

// fatal logs the error and ends the process. Opened by a double click, it
// shows the error before going: the window would close, or there is none.
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	text := msg
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			text += " : " + err.Error()
		}
	}
	reportFatal(text)
	os.Exit(1)
}

type errString string

func (e errString) Error() string { return string(e) }
