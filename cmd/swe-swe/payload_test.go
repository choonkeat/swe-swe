package main

import (
	"bytes"
	"debug/elf"
	"io"
	"runtime"
	"testing"
)

// elfMachineForArch maps a Go GOARCH to the ELF machine type its compiled
// binaries carry, so the embed test can confirm the payload was built for
// the right CPU architecture.
func elfMachineForArch(goarch string) (elf.Machine, bool) {
	switch goarch {
	case "amd64":
		return elf.EM_X86_64, true
	case "arm64":
		return elf.EM_AARCH64, true
	default:
		return 0, false
	}
}

// TestDockerlessPayloadEmbedsBinaries asserts that the CLI embeds every
// helper binary listed in dockerlessBinaries and that each is a Linux ELF
// for the host architecture. This is the Phase 1 guarantee: a published CLI
// carries a usable, correct-arch payload to dump on `init --dockerless`.
//
// The payload binaries are build artifacts produced by the Makefile
// `dockerless-payload` target (a prerequisite of `make build`/`test-cli`),
// not committed to git. Running this test outside `make` (without first
// building the payload) is expected to fail until the binaries exist.
func TestDockerlessPayloadEmbedsBinaries(t *testing.T) {
	wantMachine, ok := elfMachineForArch(runtime.GOARCH)
	if !ok {
		t.Fatalf("unsupported host arch %q for dockerless payload test", runtime.GOARCH)
	}

	binDir := dockerlessPayloadBinDir(runtime.GOARCH)
	for _, name := range dockerlessBinaries {
		name := name
		t.Run(name, func(t *testing.T) {
			p := binDir + "/" + name
			f, err := dockerlessPayload.Open(p)
			if err != nil {
				t.Fatalf("embedded payload missing %s: %v (run `make dockerless-payload`)", p, err)
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				t.Fatalf("reading embedded %s: %v", p, err)
			}
			if len(data) == 0 {
				t.Fatalf("embedded %s is empty", p)
			}

			ef, err := elf.NewFile(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("embedded %s is not a valid ELF: %v", p, err)
			}
			defer ef.Close()

			if ef.Class != elf.ELFCLASS64 {
				t.Errorf("embedded %s: ELF class = %v, want ELFCLASS64", p, ef.Class)
			}
			if ef.Machine != wantMachine {
				t.Errorf("embedded %s: ELF machine = %v, want %v (arch %s)", p, ef.Machine, wantMachine, runtime.GOARCH)
			}
		})
	}
}
