import { test, expect } from './_helpers/reaper.js';
import crypto from 'crypto';
import { openSessionViaPost } from './_helpers/sessions.js';

// Auth cookie comes from the suite-wide storageState (see playwright.config.js
// + global-setup.js); no per-test login is needed.

async function waitForUi(page, predicate) {
  return page.waitForFunction(predicate, null, { timeout: 60_000 });
}

async function openSession(page) {
  const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'terminal' });
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  // The set_credentials WS message is silently dropped if the socket is not
  // OPEN yet; wait until the sessionUUID has been delivered (proxy for "WS
  // init message round-trip completed").
  await waitForUi(page, () => window.terminalUI && window.terminalUI.sessionUUID);
  return uuid;
}

async function openSettings(page) {
  const settingsBtn = page.locator('.terminal-ui__settings-btn').first();
  await settingsBtn.click();
  await page.locator('.settings-panel:not([hidden])').waitFor({ timeout: 5_000 });
}

// Phase A reshaped Settings into a sidebar+tabs layout. The credentials and
// SSH controls live behind data-tab="git" and data-tab="ssh" panes that are
// hidden until the user clicks the nav item. Tests that touch those controls
// must switch tabs explicitly; otherwise fill/toBeVisible/etc. fail on the
// hidden ancestor.
async function switchSettingsTab(page, tab) {
  await page.locator(`.settings-panel__nav-item[data-tab="${tab}"]`).click();
  await page.locator(`.settings-panel__pane[data-pane="${tab}"]:not([hidden])`).waitFor({ timeout: 5_000 });
}

async function closeSettings(page) {
  const closeBtn = page.locator('.settings-panel__close').first();
  await closeBtn.click();
  // Panel uses the `hidden` HTML attribute; locator state 'hidden' resolves
  // when the element is either detached or hidden via that attribute.
  await page.locator('.settings-panel').waitFor({ state: 'hidden', timeout: 5_000 });
}

