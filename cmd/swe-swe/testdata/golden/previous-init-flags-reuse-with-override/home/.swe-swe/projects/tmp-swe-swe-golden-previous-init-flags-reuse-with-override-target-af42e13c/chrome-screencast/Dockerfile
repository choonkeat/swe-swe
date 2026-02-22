# Chrome browser with CDP screencast for visual observability
# Uses Page.startScreencast API instead of VNC for better resize handling
# Lighter weight than VNC: no Xvfb, x11vnc, or noVNC needed

FROM mcr.microsoft.com/playwright:v1.48.0-noble

USER root

# Install nginx for CDP reverse proxy and supervisor for process management
# Note: supervisor installed via pip to avoid transient Ubuntu mirror issues on ARM64
RUN apt-get update && apt-get install -y --no-install-recommends \
    nginx \
    python3-pip \
    libnss3-tools \
    && pip3 install --break-system-packages supervisor \
    && rm -rf /var/lib/apt/lists/*

# Create directories for supervisor and logs
RUN mkdir -p /var/log/supervisor /var/run /var/log/nginx /app

# Copy screencast server files
COPY chrome-screencast/package.json /app/
COPY chrome-screencast/server.js /app/
COPY chrome-screencast/static /app/static/

# Install Node dependencies
WORKDIR /app
RUN npm install --production

# Supervisor config to manage processes
COPY chrome-screencast/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Nginx config for CDP reverse proxy (fixes Host header issue)
COPY chrome-screencast/nginx-cdp.conf /etc/nginx/nginx.conf

# Copy entrypoint script for enterprise certificate installation
COPY chrome-screencast/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Expose ports
# 9223: CDP via nginx reverse proxy
# 6080: Screencast web UI
EXPOSE 9223 6080

# Entrypoint installs enterprise certificates (if mounted), then runs CMD
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Start supervisor to manage Chrome, nginx, and screencast server
CMD ["/usr/local/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
