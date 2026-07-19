package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestResolveRuntime(t *testing.T) {
	tests := []struct {
		name        string
		runtimeFlag string
		runtimeSet  bool
		withDocker  bool
		dockerless  bool
		defaultMode string
		want        string
		wantErr     bool
	}{
		{
			name: "no signal at all falls back to the default mode",
			want: DefaultRuntime,
		},
		{
			name:        "no signal reuses a saved mode when one is supplied",
			defaultMode: RuntimeHost,
			want:        RuntimeHost,
		},
		{
			name:        "saved mode is validated",
			defaultMode: "nonsense",
			wantErr:     true,
		},
		{
			name:       "legacy --with-docker maps to the socket mode",
			withDocker: true,
			want:       RuntimeContainerWithDockerSocket,
		},
		{
			name:       "legacy --dockerless maps to host",
			dockerless: true,
			want:       RuntimeHost,
		},
		{
			name:        "a legacy flag beats a saved mode from a previous init",
			dockerless:  true,
			defaultMode: RuntimeContainer,
			want:        RuntimeHost,
		},
		{
			name:       "both legacy flags together is an error",
			withDocker: true,
			dockerless: true,
			wantErr:    true,
		},
		{
			name:        "explicit --runtime wins over the saved mode",
			runtimeFlag: RuntimeContainerWithDockerSocket,
			runtimeSet:  true,
			defaultMode: RuntimeHost,
			want:        RuntimeContainerWithDockerSocket,
		},
		{
			name:        "explicit --runtime is validated",
			runtimeFlag: "vm",
			runtimeSet:  true,
			wantErr:     true,
		},
		{
			name:        "--runtime agreeing with a legacy flag is accepted",
			runtimeFlag: RuntimeHost,
			runtimeSet:  true,
			dockerless:  true,
			want:        RuntimeHost,
		},
		{
			name:        "--runtime contradicting --dockerless is an error",
			runtimeFlag: RuntimeContainer,
			runtimeSet:  true,
			dockerless:  true,
			wantErr:     true,
		},
		{
			name:        "--runtime contradicting --with-docker is an error",
			runtimeFlag: RuntimeContainer,
			runtimeSet:  true,
			withDocker:  true,
			wantErr:     true,
		},
		{
			name:        "explicit --runtime=container with no legacy flag",
			runtimeFlag: RuntimeContainer,
			runtimeSet:  true,
			want:        RuntimeContainer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveRuntime(tt.runtimeFlag, tt.runtimeSet, tt.withDocker, tt.dockerless, tt.defaultMode)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveRuntime() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRuntime() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveRuntime() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The legacy flags must keep parsing while no longer being advertised.
func TestDeprecatedInitFlagsHiddenButUsable(t *testing.T) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	withDocker := fs.Bool("with-docker", false, "legacy")
	dockerless := fs.Bool("dockerless", false, "legacy")
	fs.String("runtime", DefaultRuntime, "documented")

	var buf bytes.Buffer
	printInitFlagUsage(fs, &buf)
	out := buf.String()

	for name := range deprecatedInitFlags {
		if strings.Contains(out, "-"+name+"\n") {
			t.Errorf("deprecated flag -%s should not appear in usage:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "-runtime") {
		t.Errorf("-runtime should appear in usage:\n%s", out)
	}

	if err := fs.Parse([]string{"--with-docker", "--dockerless"}); err != nil {
		t.Fatalf("deprecated flags must still parse: %v", err)
	}
	if !*withDocker || !*dockerless {
		t.Errorf("deprecated flags parsed but did not take effect")
	}
}

func TestNormalizeRuntimeForLoad(t *testing.T) {
	tests := []struct {
		name           string
		config         InitConfig
		marker         bool
		wantRuntime    string
		wantWithDocker bool
		wantDockerless bool
	}{
		{
			name:        "pre-runtime file with no legacy flag is a plain container",
			config:      InitConfig{},
			wantRuntime: RuntimeContainer,
		},
		{
			name:           "pre-runtime file with withDocker becomes the socket mode",
			config:         InitConfig{WithDocker: true},
			wantRuntime:    RuntimeContainerWithDockerSocket,
			wantWithDocker: true,
		},
		{
			name:           "pre-runtime file plus a mode marker becomes host",
			config:         InitConfig{},
			marker:         true,
			wantRuntime:    RuntimeHost,
			wantDockerless: true,
		},
		{
			name:           "an explicit runtime wins over the marker",
			config:         InitConfig{Runtime: RuntimeContainer},
			marker:         true,
			wantRuntime:    RuntimeContainer,
			wantDockerless: false,
		},
		{
			name:           "an explicit runtime wins over a stale legacy key",
			config:         InitConfig{Runtime: RuntimeContainer, WithDocker: true},
			wantRuntime:    RuntimeContainer,
			wantWithDocker: false,
		},
		{
			name:           "host runtime populates the unpersisted Dockerless field",
			config:         InitConfig{Runtime: RuntimeHost},
			wantRuntime:    RuntimeHost,
			wantDockerless: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config
			normalizeRuntimeForLoad(&got, tt.marker)
			if got.Runtime != tt.wantRuntime {
				t.Errorf("Runtime = %q, want %q", got.Runtime, tt.wantRuntime)
			}
			if got.WithDocker != tt.wantWithDocker {
				t.Errorf("WithDocker = %v, want %v", got.WithDocker, tt.wantWithDocker)
			}
			if got.Dockerless != tt.wantDockerless {
				t.Errorf("Dockerless = %v, want %v", got.Dockerless, tt.wantDockerless)
			}
		})
	}
}

func TestNormalizeRuntimeForSave(t *testing.T) {
	tests := []struct {
		name           string
		config         InitConfig
		wantRuntime    string
		wantWithDocker bool
	}{
		{
			name:        "a caller that set neither writes the default",
			config:      InitConfig{},
			wantRuntime: RuntimeContainer,
		},
		{
			name:           "a caller that set only the legacy bool still writes runtime",
			config:         InitConfig{WithDocker: true},
			wantRuntime:    RuntimeContainerWithDockerSocket,
			wantWithDocker: true,
		},
		{
			name:        "a caller that set only Dockerless still writes runtime",
			config:      InitConfig{Dockerless: true},
			wantRuntime: RuntimeHost,
		},
		{
			name:           "runtime is the authority: a contradicting legacy bool is overwritten",
			config:         InitConfig{Runtime: RuntimeContainer, WithDocker: true},
			wantRuntime:    RuntimeContainer,
			wantWithDocker: false,
		},
		{
			name:           "socket runtime writes the legacy bool for older CLIs",
			config:         InitConfig{Runtime: RuntimeContainerWithDockerSocket},
			wantRuntime:    RuntimeContainerWithDockerSocket,
			wantWithDocker: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config
			normalizeRuntimeForSave(&got)
			if got.Runtime != tt.wantRuntime {
				t.Errorf("Runtime = %q, want %q", got.Runtime, tt.wantRuntime)
			}
			if got.WithDocker != tt.wantWithDocker {
				t.Errorf("WithDocker = %v, want %v", got.WithDocker, tt.wantWithDocker)
			}
		})
	}
}

// A config written by this CLI must come back identical, without consulting the
// mode marker -- the marker fallback is only for files that predate `runtime`.
func TestRuntimeJSONRoundTrip(t *testing.T) {
	for _, mode := range runtimeModes {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			if err := saveInitConfig(dir, InitConfig{Agents: []string{"claude"}, Runtime: mode}); err != nil {
				t.Fatalf("saveInitConfig: %v", err)
			}
			// isDockerlessProject(dir) is false here: no marker was written.
			got, err := loadInitConfig(dir)
			if err != nil {
				t.Fatalf("loadInitConfig: %v", err)
			}
			if got.Runtime != mode {
				t.Errorf("Runtime = %q, want %q", got.Runtime, mode)
			}
			if got.WithDocker != runtimeWithDocker(mode) {
				t.Errorf("WithDocker = %v, want %v", got.WithDocker, runtimeWithDocker(mode))
			}
			if got.Dockerless != runtimeDockerless(mode) {
				t.Errorf("Dockerless = %v, want %v", got.Dockerless, runtimeDockerless(mode))
			}
		})
	}
}

// The legacy booleans are still what the rest of init.go dispatches on, so the
// round trip through a runtime mode must be lossless.
func TestRuntimeLegacyRoundTrip(t *testing.T) {
	tests := []struct {
		withDocker bool
		dockerless bool
	}{
		{withDocker: false, dockerless: false},
		{withDocker: true, dockerless: false},
		{withDocker: false, dockerless: true},
	}

	for _, tt := range tests {
		mode := runtimeFromLegacy(tt.withDocker, tt.dockerless)
		if got := runtimeWithDocker(mode); got != tt.withDocker {
			t.Errorf("runtimeWithDocker(%q) = %v, want %v", mode, got, tt.withDocker)
		}
		if got := runtimeDockerless(mode); got != tt.dockerless {
			t.Errorf("runtimeDockerless(%q) = %v, want %v", mode, got, tt.dockerless)
		}
	}
}
