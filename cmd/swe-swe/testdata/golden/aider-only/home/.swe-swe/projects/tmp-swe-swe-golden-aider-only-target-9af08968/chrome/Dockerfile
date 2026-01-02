# Chrome browser with noVNC for visual observability
# Enables Claude to control a browser while developers watch via VNC
# Based on selenium/standalone-chrome which includes Chrome, Xvfb, VNC

FROM seleniarm/standalone-chromium:124.0

USER root

# Install nginx for CDP reverse proxy and supervisor for process management
RUN apt-get update && apt-get install -y --no-install-recommends \
    nginx \
    supervisor \
    libnss3-tools \
    && rm -rf /var/lib/apt/lists/*

# Create directories for supervisor and logs
RUN mkdir -p /var/log/supervisor /var/run /var/log/nginx

# Supervisor config to manage processes
COPY chrome/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Nginx config for CDP reverse proxy (fixes Host header issue)
COPY chrome/nginx-cdp.conf /etc/nginx/nginx.conf

# noVNC is at /opt/bin/noVNC in selenium image
ENV NOVNC_HOME=/opt/bin/noVNC

# Customize noVNC title to 'chrome - swe-swe'
RUN if [ -f /opt/bin/noVNC/vnc_lite.html ]; then \
        sed -i 's/<title>.*<\/title>/<title>chrome - swe-swe<\/title>/' /opt/bin/noVNC/vnc_lite.html; \
    fi

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

# Start supervisor to manage Chrome, nginx, and websockify (Xvfb and VNC from base image)
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
