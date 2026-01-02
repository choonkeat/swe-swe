# Task: Fix Password Managers Not Recognizing Login Form

**Date**: 2026-01-02
**Status**: Planning

## Goal

Make the swe-swe auth login form compatible with password managers (1Password, iOS Keychain, Android autofill) so users can save and autofill credentials.

## Current Form

Location: `cmd/swe-swe/templates/host/auth/main.go:186-190`

```html
<form method="POST" action="/swe-swe-auth/login">
    <input type="hidden" name="redirect" value="...">
    <input type="password" name="password" autocomplete="current-password" placeholder="Password" required>
    <button type="submit">Login</button>
</form>
```

---

## Phase 1: Research

### What will be achieved
Understand exactly why password managers aren't detecting the login form, so we make the right fix instead of guessing.

### Steps
1. Read current login form HTML in `cmd/swe-swe/templates/host/auth/main.go`
2. Research 1Password's requirements for form detection (SSL, autocomplete attributes, form structure)
3. Research iOS password autofill requirements (Safari, system keychain)
4. Research Android password manager requirements (Google Password Manager, system autofill framework)
5. Compare our form against those requirements to identify gaps

### Verification
- No code changes in this phase, so no regression risk
- Success = clear list of what needs to change, covering desktop and mobile

---

## Phase 2: Fix HTML Markup

### What will be achieved
Update the login form HTML so password managers (1Password, iOS, Android) can detect and autofill it.

### Steps
1. Apply the changes identified in Phase 1 research (likely: add hidden username field, add `id` attribute, ensure correct `autocomplete` values)
2. Keep changes minimal - only touch what's necessary

### Verification
- Build the project (`make build-cli`)
- Run `swe-swe up` and load the login page
- Test with 1Password on desktop
- Test on iOS Safari (if available)
- Test on Android (if available)
- Regression check: confirm form still submits correctly and authentication works

---

## Research Findings

*(To be filled in during Phase 1)*

## Changes Made

*(To be filled in during Phase 2)*
