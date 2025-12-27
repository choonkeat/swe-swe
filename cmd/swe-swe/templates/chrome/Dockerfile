# Chrome browser with Xvfb + noVNC for visual observability
# Enables Claude to control a browser while developers watch via VNC

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    xvfb \
    x11vnc \
    novnc \
    websockify \
    chromium \
    supervisor \
    procps \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -s /bin/bash chrome

# Create directories for supervisor and logs
RUN mkdir -p /var/log/supervisor /var/run

# Supervisor config to manage processes
COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# noVNC web files location
ENV NOVNC_HOME=/usr/share/novnc

# Expose ports
# 9222: Chrome DevTools Protocol (CDP) - for MCP Playwright connection
# 6080: noVNC web UI - for developer observation
EXPOSE 9222 6080

# Start supervisor to manage Xvfb, Chrome, x11vnc, and websockify
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