test.describe('per-session git credentials UI', () => {
  test('save round-trip: form -> WS -> credentials_stored ack -> status + localStorage', async ({ page }) => {
    await openSession(page);

    // Clear any stale stored creds left over by other tests in this origin.
    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:'))
        .forEach(k => localStorage.removeItem(k));
    });

    await openSettings(page);
    await switchSettingsTab(page, 'git');

    // The credentials section is rendered in the git tab pane.
    const credsSection = page.locator('.settings-panel__pane[data-pane="git"]');
    await expect(credsSection).toBeVisible();

    // Default host is github.com; status starts as "Not yet sent to server."
    const hostInput = page.locator('#settings-cred-host');
    await expect(hostInput).toHaveValue('github.com');
    const status = page.locator('#settings-cred-status');
    await expect(status).toHaveText(/Not yet sent to server/);

    // Fill the form with test values. Token field is type=password.
    await page.fill('#settings-cred-username', 'x-access-token');
    await page.fill('#settings-cred-token', 'ghp_test_token_e2e_xyz');
    await page.fill('#settings-cred-name', 'E2E Tester');
    await page.fill('#settings-cred-email', 'e2e@example.com');
    await expect(page.locator('#settings-cred-token')).toHaveAttribute('type', 'password');

    // Capture every status-text change in the page so we can assert that
    // "Sending..." was rendered at some point, without racing Playwright's
    // poll loop against the WS ack. Polling expect() can miss a single-frame
    // transition; a MutationObserver running in the page sees every value.
    await page.evaluate(() => {
      window.__credStatusHistory = [];
      const el = document.getElementById('settings-cred-status');
      window.__credStatusObserver = new MutationObserver(() => {
        window.__credStatusHistory.push(el.textContent);
      });
      window.__credStatusObserver.observe(el, {
        childList: true,
        subtree: true,
        characterData: true,
      });
    });

    // Click Save. The handler writes localStorage AND sends a WS message.
    await page.click('#settings-cred-save');

    // Wait for the credentials_stored ack to update _credsStoredHosts.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return Array.isArray(ui?._credsStoredHosts) && ui._credsStoredHosts.includes('github.com');
    });

    // Status now reflects the server-confirmed host list (no values).
    await expect(status).toHaveText(/Stored on server for: github\.com/);

    // Verify the transient "Sending..." state was rendered between click
    // and ack. Reading from the recorded history is race-proof: even if
    // the ack overwrote the text within one frame, the observer captured it.
    const statusHistory = await page.evaluate(() => {
      window.__credStatusObserver?.disconnect();
      return window.__credStatusHistory || [];
    });
    expect(statusHistory.some(t => /Sending\.\.\./.test(t))).toBe(true);
    // ok state attribute is set so the UI can style it green.
    await expect(status).toHaveAttribute('data-state', 'ok');

    // localStorage round-trip: the bag is keyed by host and includes only
    // the values the user entered. The server-side response is NOT stored
    // here -- we only confirm the browser-side write.
    const stored = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-creds:github.com');
      return raw ? JSON.parse(raw) : null;
    });
    expect(stored).toEqual({
      username: 'x-access-token',
      token: 'ghp_test_token_e2e_xyz',
      name: 'E2E Tester',
      email: 'e2e@example.com',
    });

    // Close + re-open: the form should be pre-filled from localStorage so
    // the user can see what's saved and re-Save without re-typing.
    await closeSettings(page);
    await openSettings(page);
    await switchSettingsTab(page, 'git');

    await expect(page.locator('#settings-cred-host')).toHaveValue('github.com');
    await expect(page.locator('#settings-cred-username')).toHaveValue('x-access-token');
    await expect(page.locator('#settings-cred-token')).toHaveValue('ghp_test_token_e2e_xyz');
    await expect(page.locator('#settings-cred-name')).toHaveValue('E2E Tester');
    await expect(page.locator('#settings-cred-email')).toHaveValue('e2e@example.com');
  });

  test('save with empty token shows validation error and does not send WS', async ({ page }) => {
    await openSession(page);

    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:'))
        .forEach(k => localStorage.removeItem(k));
      // Reset the in-memory hosts list too so prior tests don't leak.
      if (window.terminalUI) window.terminalUI._credsStoredHosts = [];
    });

    await openSettings(page);
    await switchSettingsTab(page, 'git');

    // Token blank, host defaulted to github.com.
    await page.fill('#settings-cred-token', '');
    await page.click('#settings-cred-save');

    const status = page.locator('#settings-cred-status');
    await expect(status).toHaveText(/Host and token are required/);
    await expect(status).toHaveAttribute('data-state', 'err');

    // No ack arrived because no WS message was sent.
    const hosts = await page.evaluate(() => window.terminalUI?._credsStoredHosts || []);
    expect(hosts).not.toContain('github.com');
  });

  test('switching host repopulates form from per-host localStorage', async ({ page }) => {
    await openSession(page);

    // Seed two hosts directly in localStorage so we can isolate the host-
    // change branch from the save-then-reopen branch already covered above.
    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:'))
        .forEach(k => localStorage.removeItem(k));
      localStorage.setItem('swe-swe-creds:github.com', JSON.stringify({
        username: 'x-access-token', token: 'ghp_github_x', name: 'GH User', email: 'gh@example.com',
      }));
      localStorage.setItem('swe-swe-creds:gitlab.com', JSON.stringify({
        username: 'gitlab-user', token: 'glpat-gitlab-y', name: 'GL User', email: 'gl@example.com',
      }));
    });

    await openSettings(page);
    await switchSettingsTab(page, 'git');

    // Default host github.com pre-fills with GH values.
    await expect(page.locator('#settings-cred-username')).toHaveValue('x-access-token');
    await expect(page.locator('#settings-cred-token')).toHaveValue('ghp_github_x');

    // Change the host to gitlab.com and fire the change event. The handler
    // re-runs populateCredentialsSection() and pulls in the GitLab bag.
    await page.fill('#settings-cred-host', 'gitlab.com');
    await page.locator('#settings-cred-host').dispatchEvent('change');

    await expect(page.locator('#settings-cred-username')).toHaveValue('gitlab-user');
    await expect(page.locator('#settings-cred-token')).toHaveValue('glpat-gitlab-y');
    await expect(page.locator('#settings-cred-name')).toHaveValue('GL User');
    await expect(page.locator('#settings-cred-email')).toHaveValue('gl@example.com');
  });

  test('Author fields go readonly when session WorkDir has local .git/config user.*', async ({ page }) => {
    // Connect once so the session row exists with WorkDir=/workspace
    // (the e2e container's /workspace has user.name="E2E Test" /
    // user.email="e2e@test.local" baked into .git/config). Then reload
    // the page so the server can include LocalUser{Name,Email} in the
    // index template.
    const uuid = await openSession(page);
    await page.reload();
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    // The data-* attributes flow from the index template into <terminal-ui>.
    const terminalUi = page.locator('terminal-ui');
    await expect(terminalUi).toHaveAttribute('data-local-user-name', 'E2E Test');
    await expect(terminalUi).toHaveAttribute('data-local-user-email', 'e2e@test.local');

    await openSettings(page);
    await switchSettingsTab(page, 'git');

    // populateCredentialsSection sets the values from local config and
    // marks the inputs readonly.
    const nameInput = page.locator('#settings-cred-name');
    const emailInput = page.locator('#settings-cred-email');
    await expect(nameInput).toHaveValue('E2E Test');
    await expect(emailInput).toHaveValue('e2e@test.local');
    await expect(nameInput).toHaveAttribute('readonly', '');
    await expect(emailInput).toHaveAttribute('readonly', '');

    // Explainer is inserted; mention .git/config so the user knows where
    // the override comes from.
    const explainer = page.locator('#settings-cred-local-override');
    await expect(explainer).toBeVisible();
    await expect(explainer).toContainText('.git/config');
  });
});

