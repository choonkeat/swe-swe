# swe-swe/browser-backend -- the relocatable Agent View allocation service.
#
# Same swe-swe-server binary as the main image, run with `-mode browser-backend`,
# plus ONLY the display stack (chromium/Xvfb/x11vnc/noVNC/websockify). A lean
# (dockerless) swe-swe host with no display stack offloads Agent View here:
#
#   docker run -p 9333:9333 -p 6000-6019:6000-6019 -p 7000-7039:7000-7039 \
#       -e SWE_BROWSER_BACKEND_TOKEN=secret swe-swe/browser-backend
#   swe-swe up --agent-view=https://browser-box.internal:9333
#
# Build from the REPO ROOT after `make dockerless-payload` (which builds the
# static swe-swe-server into the payload):
#
#   docker build -f docker/browser-backend/Dockerfile \
#       --build-arg ARCH=amd64 -t swe-swe/browser-backend .
#
# (or `make browser-backend-image`)
FROM debian:bookworm-slim

# Display stack only -- no agent CLIs, no Go toolchain, no web assets beyond
# the noVNC viewer. Mirrors the browser layer of the main image.
#
# Chromium is PINNED to 147 from bookworm/main; the bookworm-security pocket
# shipped 150.0.7871.46 whose zygote SIGTRAPs on launch. Keep this in sync with
# cmd/swe-swe/templates/host/Dockerfile. See
# tasks/2026-07-05-e2e-system-chromium-sigtrap.md.
#
# fonts-noto-color-emoji + fonts-noto-cjk cover emoji and CJK glyphs that
# fonts-dejavu-core (chromium's only font dep) lacks -- without them Chromium
# renders those codepoints as .notdef "tofu" boxes. Mirror the main image.
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium=147.0.7727.137-1~deb12u1 \
    chromium-common=147.0.7727.137-1~deb12u1 \
    xvfb \
    x11vnc \
    novnc \
    websockify \
    ca-certificates \
    fonts-noto-color-emoji \
    fonts-noto-cjk \
    && apt-mark hold chromium chromium-common \
    && rm -rf /var/lib/apt/lists/*

# Prebuilt static swe-swe-server from the dockerless payload (built by
# `make dockerless-payload`). ARCH matches the host you will run on.
ARG ARCH=amd64
COPY cmd/swe-swe/dockerless-payload/bin/linux-${ARCH}/swe-swe-server /usr/local/bin/swe-swe-server

# Custom noVNC viewer (View only / Interactive toggle), same override the main
# image applies.
COPY cmd/swe-swe/templates/host/swe-swe-server/static/vnc_lite.html /usr/share/novnc/vnc_lite.html

# Allocation API port. The CDP (6000-6019) and VNC (7000-7019, plus x11vnc
# internal 7020-7039) ranges must also be published so clients can dial the
# allocated browsers -- see the docker run example above.
ENV SWE_PORT=9333
EXPOSE 9333
EXPOSE 6000-6019
EXPOSE 7000-7039

# chromium needs a writable home for its per-session --user-data-dir under /tmp.
ENTRYPOINT ["/usr/local/bin/swe-swe-server", "-mode", "browser-backend"]
