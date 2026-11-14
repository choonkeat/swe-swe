Restart the swe-swe stack following the instructions in ./docs/dev/how-to-restart.md

Before going down: check the host restart loop is running the CURRENT
restart-loop2.sh. bash parses the whole `while true` loop once at start,
and `sed -i` / editor saves replace the file, so edits made after the
loop started (e.g. the --tunnel-server-url port) are silently ignored.

    docker run --rm --privileged --pid=host alpine:latest \
      nsenter -t 1 -m -u -i -n -- sh -c \
      'ls -l /proc/$(pgrep -f "^bash .*restart-loop2" | head -1)/fd/255'

If the target ends in `(deleted)`, the loop is stale: do NOT reboot the
normal way (it would redeploy with the old settings). Tell the user to
restart the loop instead: on the host, `screen -r`, Ctrl-C (this stops
the stack), then from /home/app/workspace/swe-swe re-run
`bash .swe-swe/restart-loop2.sh` with the usual exports.
