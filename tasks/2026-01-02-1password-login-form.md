# Task: Fix Password Managers Not Recognizing Login Form

**Date**: 2026-01-02
**Status**: Phase 1 Complete

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

### Sources
- [1Password Developer: Compatible Website Design](https://developer.1password.com/docs/web/compatible-website-design/)
- [1Password Support: Unsecured HTTP Page Alert](https://support.1password.com/unsecured-webpage-alert/)
- [Apple: Password AutoFill Documentation](https://developer.apple.com/documentation/security/password-autofill)
- [Android: Autofill Framework](https://developer.android.com/identity/autofill)
- [hidde.blog: Making password managers play ball](https://hidde.blog/making-password-managers-play-ball-with-your-login-form/)

### 1Password Requirements
- Use unique `id` or `name` for every field
- Enclose `<input>` fields in `<form>` elements
- Group related fields (username + password) in the same form
- Every field should have a `<label>` element with `for` attribute
- Use `autocomplete` attributes (`username`, `current-password`, `new-password`)
- Avoid dynamically adding/removing fields

### iOS/Safari Requirements
- `autocomplete="current-password"` for password fields
- `autocomplete="username"` for username fields
- Works on multi-page login flows if autocomplete is explicit
- HTTP pages show warnings but autofill still works

### Android Requirements
- Uses standard HTML autocomplete attributes
- Chrome 135+ (2025) supports third-party password managers natively
- Relies on Android Autofill Framework

### HTTP vs HTTPS
- 1Password warns on HTTP pages but does NOT block autofill
- User can still choose to fill credentials
- iOS autofill works on HTTP local addresses
- **Not a blocker** - SSL is a deployment concern, not HTML markup

### Gaps in Current Form

| Requirement | Current Status | Issue |
|-------------|---------------|-------|
| Username field | ❌ Missing | Password managers expect username + password pairs |
| `id` attribute on password | ❌ Missing | Only has `name`, should also have `id` |
| `<label>` element | ❌ Missing | 1Password recommends labels for every field |
| `autocomplete` attribute | ✅ Has `current-password` | Already correct |
| Form structure | ✅ Proper `<form>` | Already correct |

### Recommended Fix

Add a hidden username field and `id` attribute:

```html
<form method="POST" action="/swe-swe-auth/login">
    <input type="hidden" name="redirect" value="...">
    <input type="text" name="username" id="username" value="admin" autocomplete="username" style="display:none">
    <input type="password" name="password" id="password" autocomplete="current-password" placeholder="Password" required>
    <button type="submit">Login</button>
</form>
```

**Notes:**
- Hidden username with `value="admin"` gives password managers an anchor
- Using `style="display:none"` instead of `type="hidden"` because some password managers ignore hidden inputs
- `id` attributes help password managers identify fields
- Labels omitted since username is hidden and password has placeholder (minimal change)

## Changes Made

*(To be filled in during Phase 2)*
