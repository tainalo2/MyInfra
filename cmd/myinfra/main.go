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
	tools.PreparePath()
	// Read before an update renames the running file: the new one takes this path.
	exe, exeErr := os.Executable()
	if serve() {
		if exeErr != nil {
			slog.Error("redémarrage", "err", exeErr)
			os.Exit(1)
		}
		// Every deferred step ran: PostgreSQL is stopped, the port is free.
		if err := restart(exe); err != nil {
			slog.Error("redémarrage", "err", err)
			os.Exit(1)
		}
	}
}

// serve runs MyInfra until it is stopped. It returns true when an update
// asked for the new executable to start in its place.
func serve() bool {
	rt, err := boot.Start()
	if err != nil {
		slog.Error("démarrage", "err", err)
		os.Exit(1)
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
			slog.Error("clé maîtresse", "err", err)
			os.Exit(1)
		}
	}
	seal, err := box.New(master)
	if err != nil {
		slog.Error("coffre", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()

	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		slog.Error("base", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	publicURL := os.Getenv("MYINFRA_PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://127.0.0.1:8080"
	}
	if inst, err := db.Installation(ctx); err == nil && inst.PublicURL != "" {
		publicURL = inst.PublicURL
	}
	addr := os.Getenv("MYINFRA_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	app := &api.API{
		Store: db,
		Box:   seal,
		Verify: verify.Client{
			AllowTest: os.Getenv("MYINFRA_DEV") == "1",
		},
		PublicURL:    strings.TrimRight(publicURL, "/"),
		AppsDomain:   os.Getenv("MYINFRA_APPS_DOMAIN"),
		SecureCookie: strings.HasPrefix(publicURL, "https://"),
		Image:        image,
		Version:      version,
		Postgres:     postgres,
		Local:        rt.Local,
		Master:       master,
		Bootstrap:    strings.TrimSpace(os.Getenv("MYINFRA_BOOTSTRAP_TOKEN")),
		Replica:      os.Getenv("MYINFRA_REPLICA") == "1",
	}
	// The versions of private images are read with the logins kept for their registries.
	registry.Credentials = app.RegistryLogin
	if app.Replica {
		slog.Info("réplique, les machines restent pilotées par l'autre copie")
	} else {
		app.RecoverDeploys(ctx)
		go app.Deliver(ctx)
		go app.Publish(ctx)
		go app.Destructions(ctx)
		go app.Templates(ctx)
		go app.Updates(ctx)
	}
	go app.Sync(ctx)
	go app.WarmSamples(ctx)
	server := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	app.Shutdown = server.Shutdown
	var restarting atomic.Bool
	app.Restart = func() {
		restarting.Store(true)
		stop()
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
		slog.Error("serveur", "err", err)
		os.Exit(1)
	}
	return restarting.Load()
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

type errString string

func (e errString) Error() string { return string(e) }
