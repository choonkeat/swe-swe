FROM codercom/code-server:latest

# Build args for matching host user UID:GID
ARG UID=1000
ARG GID=1000

# Run as root to modify system files
USER root

# Modify existing coder group and user with captured host UID:GID
# This ensures container coder user has same UID:GID as host user
RUN groupmod -g ${GID} coder && usermod -u ${UID} -g ${GID} coder

# Fix ownership of coder home directory
RUN chown -R ${UID}:${GID} /home/coder

# Switch back to coder user (code-server runs as coder by default)
USER coder