// Test ed25519 key generated for e2e only; never used outside this suite.
// Fingerprint: SHA256:YNOCH+zR5nOPiv90YpfKRkXMeHPseO6WdGegIzLmj+U
const TEST_SIGNING_KEY_PEM = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACA/lEFqfZd6xhkn6HW5K7QjLEQFkV34Tq9W0wIAnx0ogQAAAJghU4NbIVOD
WwAAAAtzc2gtZWQyNTUxOQAAACA/lEFqfZd6xhkn6HW5K7QjLEQFkV34Tq9W0wIAnx0ogQ
AAAEC0jJWbHEAJ8zCd60hZS5xa43xt1Qh3bkj7PQQRYXxRFT+UQWp9l3rGGSfodbkrtCMs
RAWRXfhOr1bTAgCfHSiBAAAAEGUyZS10ZXN0QHN3ZS1zd2UBAgMEBQ==
-----END OPENSSH PRIVATE KEY-----
`;

const TEST_SIGNING_FINGERPRINT = 'SHA256:YNOCH+zR5nOPiv90YpfKRkXMeHPseO6WdGegIzLmj+U';

test.describe('per-session SSH commit signing UI', () => {
  test('save round-trip: signing form -> WS -> credentials_stored ack with fingerprint -> status + localStorage', async ({ page }) => {
    await openSession(page);

    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:') || k.startsWith('swe-swe-signing:'))
        .forEach(k => localStorage.removeItem(k));
      if (window.terminalUI) {
        window.terminalUI._credsStoredHosts = [];
        window.terminalUI._signingFingerprint = '';
      }
    });

    await openSettings(page);
    await switchSettingsTab(page, 'ssh');

    // Signing section is rendered in the SSH commit-signing tab.
    const signingSection = page.locator('.settings-panel__pane[data-pane="ssh"]');
    await expect(signingSection).toBeVisible();

    // Fields the form must expose. Test-first: this fails until the
    // implementation lands the matching ids.
    const keyTextarea = page.locator('#settings-cred-signing-key');
    const passphraseInput = page.locator('#settings-cred-signing-passphrase');
    const labelInput = page.locator('#settings-cred-signing-label');
    await expect(keyTextarea).toBeVisible();
    await expect(passphraseInput).toBeVisible();
    await expect(labelInput).toBeVisible();
    await expect(passphraseInput).toHaveAttribute('type', 'password');

    // Phase A split signing onto its own Save button + set_signing_key WS
    // message, so we no longer need to fill an HTTPS token to satisfy the
    // unified _saveCredentials path.
    await page.fill('#settings-cred-signing-key', TEST_SIGNING_KEY_PEM);
    await page.fill('#settings-cred-signing-label', 'e2e-signing-key');

    // Save key. Triggers set_signing_key over the WS.
    await page.click('#settings-cred-signing-save');

    // Wait for the signing_key_stored ack to populate the fingerprint.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return typeof ui?._signingFingerprint === 'string' && ui._signingFingerprint.startsWith('SHA256:');
    });

    const got = await page.evaluate(() => window.terminalUI._signingFingerprint);
    expect(got).toBe(TEST_SIGNING_FINGERPRINT);

    // Status reflects the registered key fingerprint somewhere visible
    // to the user.
    const signingStatus = page.locator('#settings-cred-signing-status');
    await expect(signingStatus).toContainText('SHA256:');

    // localStorage round-trip: the key + label persist; passphrase
    // does NOT persist (consumed once on save, not retained).
    const stored = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-signing:default');
      return raw ? JSON.parse(raw) : null;
    });
    expect(stored).toMatchObject({
      privateKey: TEST_SIGNING_KEY_PEM,
      label: 'e2e-signing-key',
    });
    expect(stored).not.toHaveProperty('passphrase');

    // Close + re-open: key + label rehydrate from localStorage.
    await closeSettings(page);
    await openSettings(page);
    await switchSettingsTab(page, 'ssh');
    await expect(page.locator('#settings-cred-signing-key')).toHaveValue(TEST_SIGNING_KEY_PEM);
    await expect(page.locator('#settings-cred-signing-label')).toHaveValue('e2e-signing-key');
    // Passphrase is NEVER rehydrated -- it isn't in localStorage to
    // rehydrate from.
    await expect(page.locator('#settings-cred-signing-passphrase')).toHaveValue('');
  });

  test('end-to-end: git commit -S in the session terminal produces an ssh-signed commit', async ({ page }) => {
    // Smoke that the entire stack works: WS set_credentials carries
    // the signing key, the broker stores it, writeSessionGitconfig
    // emits the [gpg] / [commit] blocks, the session shell sees the
    // new gitconfig, git invokes git-sign-swe-swe, the wrapper dials
    // the broker, sign-ssh signs, the wrapper writes the .sig, and
    // git embeds it as a gpgsig field in the commit object.
    //
    // Assertion: `git cat-file -p HEAD` of the just-made commit
    // contains a "-----BEGIN SSH SIGNATURE-----" block. We don't
    // verify the signature here -- that's covered by the Go-side
    // SSHSIG round-trip unit test. This test catches *wiring* errors
    // between the layers, which the unit tests can't.
    //
    // Session shape: `?assistant=shell` gives a plain bash so we can
    // type git commands into it. Other test sessions use opencode
    // which grabs the terminal with its own TUI -- not usable here.
    // Using a single session means the WS that saves the signing key
    // is the same sid the broker resolves for the helper's connect,
    // sidestepping the sibling-session creds gap (research addendum
    // 2026-04-26 finding #1).
    const uuid = await openSessionViaPost(page, { assistant: 'shell' });
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
    await waitForUi(page, () => window.terminalUI && window.terminalUI.ws && window.terminalUI.ws.readyState === 1);
    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:') || k.startsWith('swe-swe-signing:'))
        .forEach(k => localStorage.removeItem(k));
      if (window.terminalUI) {
        window.terminalUI._credsStoredHosts = [];
        window.terminalUI._signingFingerprint = '';
      }
    });

    // Save signing key + author identity by sending the WS message
    // directly. Shell-mode sessions don't ship the Settings panel
    // chrome (preset=single, no settings button visible), so going
    // through the form would hang. The Phase 4 UI test covers that
    // path against the chat session; here we want the back-end smoke.
    await page.evaluate((pem) => {
      window.terminalUI.sendJSON({
        type: 'set_credentials',
        data: {
          host: 'github.com',
          username: 'x-access-token',
          token: 'ghp_e2e_phase5_smoke',
          name: 'Phase5 Smoke',
          email: 'phase5@example.com',
          signing_private_key_pem: pem,
          signing_passphrase: '',
          signing_key_label: 'phase5-key',
        },
      });
    }, TEST_SIGNING_KEY_PEM);
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return typeof ui?._signingFingerprint === 'string' && ui._signingFingerprint.startsWith('SHA256:');
    });

    // Drive the session shell via raw WS bytes. The terminal-ui
    // forwards them straight to the PTY -- same path the user takes
    // when typing into the terminal pane.
    //
    // The PTY echoes typed input back into the xterm buffer, so any
    // literal sentinel in the command appears once just from being
    // typed. To distinguish "command was typed" from "command ran",
    // we put the sentinel inside a printf format string with hex
    // escapes -- the typed line shows the source `\x5f` while the
    // executed line shows the resolved underscore `_`. Wait for the
    // resolved form so we know bash actually ran the pipeline.
    const SENTINEL = 'PHASE5DONE' + crypto.randomUUID().slice(0, 8).replace(/-/g, '');
    // Source form interleaves \x5fs. Resolved form is the same
    // string with the \x5f sequences turned into underscores.
    const sourceForm = SENTINEL.replace(/^([A-Z0-9]{5})/, '$1\\x5f');
    const resolvedForm = SENTINEL.replace(/^([A-Z0-9]{5})/, '$1_');
    const cmd = [
      'cd /tmp',
      'rm -rf signtest',
      'mkdir signtest',
      'cd signtest',
      'git init -q',
      'git commit --allow-empty -S -m smoke 2>&1',
      'echo === HEAD ===',
      'git cat-file -p HEAD 2>&1 || true',
      'echo === VERIFY ===',
      'git log -1 --show-signature 2>&1 || true',
      "printf '" + sourceForm + "\\n'",
    ].join(' && ');
    await page.evaluate((c) => {
      const enc = new TextEncoder();
      window.terminalUI.ws.send(enc.encode(c + '\n'));
    }, cmd);

    // Wait for the resolved sentinel form to appear (proof bash
    // executed the printf). Cap at 30s -- warm container is ~1s.
    await page.waitForFunction((s) => {
      const term = window.terminalUI?.term;
      if (!term) return false;
      const buf = term.buffer.active;
      for (let y = 0; y < buf.length; y++) {
        const line = buf.getLine(y);
        if (line && line.translateToString().includes(s)) return true;
      }
      return false;
    }, resolvedForm, { timeout: 30_000 });

    // Pull the full visible buffer back to JS for assertions.
    const text = await page.evaluate(() => {
      const term = window.terminalUI.term;
      const buf = term.buffer.active;
      let out = '';
      for (let y = 0; y < buf.length; y++) {
        const line = buf.getLine(y);
        if (line) out += line.translateToString().trimEnd() + '\n';
      }
      return out;
    });

    // The smoke fails loudly if any of these show up.
    expect(text, text).not.toMatch(/error: gpg failed to sign/i);
    expect(text, text).not.toMatch(/error: cannot run git-sign-swe-swe/i);
    expect(text, text).not.toMatch(/refusing to serve - not invoked by git/);

    // The proof: the commit object (printed by git cat-file -p HEAD)
    // must contain a gpgsig field with an SSH signature block.
    expect(text, text).toMatch(/gpgsig\s+-----BEGIN SSH SIGNATURE-----/);
    expect(text, text).toContain('-----END SSH SIGNATURE-----');

    // Verification path (F1: git-sign-swe-swe -Y verify, F2: allowedSignersFile).
    // Before F1, `git log --show-signature` died with
    // "git-sign-swe-swe: only -Y sign is supported". Before F2,
    // ssh-keygen complained that gpg.ssh.allowedSignersFile needed to be
    // configured. After both fixes, ssh-keygen recognises the principal
    // from the per-session allowed_signers file and verifies the signature.
    expect(text, text).not.toMatch(/only -Y sign is supported/);
    expect(text, text).not.toMatch(/allowedSignersFile needs to be configured/i);
    expect(text, text).toMatch(/Good "git" signature for phase5@example.com/);
  });

  test('auto-restore: trusted (origin, init_sha) auto-sends signing key on next session', async ({ page }) => {
    // After the user explicitly trusts a (origin, init_sha) pair via
    // the "Trust this browser to auto-load..." confirm dialog, opening a
    // new session at the same origin should send the bound key to the
    // server without any UI interaction. The server's _signingFingerprint
    // populates from the signing_key_stored ack, so we watch for that.
    await openSession(page);
    // Sanity: the page exposes the workdir's init-commit SHA. Without
    // it, no trust binding is possible -- skip rather than asserting
    // false negatives.
    const initSha = await page.evaluate(() => document.querySelector('terminal-ui')?.dataset?.initSha || '');
    test.skip(!initSha, 'workdir is not a git repo with commits; cannot test init_sha trust binding');

    // Clean slate: nothing in localStorage and no prior fingerprint.
    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:') || k.startsWith('swe-swe-signing:') || k.startsWith('swe-swe:signing-'))
        .forEach(k => localStorage.removeItem(k));
      if (window.terminalUI) window.terminalUI._signingFingerprint = '';
    });

    // First session: explicit Save flow.
    await openSettings(page);
    await switchSettingsTab(page, 'ssh');
    await page.fill('#settings-cred-signing-key', TEST_SIGNING_KEY_PEM);
    await page.fill('#settings-cred-signing-label', 'auto-restore-key');
    page.once('dialog', d => d.accept());
    await page.click('#settings-cred-signing-save');
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return typeof ui?._signingFingerprint === 'string' && ui._signingFingerprint.startsWith('SHA256:');
    });
    const fpAfterSave = await page.evaluate(() => window.terminalUI._signingFingerprint);
    expect(fpAfterSave).toBe(TEST_SIGNING_FINGERPRINT);

    // Trust entry was written by the dialog accept above.
    const trust = await page.evaluate((initSha) => {
      const key = 'swe-swe:signing-trust:' + window.location.origin + '|' + initSha;
      const raw = localStorage.getItem(key);
      return raw ? JSON.parse(raw) : null;
    }, initSha);
    expect(trust).not.toBeNull();
    expect(trust.fingerprint).toBe(TEST_SIGNING_FINGERPRINT);

    // Second session: reload. The on-open auto-restore should re-send
    // the bound key without the user opening Settings.
    await page.evaluate(() => {
      if (window.terminalUI) window.terminalUI._signingFingerprint = '';
    });
    await page.reload();
    await waitForUi(page, () => window.terminalUI && window.terminalUI.ws && window.terminalUI.ws.readyState === 1);
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return typeof ui?._signingFingerprint === 'string' && ui._signingFingerprint.startsWith('SHA256:');
    });
    const fpAfterReload = await page.evaluate(() => window.terminalUI._signingFingerprint);
    expect(fpAfterReload).toBe(TEST_SIGNING_FINGERPRINT);
  });

  test('Local .git/config signing overrides surface as a warning in the SSH tab', async ({ page }) => {
    // Server reads <workdir>/.git/config and exposes any signing-side
    // override via data-local-gpg-overrides on <terminal-ui>. The JS
    // renders a warning banner in the SSH Signing pane so the user
    // can fix the local config before they hit `gpg failed to sign`.
    // Driving the Go side end-to-end would need a controlled workdir
    // with seeded overrides; instead we exercise the rendering hook
    // by setting the data-attribute on the element and asserting the
    // banner appears (and disappears) accordingly.
    await openSession(page);
    await page.evaluate(() => {
      const el = document.querySelector('terminal-ui');
      el.dataset.localGpgOverrides = 'gpg.format=openpgp, commit.gpgSign=true';
    });
    await openSettings(page);
    await switchSettingsTab(page, 'ssh');

    const banner = page.locator('#settings-cred-signing-local-overrides');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('gpg.format=openpgp');
    await expect(banner).toContainText('commit.gpgSign=true');
    await expect(banner).toContainText('git config --local --unset');

    // Clear the override -> banner gone next time the tab is refreshed.
    await page.evaluate(() => {
      document.querySelector('terminal-ui').dataset.localGpgOverrides = '';
    });
    await switchSettingsTab(page, 'profile');
    await switchSettingsTab(page, 'ssh');
    await expect(page.locator('#settings-cred-signing-local-overrides')).toHaveCount(0);
  });

  test('Forget on this device: clears trust so reload no longer auto-sends', async ({ page }) => {
    await openSession(page);
    const initSha = await page.evaluate(() => document.querySelector('terminal-ui')?.dataset?.initSha || '');
    test.skip(!initSha, 'workdir is not a git repo with commits; cannot test init_sha trust binding');

    // Seed: pretend we already trusted this (origin, init_sha) with the test key.
    await page.evaluate((opts) => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:') || k.startsWith('swe-swe-signing:') || k.startsWith('swe-swe:signing-'))
        .forEach(k => localStorage.removeItem(k));
      localStorage.setItem('swe-swe:signing-key:' + opts.fp, JSON.stringify({
        pem: opts.pem,
        label: 'forget-test',
        fingerprint: opts.fp,
        savedAt: Date.now(),
      }));
      localStorage.setItem('swe-swe:signing-trust:' + window.location.origin + '|' + opts.initSha, JSON.stringify({
        fingerprint: opts.fp,
        savedAt: Date.now(),
      }));
      if (window.terminalUI) window.terminalUI._signingFingerprint = '';
    }, { fp: TEST_SIGNING_FINGERPRINT, pem: TEST_SIGNING_KEY_PEM, initSha });

    // Open Settings -> SSH; the Forget button is rendered because a
    // trust entry exists for this repo. Click it.
    await openSettings(page);
    await switchSettingsTab(page, 'ssh');
    const forgetBtn = page.locator('#settings-cred-signing-forget');
    await expect(forgetBtn).toBeVisible();
    await forgetBtn.click();

    // Trust entry is gone from localStorage.
    const trustAfter = await page.evaluate((initSha) => {
      const key = 'swe-swe:signing-trust:' + window.location.origin + '|' + initSha;
      return localStorage.getItem(key);
    }, initSha);
    expect(trustAfter).toBeNull();

    // Reload: nothing auto-sends, fingerprint stays empty.
    await page.reload();
    await waitForUi(page, () => window.terminalUI && window.terminalUI.ws && window.terminalUI.ws.readyState === 1);
    // Give the auto-restore code a chance to fire (it would have by now
    // since onopen runs synchronously on connect).
    await page.waitForTimeout(500);
    const fpAfterReload = await page.evaluate(() => window.terminalUI._signingFingerprint || '');
    expect(fpAfterReload).toBe('');
  });
});
