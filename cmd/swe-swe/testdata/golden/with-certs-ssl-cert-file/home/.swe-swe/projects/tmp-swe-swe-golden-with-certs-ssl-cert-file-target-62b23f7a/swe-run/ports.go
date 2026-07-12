package main

import "fmt"

// previewBaseLo/Hi are the session preview-base range swe-swe-server allocates
// from (previewPortStart..previewPortEnd in server main.go). assignPorts must
// keep non-primary ports session-unique across every base in this range and
// clear of the derived-port bands the server carves off each base.
const (
	previewBaseLo = 3000
	previewBaseHi = 3019
)

// reservedOffsets are the fixed offsets swe-swe-server adds to a session's
// preview base for its own derived listeners (agent-chat +1000, public +2000,
// cdp +3000, vnc +4000, files +6000, proxy +20000). Offset 0 is the preview
// base band itself. A non-primary service port must avoid every band
// [base+off, base+off+19] for base in [previewBaseLo, previewBaseHi].
var reservedOffsets = []int{0, 1000, 2000, 3000, 4000, 6000, 20000}

// inReservedBand reports whether p falls in any derived-port band across all
// preview bases -- i.e. whether it could collide with a server listener in some
// session.
func inReservedBand(p int) bool {
	for _, off := range reservedOffsets {
		lo := previewBaseLo + off
		hi := previewBaseHi + off
		if p >= lo && p <= hi {
			return true
		}
	}
	return false
}

// selectPrimary picks the primary service: the explicit override if given (must
// exist), else the service named "web" if present, else the first line.
func selectPrimary(services []Service, override string) (string, error) {
	if override != "" {
		for _, s := range services {
			if s.Name == override {
				return override, nil
			}
		}
		return "", fmt.Errorf("primary service %q not found in Procfile", override)
	}
	for _, s := range services {
		if s.Name == "web" {
			return "web", nil
		}
	}
	return services[0].Name, nil
}

// assignPorts maps each service to a session-unique port derived from base.
//
// The primary service (see selectPrimary) gets the session base PORT so the
// default Preview tab shows it with zero config. The i-th non-primary service
// (0-based, in file order) gets base + 5000 + i*20, which for every base in
// [previewBaseLo, previewBaseHi] stays inside the free 8000-8999 block, is
// session-unique across bases, avoids the reserved derived-port bands, and
// stays within [1024, 65535].
func assignPorts(base int, services []Service, primaryOverride string) (map[string]int, error) {
	if len(services) == 0 {
		return nil, fmt.Errorf("no services to assign ports")
	}
	primary, err := selectPrimary(services, primaryOverride)
	if err != nil {
		return nil, err
	}

	ports := make(map[string]int, len(services))
	ports[primary] = base

	i := 0
	for _, s := range services {
		if s.Name == primary {
			continue
		}
		p := base + 5000 + i*20
		if p < 1024 || p > 65535 {
			return nil, fmt.Errorf("service %q: derived port %d out of range [1024,65535] (too many services)", s.Name, p)
		}
		if inReservedBand(p) {
			return nil, fmt.Errorf("service %q: derived port %d collides with a reserved swe-swe port band (too many services)", s.Name, p)
		}
		ports[s.Name] = p
		i++
	}
	return ports, nil
}
