FROM codercom/code-server:latest

# Build args for matching host user UID:GID
ARG UID=1000
ARG GID=1000

# Recreate coder group and user with captured host UID:GID
# This ensures container coder user has same UID:GID as host user
RUN groupdel coder && groupadd -g ${GID} coder && \
    userdel coder && useradd -m -u ${UID} -g ${GID} -s /bin/bash coder

# Fix ownership of coder home directory
RUN chown -R ${UID}:${GID} /home/coder
