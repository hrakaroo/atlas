// Atlas shared service: library storage, HTTP API and MCP.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"atlas.local/atlas/service/internal/assets"
	appLibrary "atlas.local/atlas/service/internal/library"
	"atlas.local/atlas/service/internal/store"
	"atlas.local/atlas/service/internal/transport"
)

type connection struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Library string `json:"library"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Atlas Go service:", err)
		os.Exit(1)
	}
}

func run() error {
	var library, webRoot, owner, timezone string
	var port int
	var contract, serve bool
	flag.StringVar(&library, "library", "", "Explicit absolute library path")
	flag.IntVar(&port, "port", 47831, "Loopback port; zero selects an available port")
	flag.StringVar(&webRoot, "web-root", "", "Optional canonical web assets override; default is embedded assets")
	flag.StringVar(&owner, "owner-name", "", "Library owner display name")
	flag.StringVar(&timezone, "timezone", "America/Los_Angeles", "Library timezone")
	flag.BoolVar(&contract, "contract-mode", false, "Allow marked disposable test libraries only")
	flag.BoolVar(&serve, "serve", false, "Start the Go service for the configured library")
	flag.Parse()
	if flag.NArg() != 0 || library == "" || contract == serve {
		return errors.New("choose exactly one of --contract-mode or --serve and supply an explicit --library")
	}
	owner = strings.TrimSpace(owner)
	if serve {
		explicitTimezone := false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "timezone" {
				explicitTimezone = true
			}
		})
		if !filepath.IsAbs(library) || owner == "" || !explicitTimezone || timezone == "" {
			return errors.New("--serve requires an absolute --library, nonblank --owner-name and explicit --timezone")
		}
		for _, value := range os.Environ() {
			if strings.HasPrefix(value, "ATLAS_CONTRACT_") {
				return errors.New("contract environment variables are prohibited in --serve mode")
			}
		}
	}
	if port < 0 || port > 65535 {
		return errors.New("invalid port")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	absoluteWeb := ""
	var readAsset func(string) ([]byte, error) = assets.ReadFile
	if webRoot != "" {
		absoluteWeb, err = filepath.Abs(webRoot)
		if err != nil {
			return err
		}
		for _, name := range []string{"index.html", "app.js", "style.css"} {
			info, err := os.Stat(filepath.Join(absoluteWeb, name))
			if err != nil || !info.Mode().IsRegular() {
				return errors.New("explicit --web-root must contain all three canonical web assets")
			}
		}
		readAsset = nil
	}
	// Contract mode requires its harness marker before any runtime/database writes.
	root, err := filepath.Abs(library)
	if err != nil {
		return err
	}
	if serve {
		if err := os.MkdirAll(root, 0755); err != nil {
			return err
		}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if contract {
		temporaryRoot, err := filepath.EvalSymlinks(os.TempDir())
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(temporaryRoot, filepath.Dir(root))
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
			!strings.HasPrefix(filepath.Base(filepath.Dir(root)), "atlas-contract-") {
			return errors.New("test library must be inside a contract-owned temporary root")
		}
		marker, err := os.ReadFile(filepath.Join(filepath.Dir(root), ".atlas-contract-fixture"))
		if err != nil || string(marker) != "atlas-contract-v1" {
			return errors.New("contract mode accepts only marked disposable fixtures")
		}
	}
	if value := os.Getenv("ATLAS_CONTRACT_CLOCK"); value != "" {
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return errors.New("contract clock requires a timezone-aware ISO timestamp")
		}
	}
	var testHook func(string) error
	if contract {
		testHook, err = contractFault(root)
	}
	if err != nil {
		return err
	}
	runtime := filepath.Join(root, ".service")
	if entry, err := os.Lstat(runtime); err == nil && entry.Mode()&os.ModeSymlink != 0 {
		return errors.New("runtime directory must not be a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(runtime, 0700); err != nil {
		return err
	}
	if err := os.Chmod(runtime, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(runtime, "server.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("Atlas shared service is already running for this library")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	// Ownership precedes SQLite open AND connection-file replacement.
	state, err := store.Open(root)
	if err != nil {
		return err
	}
	defer state.Close()
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer listener.Close()
	token, err := existingToken(root)
	if err != nil {
		return err
	}
	if token == "" {
		buffer := make([]byte, 32)
		if _, err = rand.Read(buffer); err != nil {
			return err
		}
		token = base64.RawURLEncoding.EncodeToString(buffer)
	}
	address := "http://" + listener.Addr().String()
	settings := connection{URL: address, Token: token, Library: root}
	clock := time.Now
	if value := os.Getenv("ATLAS_CONTRACT_CLOCK"); value != "" {
		fixed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return err
		}
		clock = func() time.Time { return fixed }
	}
	domain := appLibrary.New(state, appLibrary.Config{Now: clock, Location: location, OwnerName: owner, TestHook: testHook})
	if err := domain.RecoverPendingRefreshes(); err != nil {
		return err
	}
	if err := domain.RecoverPendingTrash(); err != nil {
		return err
	}
	if err := domain.MigrateOwnedStorage(); err != nil {
		return err
	}
	if err := saveConnection(runtime, settings); err != nil {
		return err
	}
	dispatch := domain.Dispatch
	handler := transport.New(transport.Config{Token: token, Library: root, WebRoot: absoluteWeb, ReadAsset: readAsset, Dispatch: dispatch})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 120 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Fprintln(os.Stdout, "Atlas Go migration service listening at", address)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func existingToken(root string) (string, error) {
	input, err := os.OpenFile(filepath.Join(root, ".service", "connection.json"), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return "", errors.New("invalid existing connection settings")
	}
	payload, err := io.ReadAll(io.LimitReader(input, 65537))
	if err != nil {
		return "", err
	}
	var previous connection
	if err := json.Unmarshal(payload, &previous); err != nil {
		return "", errors.New("invalid existing connection settings")
	}
	parsed, err := url.Parse(previous.URL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil ||
		previous.Library != root || previous.Token == "" {
		return "", errors.New("invalid existing connection settings")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("invalid existing connection settings")
	}
	return previous.Token, nil
}

func saveConnection(runtime string, settings connection) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(runtime, "connection-*.tmp")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(runtime, "connection.json"))
}
