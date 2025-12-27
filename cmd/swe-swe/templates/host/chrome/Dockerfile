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
    nginx \
    ca-certificates \
    libnss3-tools \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -s /bin/bash chrome

# Create directories for supervisor and logs
RUN mkdir -p /var/log/supervisor /var/run

# Supervisor config to manage processes
COPY chrome/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Nginx config for CDP reverse proxy (fixes Host header issue)
COPY chrome/nginx-cdp.conf /etc/nginx/nginx.conf

# Create nginx log directory
RUN mkdir -p /var/log/nginx

# noVNC web files location
# Create symlink to full viewer (vnc_lite.html) as vnc.html so it's available when wrapper redirects to it
ENV NOVNC_HOME=/usr/share/novnc
RUN ln -sf /usr/share/novnc/vnc_lite.html /usr/share/novnc/vnc.html

# Customize noVNC title to 'chrome - swe-swe'
RUN sed -i 's/<title>.*<\/title>/<title>chrome - swe-swe<\/title>/' /usr/share/novnc/vnc_lite.html

# Copy noVNC wrapper HTML for /chrome basepath (configures correct WebSocket path)
COPY chrome/novnc-wrapper.html /app/novnc-wrapper.html

# Copy entrypoint script for enterprise certificate installation and noVNC setup
COPY chrome/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Expose ports
# 9223: CDP via nginx reverse proxy (Chrome binds to localhost only, nginx fixes Host header)
# 6080: noVNC web UI - for developer observation
EXPOSE 9223 6080

# Entrypoint installs enterprise certificates (if mounted), then runs CMD
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Start supervisor to manage Xvfb, Chrome, x11vnc, nginx, and websockify
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
