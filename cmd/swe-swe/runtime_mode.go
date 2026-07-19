package main

import "fmt"

// Runtime modes for `swe-swe init --runtime`. They unify two flags that used to
// sound like a boolean pair but control different axes: --with-docker is a
// capability granted to the workspace container, while --dockerless is a
// deployment mode with no containers at all.
const (
	// RuntimeContainer is the docker-compose mode WITHOUT the host docker
	// socket mounted into the workspace container.
	RuntimeContainer = "container"
	// RuntimeContainerWithDockerSocket is RuntimeContainer plus the host
	// docker socket, i.e. the legacy --with-docker.
	RuntimeContainerWithDockerSocket = "container-with-docker-socket"
	// RuntimeHost runs swe-swe-server directly on the host with no
	// containers, i.e. the legacy --dockerless.
	RuntimeHost = "host"
)

// DefaultRuntime is what a bare `swe-swe init` resolves to when neither
// --runtime nor a legacy flag is given.
const DefaultRuntime = RuntimeContainer

// runtimeModes lists the valid --runtime values in help order.
var runtimeModes = []string{RuntimeContainer, RuntimeContainerWithDockerSocket, RuntimeHost}

// validRuntime reports whether s names a runtime mode.
func validRuntime(s string) bool {
	for _, m := range runtimeModes {
		if s == m {
			return true
		}
	}
	return false
}

// runtimeFromLegacy maps the legacy --with-docker / --dockerless booleans onto
// a runtime mode. Both set at once is caller-rejected, not represented here.
func runtimeFromLegacy(withDocker, dockerless bool) string {
	switch {
	case dockerless:
		return RuntimeHost
	case withDocker:
		return RuntimeContainerWithDockerSocket
	default:
		return DefaultRuntime
	}
}

// runtimeWithDocker reports whether mode mounts the host docker socket into the
// workspace container.
func runtimeWithDocker(mode string) bool {
	return mode == RuntimeContainerWithDockerSocket
}

// runtimeDockerless reports whether mode runs host-native with no containers.
func runtimeDockerless(mode string) bool {
	return mode == RuntimeHost
}

// resolveRuntime folds --runtime and the legacy flags into one mode.
//
// The legacy flags keep working indefinitely (they are simply undocumented), so
// existing scripts and saved configs are unaffected. An explicit --runtime that
// AGREES with a legacy flag is accepted; one that CONTRADICTS it is a hard
// error rather than a silent winner, because either reading would surprise
// someone. defaultMode is what an invocation with no runtime signal at all
// resolves to -- callers pass the saved mode when reusing a previous init.
func resolveRuntime(runtimeFlag string, runtimeSet, withDocker, dockerless bool, defaultMode string) (string, error) {
	if withDocker && dockerless {
		return "", fmt.Errorf("--with-docker and --dockerless are mutually exclusive: --with-docker gives the container docker access, --dockerless uses no containers at all (use --runtime=%s or --runtime=%s)",
			RuntimeContainerWithDockerSocket, RuntimeHost)
	}
	if !runtimeSet {
		if !withDocker && !dockerless {
			if defaultMode == "" {
				return DefaultRuntime, nil
			}
			if !validRuntime(defaultMode) {
				return "", fmt.Errorf("invalid saved runtime %q (expected one of %v)", defaultMode, runtimeModes)
			}
			return defaultMode, nil
		}
		return runtimeFromLegacy(withDocker, dockerless), nil
	}
	if !validRuntime(runtimeFlag) {
		return "", fmt.Errorf("invalid --runtime %q (expected one of %v)", runtimeFlag, runtimeModes)
	}
	if withDocker || dockerless {
		if legacy := runtimeFromLegacy(withDocker, dockerless); legacy != runtimeFlag {
			legacyFlag := "--with-docker"
			if dockerless {
				legacyFlag = "--dockerless"
			}
			return "", fmt.Errorf("--runtime=%s conflicts with %s (which means --runtime=%s): pass only one", runtimeFlag, legacyFlag, legacy)
		}
	}
	return runtimeFlag, nil
}
