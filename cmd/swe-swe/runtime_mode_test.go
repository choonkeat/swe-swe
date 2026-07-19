package main

import "testing"

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
