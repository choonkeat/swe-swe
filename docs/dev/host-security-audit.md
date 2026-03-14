# Host Security Audit

## Running Host Commands as Root

The Docker daemon runs on the host (not in a VM). We can execute host-level commands as root via:
```bash
docker run --rm --privileged --pid=host alpine:latest \
    nsenter -t 1 -m -u -i -n -- <command>
```
This enters PID 1's namespaces (mount, UTS, IPC, net) giving full host access. Used for:
- Installing packages (`apt-get install`)
- Managing systemd services
- Reading host logs (`/var/log/auth.log`, `dmesg`, `journalctl`)
- Running `auditctl`/`ausearch` for kernel audit
- Checking `ufw`, `fail2ban`, `ss`, etc.

## Quick checks
```bash
# Failed SSH logins and brute force
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- sh -c 'grep -c "Failed password" /var/log/auth.log; grep "Invalid user" /var/log/auth.log | tail -10'

# Successful SSH logins
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- grep "Accepted" /var/log/auth.log | tail -10

# fail2ban status
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- fail2ban-client status sshd

# Listening ports
docker run --rm --privileged --pid=host --net=host alpine:latest nsenter -t 1 -m -u -i -n -- ss -tlnp

# Firewall
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- ufw status

# Users with login shells
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- grep -v 'nologin\|false' /etc/passwd

# Pending security updates
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- apt list --upgradable 2>/dev/null | grep security

# Suspicious processes
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- ps aux --sort=-%cpu | head -15
```

## Port layout (swe-swe managed)
| Ports | Purpose | Auth |
|-------|---------|------|
| 22 | SSH | pubkey + fail2ban |
| 80 | Traefik HTTP | entry point |
| 1977, 3000-3019, 4000-4019, 23000-23019, 24000-24019 | swe-swe session ports via Traefik | behind auth |
| 5000-5019 | Preview ports (direct access) | no auth, user-controlled |
