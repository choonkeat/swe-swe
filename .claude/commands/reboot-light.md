Light reboot of the swe-swe stack: no pre-restart smoke test, no graceful
end_session ceremony -- just clear stray containers, then let the host
restart-loop redeploy us (any death of our container makes its foreground
`swe-swe up` return, and the loop then runs down -> init -> build
--no-cache -> up).

Steps:

1. Identify our own container: `SELF=$(hostname)` (the in-container
   hostname IS our container id).
2. List everything: `docker ps -a --format '{{.ID}} {{.Names}} {{.Status}}'`.
   The kill list = every container EXCEPT our own id and any name matching
   `swe-swe-tunneld`. NEVER touch swe-swe-tunneld.
3. Tell the user (send_message) what is on the kill list and that our own
   container dies last -- this session included; other live sessions in it
   will disconnect abruptly and can be resumed later via
   /swe-swe:recordings-list-orphaned. Wait for their go-ahead ONLY if the
   kill list contains something unexpected (not obviously a test/e2e/
   browser-backend leftover); otherwise proceed.
4. Kill the strays: `docker kill <ids>` (kill, not stop -- they are
   leftovers by definition).
5. Send a final chat message ("rebooting now, back in a few minutes --
   the rebuild takes a while"), then kill our own container:
   `docker kill $SELF`. Expect this command to never return.
