package boot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"myinfra/internal/proc"
)

func startPostgres(home string) (string, func(), error) {
	archive := postgresArchive()
	if len(archive) < 1024 {
		return "", nil, errString("Ce binaire n'embarque pas PostgreSQL. Définissez DATABASE_URL.")
	}
	root := filepath.Join(home, "pg")
	data := filepath.Join(home, "pgdata")
	// The first start unpacks and initializes PostgreSQL: it takes a while.
	slog.Info("démarrage de PostgreSQL", "dossier", home)
	fresh, err := extractPostgres(archive, root)
	if err != nil {
		return "", nil, err
	}
	bin := filepath.Join(root, "bin", exe("postgres"))
	if _, err := os.Stat(bin); err != nil {
		return "", nil, errString("PostgreSQL embarqué est incomplet.")
	}
	if _, err := os.Stat(filepath.Join(data, "PG_VERSION")); err != nil {
		// initdb creates pgdata itself. On Windows it runs with the
		// administrator rights dropped: a folder made by an elevated MyInfra
		// belongs to the administrators, and initdb could not restrict it.
		if err := run(root, filepath.Join(root, "bin", exe("initdb")), "--username=myinfra", "--auth=trust", "--encoding=UTF8", "--locale=C", "--no-instructions", "-D", data); err != nil {
			return "", nil, err
		}
	}
	ctl := filepath.Join(root, "bin", exe("pg_ctl"))
	if _, ok := runningPort(data); ok && fresh {
		// A server left by the previous programs, after an update: it is
		// stopped and started again from the new ones.
		_ = run(root, ctl, "-D", data, "-w", "-t", "60", "-m", "fast", "stop")
	}
	if port, ok := runningPort(data); ok {
		url, err := databaseURL(port)
		if err != nil {
			return "", nil, err
		}
		if err := ensureDatabase(url); err != nil {
			return "", nil, err
		}
		return url, func() {}, nil
	}
	port, err := freePort()
	if err != nil {
		return "", nil, err
	}
	if err := requireLoopback("127.0.0.1"); err != nil {
		return "", nil, err
	}
	logPath := filepath.Join(home, "pg.log")
	opt := "-c listen_addresses=127.0.0.1 -c port=" + strconv.Itoa(port)
	if err := run(root, ctl, "-D", data, "-l", logPath, "-w", "-t", "60", "-o", opt, "start"); err != nil {
		return "", nil, fmt.Errorf("PostgreSQL n'a pas démarré. %s", tail(logPath))
	}
	url, err := databaseURL(port)
	if err != nil {
		_ = run(root, ctl, "-D", data, "-w", "-m", "fast", "stop")
		return "", nil, err
	}
	if err := ensureDatabase(url); err != nil {
		_ = run(root, ctl, "-D", data, "-w", "-m", "fast", "stop")
		return "", nil, err
	}
	return url, func() { _ = run(root, ctl, "-D", data, "-w", "-m", "fast", "stop") }, nil
}

func databaseURL(port int) (string, error) {
	if err := requireLoopback("127.0.0.1"); err != nil {
		return "", err
	}
	return fmt.Sprintf("postgres://myinfra@127.0.0.1:%d/myinfra?sslmode=disable", port), nil
}

func ensureDatabase(url string) error {
	admin := strings.Replace(url, "/myinfra?", "/postgres?", 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'myinfra')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = conn.Exec(ctx, `CREATE DATABASE myinfra OWNER myinfra`)
	return err
}

// extractedMark holds the digest of the archive last unpacked in full. A
// folder without it, or from another archive, is unpacked again: a binary
// only checking for bin/postgres kept a half extraction, or one without its
// libraries, for good.
const extractedMark = ".myinfra-archive"

// extractPostgres unpacks the programs unless they are already there. It
// returns true when it unpacked them.
func extractPostgres(archive []byte, root string) (bool, error) {
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if mark, err := os.ReadFile(filepath.Join(root, extractedMark)); err == nil && strings.TrimSpace(string(mark)) == digest {
		return false, nil
	}
	// The data lives beside, in pgdata: this folder only holds the programs.
	if err := os.RemoveAll(root); err != nil {
		return false, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return false, err
	}
	if err := unpackPostgres(archive, root); err != nil {
		return false, err
	}
	return true, os.WriteFile(filepath.Join(root, extractedMark), []byte(digest+"\n"), 0o644)
}

func unpackPostgres(archive []byte, root string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := header.Name
		if name == "" || path.Clean(strings.ReplaceAll(name, "\\", "/")) == "." {
			continue
		}
		dest, err := safeJoin(root, name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			file, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tr)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			// The Linux libraries are reached through links such as
			// libicuuc.so.60 -> libicuuc.so.60.2: without them initdb stops.
			if err := safeLink(root, dest, header.Linkname); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			_ = os.Remove(dest)
			if err := os.Symlink(header.Linkname, dest); err != nil {
				return err
			}
		}
	}
}

// safeLink refuses a link that leads out of the PostgreSQL folder.
func safeLink(root, dest, target string) error {
	if target == "" || filepath.IsAbs(target) || strings.HasPrefix(target, "/") {
		return errString("Archive PostgreSQL refusée.")
	}
	resolved := filepath.Join(filepath.Dir(dest), filepath.FromSlash(target))
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errString("Archive PostgreSQL refusée.")
	}
	return nil
}

func safeJoin(root, name string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", errString("Archive PostgreSQL refusée.")
	}
	dest := filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errString("Archive PostgreSQL refusée.")
	}
	return dest, nil
}

func runningPort(data string) (int, bool) {
	raw, err := os.ReadFile(filepath.Join(data, "postmaster.pid"))
	if err != nil {
		return 0, false
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) < 4 {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || !processAlive(pid) {
		return 0, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[3]))
	if err != nil || port <= 0 {
		return 0, false
	}
	return port, true
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return port, ln.Close()
}

func run(root, name string, args ...string) error {
	cmd := proc.Hide(exec.Command(name, args...))
	cmd.Env = pgEnv(root)
	// On Windows, pg_ctl start hands its inheritable handles to the server,
	// which keeps the output pipe open for good: without a delay, waiting
	// for the end of the output never returns once PostgreSQL runs.
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if err != nil {
		text := string(out)
		if len(text) > 400 {
			text = text[len(text)-400:]
		}
		return fmt.Errorf("%s", strings.TrimSpace(text))
	}
	return nil
}

func pgEnv(root string) []string {
	lib := filepath.Join(root, "lib")
	sep := string(os.PathListSeparator)
	return append(os.Environ(),
		"LD_LIBRARY_PATH="+lib,
		"DYLD_LIBRARY_PATH="+lib,
		"PATH="+filepath.Join(root, "bin")+sep+os.Getenv("PATH"),
	)
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func tail(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(raw) > 800 {
		raw = raw[len(raw)-800:]
	}
	return strings.TrimSpace(string(raw))
}
