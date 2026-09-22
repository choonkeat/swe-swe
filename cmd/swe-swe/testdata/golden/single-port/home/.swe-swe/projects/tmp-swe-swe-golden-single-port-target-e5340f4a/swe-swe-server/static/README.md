# Static Files Registration

**IMPORTANT**: When adding new files to this directory, you MUST register them in
`cmd/swe-swe/init.go` in the `hostFiles` slice. Otherwise they won't be copied
during `swe-swe init` and will 404 at runtime.

## After adding a new file

```bash
# 1. Add the file path to hostFiles in init.go
vim cmd/swe-swe/init.go
# Find the hostFiles slice and add your file

# 2. Rebuild and update golden tests
make build golden-update

# 3. Verify your file appears in golden tests
ls cmd/swe-swe/testdata/golden/default/**/swe-swe-server/static/
```

## Why?

Unlike a typical web server that serves all files from a directory, `swe-swe init`
explicitly copies only registered files from the embedded template filesystem. This
gives us control over conditional inclusion but requires manual registration.
