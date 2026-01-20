FROM codercom/code-server:latest

# Build args for matching host user UID:GID
ARG UID=1000
ARG GID=1000

# Run as root to modify system files
USER root

# Modify coder user to match host UID:GID for correct file ownership
# If target GID already exists (e.g., dialout=20, staff=20), use that group
# If not, create a group with that GID first
RUN if ! getent group ${GID} > /dev/null; then \
        groupmod -g ${GID} coder; \
    fi && \
    usermod -u ${UID} -g ${GID} coder

# Fix ownership of coder home directory
RUN chown -R ${UID}:${GID} /home/coder

# Switch back to coder user (code-server runs as coder by default)
USER coder
