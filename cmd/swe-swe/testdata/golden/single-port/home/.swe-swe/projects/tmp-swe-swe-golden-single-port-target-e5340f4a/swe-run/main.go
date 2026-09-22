// Command swe-run is a tiny foreman-compatible Procfile runner for swe-swe
// sessions. It reads a Procfile, assigns each service a session-unique port
// derived from the session base PORT, publishes discovery env vars
// (PORT_<NAME>), multiplexes service output into the Agent Terminal, and tears
// the whole process group down cleanly on any service exit or on SIGINT/SIGTERM.
//
// Because its children are ordinary processes in the session's process group
// (no Docker socket, no root), they die with the session -- nothing leaks.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// fallbackBasePort is used when the session PORT env var is unset or invalid.
const fallbackBasePort = 5000

// defaultGrace is how long teardown waits after SIGTERM before SIGKILL.
const defaultGrace = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(runMain(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv, os.Environ(), ""))
}

// basePortFromEnv reads the session base PORT. It returns (port, true) for a
// valid 1024..65535 value, else (fallbackBasePort, false).
func basePortFromEnv(getenv func(string) string) (int, bool) {
	v := getenv("PORT")
	if v == "" {
		return fallbackBasePort, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1024 || n > 65535 {
		return fallbackBasePort, false
	}
	return n, true
}

// printPortTable prints the startup banner: each service, its assigned port, and
// how siblings reach it (primary via the Preview tab; others via PORT_<NAME>).
func printPortTable(w io.Writer, services []Service, ports map[string]int, primary string) {
	width := nameWidth(services)
	fmt.Fprintf(w, "swe-run | assigning ports for %d service(s):\n", len(services))
	for _, s := range services {
		pad := s.Name
		for len(pad) < width {
			pad += " "
		}
		if s.Name == primary {
			fmt.Fprintf(w, "swe-run |   %s -> %d  (primary; PORT, Preview tab)\n", pad, ports[s.Name])
		} else {
			fmt.Fprintf(w, "swe-run |   %s -> %d  (PORT_%s)\n", pad, ports[s.Name], normalizeEnvName(s.Name))
		}
	}
}

// loadEnvFileIfExists parses path as a KEY=value env file, returning nil if the
// file does not exist.
func loadEnvFileIfExists(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return parseEnvFile(f)
}

// runMain is the testable entry point. workdir roots the Procfile and env-file
// lookups ("" means the process cwd).
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, environ []string, workdir string) int {
	fs := flag.NewFlagSet("swe-run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	procfileName := fs.String("f", "Procfile", "path to the Procfile")
	primaryFlag := fs.String("primary", "", "name of the primary service (defaults to 'web', else the first line)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	resolve := func(p string) string {
		if filepath.IsAbs(p) || workdir == "" {
			return p
		}
		return filepath.Join(workdir, p)
	}

	procfilePath := resolve(*procfileName)
	pf, err := os.Open(procfilePath)
	if err != nil {
		fmt.Fprintf(stderr, "swe-run: cannot open Procfile %q: %v\n", procfilePath, err)
		return 1
	}
	services, err := parseProcfile(pf)
	pf.Close()
	if err != nil {
		fmt.Fprintf(stderr, "swe-run: %v\n", err)
		return 1
	}

	base, ok := basePortFromEnv(getenv)
	if !ok {
		fmt.Fprintf(stderr, "swe-run: PORT env not set/invalid; using fallback base %d\n", base)
	}

	primary, err := selectPrimary(services, *primaryFlag)
	if err != nil {
		fmt.Fprintf(stderr, "swe-run: %v\n", err)
		return 1
	}
	ports, err := assignPorts(base, services, *primaryFlag)
	if err != nil {
		fmt.Fprintf(stderr, "swe-run: %v\n", err)
		return 1
	}

	sweEnv, err := loadEnvFileIfExists(resolve(filepath.Join(".swe-swe", "env")))
	if err != nil {
		fmt.Fprintf(stderr, "swe-run: reading .swe-swe/env: %v\n", err)
		return 1
	}
	dotEnv, err := loadEnvFileIfExists(resolve(".env"))
	if err != nil {
		fmt.Fprintf(stderr, "swe-run: reading .env: %v\n", err)
		return 1
	}

	printPortTable(stdout, services, ports, primary)

	sup := &supervisor{
		services:  services,
		ports:     ports,
		sweEnv:    sweEnv,
		dotEnv:    dotEnv,
		inherited: environ,
		out:       stdout,
		grace:     defaultGrace,
		noColor:   noColorSet(getenv),
	}
	return sup.run(ctx)
}

// noColorSet honors the NO_COLOR convention: any non-empty value disables color.
func noColorSet(getenv func(string) string) bool {
	return getenv("NO_COLOR") != ""
}
