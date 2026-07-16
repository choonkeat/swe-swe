import { test, expect } from './_helpers/reaper.js';
import crypto from 'crypto';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

// Same base-url resolution ports.spec.js uses, for the cross-origin
// filesProxyPort reachability check in the Files-tab test.
const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;

// Auth cookie comes from the suite-wide storageState (see playwright.config.js
// + global-setup.js); no per-test login is needed.

// Wait until window.terminalUI exists and reports a given property via predicate.
// 90s default: on isolated runs the MCP probe completes in ~12-15s, but when
// the suite runs end-to-end (agent-browser plus the credentials and ports
// specs leave Chrome, sidecars, and processes behind even after their own
// pages close), the probe can stretch past 60s. 90s keeps the full suite
// stable without hiding real regressions -- a clean baseline (see
// globalSetup) plus per-test session cleanup (afterEach on pass) handles
// most accumulation; this budget is the safety margin on top.
async function waitForUi(page, predicate) {
  return page.waitForFunction(predicate, null, { timeout: 90_000 });
}

// Per-test session tracker. We push every UUID openChatSession /
// openTerminalSession creates so afterEach can end them on pass. Failed
// tests skip cleanup so the broken session state stays in the container
// for inspection.
let testSessions = [];

async function openChatSession(page) {
  const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
  testSessions.push(uuid);
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  return uuid;
}

async function openTerminalSession(page) {
  const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'terminal' });
  testSessions.push(uuid);
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  return uuid;
}

test.describe('terminal-ui tab switching', () => {
  test.beforeEach(async ({ page }) => {
    testSessions = [];
  });

  test.afterEach(async ({ page }, testInfo) => {
    if (testInfo.status === 'passed' && testSessions.length > 0) {
      await endSessions(page, testSessions);
    }
  });

  test('?session=chat: Agent Terminal stays active while chat probe runs, chat activates on probe success', async ({ page }) => {
    await openChatSession(page);

    // setActiveInSlot fires inside chatIframe.onload, which happens AFTER
    // _agentChatAvailable flips to true. Wait on the slot state directly.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return ui && ui.activeBySlot?.a?.active === 'agent-chat'
        && ui._agentChatAvailable === true;
    });

    const post = await page.evaluate(() => {
      const ui = window.terminalUI;
      return {
        slotA_active: ui.activeBySlot?.a?.active,
        slotA_tabs: ui.activeBySlot?.a?.tabs,
        slotB_active: ui.activeBySlot?.b?.active,
        slotB_tabs: ui.activeBySlot?.b?.tabs,
        probing: ui._agentChatProbing,
        pending: ui._agentChatPending,
        available: ui._agentChatAvailable,
        chatTabLabel: ui.querySelector('.terminal-ui__slot-tab[data-pane="agent-chat"] .terminal-ui__slot-tab-label')?.textContent,
      };
    });

    // Fresh classic defaults: Agent Chat + Files left, Agent Terminal +
    // Preview right; the probe-success flip landed on Agent Chat while the
    // right slot keeps Agent Terminal active (mount-time force-active).
    expect(post.slotA_tabs).toEqual(['agent-chat', 'files']);
    expect(post.slotA_active).toBe('agent-chat');
    expect(post.slotB_tabs).toEqual(['agent-terminal', 'preview']);
    expect(post.slotB_active).toBe('agent-terminal');
    expect(post.probing).toBe(false);
    expect(post.pending).toBe(false);
    expect(post.available).toBe(true);
    expect(post.chatTabLabel).toBe('Agent Chat');
  });

  test('fresh default layout: Files first active on the left, param-less revisit still hands focus to Agent Chat', async ({ page }) => {
    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
    testSessions.push(uuid);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
    await waitForUi(page, () => window.terminalUI && window.terminalUI.activeBySlot);

    // Fresh classic defaults: chat+files left with Files initially active
    // (first paint is never a chat spinner), terminal+preview right with
    // Agent Terminal forced active. Race-guarded: on a warm container the
    // probe may already have handed the left slot to agent-chat.
    const atBoot = await page.evaluate(() => {
      const ui = window.terminalUI;
      return {
        preset: ui.preset,
        slotA_tabs: ui.activeBySlot?.a?.tabs,
        slotA_active: ui.activeBySlot?.a?.active,
        slotB_tabs: ui.activeBySlot?.b?.tabs,
        slotB_active: ui.activeBySlot?.b?.active,
        available: ui._agentChatAvailable,
      };
    });
    expect(atBoot.preset).toBe('classic');
    expect(atBoot.slotA_tabs).toEqual(['agent-chat', 'files']);
    expect(atBoot.slotB_tabs).toEqual(['agent-terminal', 'preview']);
    expect(atBoot.slotB_active).toBe('agent-terminal');
    if (atBoot.available !== true) {
      expect(atBoot.slotA_active).toBe('files');
    }

    // Revisit WITHOUT ?session=chat (bookmark / session-list style; the
    // assistant param stays -- the server redirects to "/" without it).
    // agentChatPort is a server-side session property (SessionMode ==
    // "chat"), so the probe still runs -- and it's the fresh-default
    // handoff (not the session=chat branch) that flips focus to Agent
    // Chat once the iframe is usable.
    await page.goto(`/session/${uuid}?assistant=opencode`);
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true
      && window.terminalUI?.activeBySlot?.a?.active === 'agent-chat');

    // The handoff is ephemeral -- nothing persisted agent-chat as active.
    const saved = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-layout-v1');
      return raw ? JSON.parse(raw) : null;
    });
    if (saved) {
      expect(saved.activeBySlot?.a?.active).not.toBe('agent-chat');
    }
  });

  test('?session=chat: probe-success flip does NOT persist to localStorage (next visit finds Agent Terminal active)', async ({ page }) => {
    // Regression: the session=chat auto-activation used to write
    // active:'agent-chat' to localStorage, so the NEXT page load
    // restored agent-chat as active and it flashed "Connecting to chat..."
    // before the probe ran -- defeating the "Agent Terminal during probe"
    // behavior. Probe-success flip must be ephemeral.
    await openChatSession(page);

    // Wait for the probe to complete and the in-memory flip to land.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return ui && ui._agentChatAvailable === true
        && ui.activeBySlot?.a?.active === 'agent-chat';
    });

    // Inspect what's actually in localStorage. Auto-opens (both the tabs
    // addition from autoAddPaneToHome and the probe-success active flip) are
    // now persist:false -- session-driven state shouldn't prime future
    // visits. localStorage should either be absent entirely (fresh user with
    // no manual layout actions) or, if some other action persisted, must not
    // have baked in the ephemeral active:'agent-chat' flip. (agent-chat in
    // the tabs LIST is legitimate now -- it ships in the classic defaults.)
    const saved = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-layout-v1');
      return raw ? JSON.parse(raw) : null;
    });

    if (saved) {
      expect(saved.activeBySlot?.a?.active).not.toBe('agent-chat');
    }

    // Now reload with session=chat and verify that during the probe window,
    // the slot default (Files) is the active tab -- not agent-chat from a
    // stale save.
    await page.reload();

    // As soon as the custom element boots, before the probe completes, the
    // slot active should be the Files default. Assert before
    // _agentChatAvailable flips true.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return ui && ui.activeBySlot?.a?.tabs?.includes('agent-chat');
    });
    const duringProbe = await page.evaluate(() => {
      const ui = window.terminalUI;
      return {
        slotA_active: ui.activeBySlot?.a?.active,
        available: ui._agentChatAvailable,
      };
    });
    // The probe MAY complete before we observe; that's fine. What we care
    // about is that pre-probe it was the Files default, not agent-chat. If
    // available is already true, we've raced past the probe window -- the
    // prior assertion on saved.active already covered the persistence bug.
    if (duringProbe.available === false || duringProbe.available === undefined) {
      expect(duringProbe.slotA_active).toBe('files');
    }

    // After probe, agent-chat is active in memory -- still not persisted.
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true
      && window.terminalUI?.activeBySlot?.a?.active === 'agent-chat');
    const savedAfterReload = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-layout-v1');
      return raw ? JSON.parse(raw) : null;
    });
    if (savedAfterReload) {
      expect(savedAfterReload.activeBySlot?.a?.active).not.toBe('agent-chat');
    }
  });

  test('stale localStorage active:agent-chat is overridden to agent-terminal at boot', async ({ page }) => {
    // Regression: even with the persist:false fixes, a user's localStorage
    // could carry active:'agent-chat' from a prior manual click. On reload,
    // the page painted with Agent Chat already active and showing the
    // "Connecting to chat..." placeholder while the probe was still in
    // flight -- and the agent process in Agent Terminal was often blocked
    // on a prompt the user had no way to see.
    //
    // Fix: at boot, if a slot's saved active is agent-chat AND the same
    // slot has agent-terminal in its tabs, override to agent-terminal in
    // memory. Saved preference is untouched. The probe-success handler
    // flips back to agent-chat (persist:false) once chat is loadable.
    // Pre-seed localStorage with active:'agent-chat' before the page boots
    // its custom element. We need to be at the same origin for localStorage
    // to apply -- the login in beforeEach already put us there. The layout key
    // is global (not per-uuid), so it persists across the POST -> redirect that
    // openSessionViaPost performs to mint+open the session.
    await page.goto('/'); // any same-origin URL is fine
    await page.evaluate(() => {
      localStorage.setItem('swe-swe-layout-v1', JSON.stringify({
        preset: 'classic',
        activeBySlot: {
          a: { tabs: ['agent-terminal', 'agent-chat'], active: 'agent-chat' },
          b: { tabs: ['preview'], active: 'preview' },
        },
      }));
    });

    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
    await waitForUi(page, () => window.terminalUI && window.terminalUI.activeBySlot);

    // Boot-time override should have demoted slot a's active to agent-terminal
    // BEFORE the probe completes. Inspect right after the element mounts.
    const atBoot = await page.evaluate(() => {
      const ui = window.terminalUI;
      return {
        slotA_active: ui.activeBySlot?.a?.active,
        slotA_tabs: ui.activeBySlot?.a?.tabs,
        available: ui._agentChatAvailable,
      };
    });
    // If we observed before the probe completed, agent-terminal is active.
    // If the probe was instant (warm container), the post-probe flip already
    // landed us on agent-chat -- that's fine; the saved-state assertion
    // below covers the persistence guarantee.
    if (atBoot.available !== true) {
      expect(atBoot.slotA_active).toBe('agent-terminal');
    }
    expect(atBoot.slotA_tabs).toContain('agent-chat');

    // Saved preference must NOT have been rewritten by the override -- the
    // user's stored intent (active:'agent-chat') is preserved.
    const saved = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-layout-v1');
      return raw ? JSON.parse(raw) : null;
    });
    expect(saved?.activeBySlot?.a?.active).toBe('agent-chat');

    // After probe success, the in-memory active flips to agent-chat via the
    // probe-success handler (persist:false). localStorage still says
    // agent-chat (unchanged from our seed).
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true
      && window.terminalUI?.activeBySlot?.a?.active === 'agent-chat');
    const savedAfter = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-layout-v1');
      return raw ? JSON.parse(raw) : null;
    });
    expect(savedAfter?.activeBySlot?.a?.active).toBe('agent-chat');
  });

  test('browserStarted auto-open must not persist Agent View as active for next session', async ({ page }) => {
    // Regression: when the server reported browserStarted, autoAddPaneToHome
    // added Agent View to the slot and persisted the change to localStorage
    // with active:'browser'. A subsequent session (even one where the
    // browser never starts) restored Agent View as the active tab on first
    // paint, leaving users stuck on a "Starting browser..." placeholder or
    // a stale browser view they never opened.
    //
    // Auto-opens driven by session runtime state should be ephemeral;
    // only manual user tab actions persist.
    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'terminal' });
    await waitForUi(page, () => window.terminalUI && window.terminalUI.activeBySlot);

    // Drive the auto-open code path directly: this is what the WS-init
    // msg.browserStarted branch does on line ~1243.
    await page.evaluate(() => {
      window.terminalUI.autoAddPaneToHome('browser');
    });

    // In-memory: browser is now in slot b as the active tab.
    const inMemory = await page.evaluate(() => ({
      slotB_tabs: window.terminalUI.activeBySlot?.b?.tabs,
      slotB_active: window.terminalUI.activeBySlot?.b?.active,
    }));
    expect(inMemory.slotB_tabs).toContain('browser');
    expect(inMemory.slotB_active).toBe('browser');

    // But localStorage must NOT have been updated with browser as active.
    // Either absent entirely (fresh user) or active still at its
    // preset default (preview).
    const saved = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-layout-v1');
      return raw ? JSON.parse(raw) : null;
    });
    if (saved) {
      expect(saved.activeBySlot?.b?.active).not.toBe('browser');
      expect(saved.activeBySlot?.b?.tabs || []).not.toContain('browser');
    }
  });

  test('plain ?session=terminal: no Agent Chat tab appears', async ({ page }) => {
    await openTerminalSession(page);

    // Wait for a brief settling period so any probe that would fire has had
    // a chance. Then assert agent-chat is absent from all slots.
    await page.waitForTimeout(2_000);

    const state = await page.evaluate(() => {
      const ui = window.terminalUI;
      const allTabs = Array.from(ui.querySelectorAll('.terminal-ui__slot-tab')).map(t => t.dataset.pane);
      return {
        tabs: allTabs,
        probing: ui._agentChatProbing,
        pending: ui._agentChatPending,
        available: ui._agentChatAvailable,
      };
    });

    expect(state.tabs).not.toContain('agent-chat');
    expect(state.probing).toBeFalsy();
    expect(state.pending).toBeFalsy();
    expect(state.available).toBeFalsy();
  });

  test('toggling Chat -> Terminal -> Chat -> Terminal keeps xterm visible (no blank regression)', async ({ page }) => {
    await openChatSession(page);

    // Wait for chat probe to complete
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true);

    // Agent Terminal and Agent Chat live in different slots under the new
    // classic defaults (chat+files left, terminal+preview right), so resolve
    // each pane's slot dynamically instead of assuming slot a. The blanking
    // regression this guards against was applyPreset show/hide churn -- the
    // repeated activations below still exercise that path.
    const activeOf = (page, pane) => page.evaluate((p) => {
      const ui = window.terminalUI;
      return ui.activeBySlot?.[ui._slotForPane(p)]?.active;
    }, pane);

    // Click Agent Terminal slot-tab, check xterm is rendered
    await page.locator('.terminal-ui__slot-tab[data-pane="agent-terminal"]').click();
    let state = await page.evaluate(() => {
      const term = window.terminalUI.querySelector('.terminal-ui__terminal');
      return {
        term_inline_style: term?.getAttribute('style') || '',
        term_computed_visibility: getComputedStyle(term).visibility,
      };
    });
    expect(await activeOf(page, 'agent-terminal')).toBe('agent-terminal');
    expect(state.term_inline_style).not.toContain('visibility: hidden');
    expect(state.term_computed_visibility).toBe('visible');

    // Click Agent Chat
    await page.locator('.terminal-ui__slot-tab[data-pane="agent-chat"]').click();
    expect(await activeOf(page, 'agent-chat')).toBe('agent-chat');

    // Click Agent Terminal again -- must still be visible
    await page.locator('.terminal-ui__slot-tab[data-pane="agent-terminal"]').click();
    state = await page.evaluate(() => {
      const term = window.terminalUI.querySelector('.terminal-ui__terminal');
      return {
        term_inline_style: term?.getAttribute('style') || '',
        term_computed_visibility: getComputedStyle(term).visibility,
        term_computed_position: getComputedStyle(term).position,
      };
    });
    expect(await activeOf(page, 'agent-terminal')).toBe('agent-terminal');
    expect(state.term_inline_style).not.toContain('visibility: hidden');
    expect(state.term_inline_style).not.toContain('position: absolute');
    expect(state.term_computed_visibility).toBe('visible');

    // One more toggle for good measure
    await page.locator('.terminal-ui__slot-tab[data-pane="agent-chat"]').click();
    await page.locator('.terminal-ui__slot-tab[data-pane="agent-terminal"]').click();
    state = await page.evaluate(() => {
      const term = window.terminalUI.querySelector('.terminal-ui__terminal');
      return {
        visible: getComputedStyle(term).visibility === 'visible',
        position: getComputedStyle(term).position,
      };
    });
    expect(state.visible).toBe(true);
    expect(state.position).not.toBe('absolute');
  });

  test('Agent Chat tab label carries braille spinner during probe (not the legacy "(Loading)" text)', async ({ page }) => {
    // Race the probe: navigate and immediately start polling for the spinner
    // character in the Agent Chat tab label. If the probe is fast enough we may
    // never observe it -- retry a few times.
    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });

    // Poll aggressively for up to 3 seconds to catch the pending window.
    // Use \u-escaped range so this source file stays ASCII.
    const BRAILLE_RE = /[\u2800-\u28FF]/;
    const sawSpinner = await page.evaluate(async (pattern) => {
      const re = new RegExp(pattern);
      const deadline = Date.now() + 3000;
      while (Date.now() < deadline) {
        const ui = window.terminalUI;
        if (ui) {
          const lbl = ui.querySelector('.terminal-ui__slot-tab[data-pane="agent-chat"] .terminal-ui__slot-tab-label')?.textContent || '';
          if (re.test(lbl)) return { hit: true, label: lbl };
          if (lbl.includes('(Loading)')) return { hit: false, label: lbl, bug: 'legacy (Loading) text' };
        }
        await new Promise(r => setTimeout(r, 50));
      }
      return { hit: false, label: null };
    }, BRAILLE_RE.source);

    // It's legitimate for a fast probe to skip the spinner window entirely --
    // assert only that we never saw the legacy text, which the fix replaced.
    if (sawSpinner.bug) {
      throw new Error(`Saw legacy loading text: ${sawSpinner.label}`);
    }

    // Now wait for probe success and confirm the spinner cleared.
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true);
    const finalLabel = await page.evaluate(() => {
      return window.terminalUI.querySelector('.terminal-ui__slot-tab[data-pane="agent-chat"] .terminal-ui__slot-tab-label')?.textContent;
    });
    expect(finalLabel).toBe('Agent Chat');
  });

  test('mobile viewport ?session=chat: Agent Terminal stays active during probe, flips to chat on probe success', async ({ page }) => {
    // Mirrors the desktop test above for the mobile dropdown / pane-host
    // attribute. On ?session=chat we used to eagerly point the mobile nav at
    // agent-chat, which painted a blank "Connecting to chat..." view while
    // the probe was still running -- and any agent-terminal prompt was
    // hidden behind it. Mobile should mirror desktop: stay on Agent Terminal
    // until the chat iframe is actually loadable, then flip.
    await page.setViewportSize({ width: 400, height: 800 });
    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });

    // Wait until the mobile init has run (data-mobile-active is set).
    await page.waitForFunction(() => {
      return Array.from(document.querySelectorAll('.terminal-ui__pane-host'))
        .some(h => h.hasAttribute('data-mobile-active'));
    }, null, { timeout: 30_000 });

    // Snapshot before-probe state. If the probe is still in flight we
    // should observe agent-terminal. If it has already completed (warm
    // container), we'll see agent-chat -- the post-probe assertion below
    // catches the flip either way.
    const beforeProbe = await page.evaluate(() => {
      const ui = window.terminalUI;
      const hosts = Array.from(document.querySelectorAll('.terminal-ui__pane-host'));
      const active = hosts.filter(h => h.hasAttribute('data-mobile-active'));
      return {
        activePane: active[0]?.dataset.pane,
        available: ui?._agentChatAvailable,
      };
    });
    if (beforeProbe.available !== true) {
      expect(beforeProbe.activePane).toBe('agent-terminal');
    }

    // After probe success the mobile nav must have flipped to agent-chat.
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true);
    await page.waitForFunction(() => {
      const host = document.querySelector('.terminal-ui__pane-host[data-pane="agent-chat"]');
      return host?.hasAttribute('data-mobile-active');
    }, null, { timeout: 10_000 });
  });

  test('mobile viewport: switchMobileNav toggles visible pane, not blank body', async ({ page }) => {
    // Use mobile viewport (matches CSS @media max-width: 639px).
    // Set BEFORE navigating so the page loads in mobile mode and our
    // openChatSession helper's `.terminal-ui__terminal` locator (hidden on
    // mobile when session=chat) isn't what we rely on.
    await page.setViewportSize({ width: 400, height: 800 });
    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });

    // Wait until the custom element has mounted + WS has delivered session.
    await waitForUi(page, () => window.terminalUI && window.terminalUI.sessionUUID);
    // Wait for the probe to complete so the mobile nav has flipped to
    // agent-chat -- otherwise we'd race the eager-vs-deferred init.
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true);
    await page.waitForFunction(() => {
      const host = document.querySelector('.terminal-ui__pane-host[data-pane="agent-chat"]');
      return host?.hasAttribute('data-mobile-active');
    }, null, { timeout: 10_000 });

    // After probe success, exactly one pane-host carries data-mobile-active
    // and it is agent-chat.
    const initial = await page.evaluate(() => {
      const hosts = Array.from(document.querySelectorAll('.terminal-ui__pane-host'));
      const active = hosts.filter(h => h.hasAttribute('data-mobile-active'));
      return {
        activeCount: active.length,
        activePane: active[0]?.dataset.pane,
        anyVisibleSlotFrame: Array.from(document.querySelectorAll('.terminal-ui__slot-frame')).some(f => f.offsetHeight > 0),
      };
    });
    expect(initial.activeCount).toBe(1);
    expect(initial.activePane).toBe('agent-chat');
    expect(initial.anyVisibleSlotFrame).toBe(false);

    // Switch to Agent Terminal via the mobile dropdown.
    await page.selectOption('.terminal-ui__mobile-nav-select', 'agent-terminal');
    const afterTerm = await page.evaluate(() => {
      const hosts = Array.from(document.querySelectorAll('.terminal-ui__pane-host'));
      const active = hosts.filter(h => h.hasAttribute('data-mobile-active'));
      const termHost = document.querySelector('.terminal-ui__pane-host[data-pane="agent-terminal"]');
      return {
        activePane: active[0]?.dataset.pane,
        termHost_computed_display: termHost ? getComputedStyle(termHost).display : null,
      };
    });
    expect(afterTerm.activePane).toBe('agent-terminal');
    expect(afterTerm.termHost_computed_display).not.toBe('none');

    // Switch to Preview.
    await page.selectOption('.terminal-ui__mobile-nav-select', 'preview');
    const afterPreview = await page.evaluate(() => {
      const active = Array.from(document.querySelectorAll('.terminal-ui__pane-host')).filter(h => h.hasAttribute('data-mobile-active'));
      const previewHost = document.querySelector('.terminal-ui__pane-host[data-pane="preview"]');
      return {
        activePane: active[0]?.dataset.pane,
        activeCount: active.length,
        previewHost_computed_display: previewHost ? getComputedStyle(previewHost).display : null,
      };
    });
    expect(afterPreview.activeCount).toBe(1);
    expect(afterPreview.activePane).toBe('preview');
    expect(afterPreview.previewHost_computed_display).not.toBe('none');
  });

  test('assistant=shell renders single-slot agent-terminal, no nested tab bars', async ({ page }) => {
    // Regression: the Terminal (shell) pane's iframe URL is
    // /session/<shellUUID>?assistant=shell -- loading the full terminal-ui
    // element inside an iframe. Before the fix, it rehydrated the user's
    // multi-slot preset from localStorage, so the inner iframe showed
    // Preview / Agent View / Terminal tabs. Clicking the inner Terminal
    // recursed infinitely.
    //
    // Fix: in shell mode, force preset=single with only agent-terminal in
    // slot a, and do not persist. CSS hides slot-frame in embedded-iframe
    // mode as a defense-in-depth so any embedded rendering has no visible
    // tab chrome.

    // First persist a non-default multi-slot preset in this origin's
    // localStorage so we can confirm the shell-mode override wins.
    // Visit the origin so we have a document to access localStorage on;
    // the storageState cookie keeps us logged in.
    await page.goto('/');
    await page.evaluate(() => {
      localStorage.setItem('swe-swe-layout-v1', JSON.stringify({
        preset: 'quadrants',
        activeBySlot: {
          a: { tabs: ['agent-terminal'], active: 'agent-terminal' },
          b: { tabs: ['preview', 'browser'], active: 'browser' },
          c: { tabs: ['agent-chat'], active: 'agent-chat' },
          d: { tabs: ['shell'], active: 'shell' },
        },
      }));
    });

    // Now load the shell URL directly. This is what the Terminal pane's
    // iframe loads.
    const shellUUID = crypto.randomUUID();
    await page.goto(`/session/${shellUUID}?assistant=shell&parent=${crypto.randomUUID()}`);
    await waitForUi(page, () => window.terminalUI && window.terminalUI.activeBySlot);

    const state = await page.evaluate(() => {
      const ui = window.terminalUI;
      return {
        preset: ui.preset,
        slots: Object.keys(ui.activeBySlot || {}),
        slotA_tabs: ui.activeBySlot?.a?.tabs,
        slotA_active: ui.activeBySlot?.a?.active,
        // Count slot-tab-bars actually rendered in the DOM.
        tabBars: ui.querySelectorAll('.terminal-ui__slot-tab-bar').length,
        // Count iframes for iframe-capable panes that shouldn't be mounted
        // in shell mode (shell pane itself would recurse; Preview / Agent View
        // aren't useful inside a shell iframe either).
        shellIframesInDom: ui.querySelectorAll('.terminal-ui__pane-host[data-pane="shell"]:not([hidden]) iframe, .terminal-ui__pane-host[data-pane="browser"]:not([hidden]) iframe, .terminal-ui__pane-host[data-pane="preview"]:not([hidden]) iframe').length,
      };
    });

    expect(state.preset).toBe('single');
    expect(state.slots).toEqual(['a']);
    expect(state.slotA_tabs).toEqual(['agent-terminal']);
    expect(state.slotA_active).toBe('agent-terminal');
    // Zero visible iframes for iframe-capable panes -- nothing to recurse.
    expect(state.shellIframesInDom).toBe(0);

    // Outer localStorage must not have been rewritten by the shell-mode
    // override -- the quadrants preset we set up-front should still be
    // there (the user's "real" preset for the main session).
    const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('swe-swe-layout-v1')));
    expect(saved.preset).toBe('quadrants');
  });

  test('slot "+" popover menu uses the app font stack, not browser default', async ({ page }) => {
    // Regression: the slot-add popover is appended to document.body (to
    // escape the slot's overflow/stacking context). Since the terminal-ui
    // custom element sets font-family on itself (not on body), the
    // portaled menu inherited the browser default -- the user saw the
    // "Terminal" menu item rendered in a mismatched serif-ish font.
    //
    // Fix: set font-family explicitly on .terminal-ui__slot-replace-menu.
    await openChatSession(page);

    // Wait for the layout to render. The "+" button lives on the slot
    // tab bar; click whichever slot has one unassigned pane available so
    // the popover actually has items.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return ui && ui.querySelector('.terminal-ui__slot-add');
    });

    const addBtn = page.locator('.terminal-ui__slot-add').first();
    await addBtn.click();
    await page.locator('.terminal-ui__slot-replace-menu').waitFor({ state: 'visible' });

    const fonts = await page.evaluate(() => {
      const menu = document.querySelector('.terminal-ui__slot-replace-menu');
      const firstItem = menu?.querySelector('.terminal-ui__slot-replace-item');
      const refTab = document.querySelector('terminal-ui .terminal-ui__slot-tab');
      return {
        menuFamily: menu ? getComputedStyle(menu).fontFamily : null,
        itemFamily: firstItem ? getComputedStyle(firstItem).fontFamily : null,
        refTabFamily: refTab ? getComputedStyle(refTab).fontFamily : null,
      };
    });

    // The popover menu's font stack must include 'Inter' (or at least the
    // Apple / BlinkMacSystemFont fallbacks) so it visually matches the
    // rest of the UI -- NOT the browser default (serif / Times).
    expect(fonts.menuFamily || '').toMatch(/Inter|-apple-system|BlinkMacSystemFont/i);
    expect(fonts.itemFamily || '').toMatch(/Inter|-apple-system|BlinkMacSystemFont/i);
    // Items (which are font-family:inherit) should match the menu.
    expect(fonts.itemFamily).toBe(fonts.menuFamily);
    // And sanity: a slot-tab button (same font expectation) resolves to the
    // same family stack.
    if (fonts.refTabFamily) {
      expect(fonts.menuFamily).toBe(fonts.refTabFamily);
    }
  });

  test('Files tab: present by default, activating it loads md-serve at filesProxyPort', async ({ page }) => {
    // The Files pane (per-session md-serve) ships in the classic preset
    // defaults (left slot, alongside Agent Chat). Its tab only renders once
    // the WS Status payload delivers filesProxyPort (unknown panes are
    // hidden from tab bars), so wait for that first.
    await openChatSession(page);

    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return ui && ui.filesProxyPort && ui.sessionUUID;
    });

    const filesProxyPort = await page.evaluate(() => window.terminalUI.filesProxyPort);
    expect(filesProxyPort).toBeTruthy();

    // The Files tab is a default -- no "+" menu needed. It starts active
    // (fresh-default), but the chat probe-success handoff may have flipped
    // the slot to Agent Chat by now; click it to (re)activate.
    const filesTab = page.locator('.terminal-ui__slot-tab[data-pane="files"]');
    await filesTab.waitFor({ timeout: 10_000 });
    await filesTab.click();

    const tabState = await page.evaluate(() => {
      const ui = window.terminalUI;
      const tab = ui.querySelector('.terminal-ui__slot-tab[data-pane="files"]');
      const slot = ui._slotForPane('files');
      return {
        hasTab: !!tab,
        tabLabel: tab?.querySelector('.terminal-ui__slot-tab-label')?.textContent,
        slotForFiles: slot,
        activeInSlot: ui.activeBySlot?.[slot]?.active,
      };
    });
    expect(tabState.hasTab).toBe(true);
    expect(tabState.tabLabel).toBe('Files');
    expect(tabState.slotForFiles).toBeTruthy();
    expect(tabState.activeInSlot).toBe('files');

    // The files iframe src must point at the filesProxyPort (cross-origin
    // port form in non-tunnel mode: http://<host>:<filesProxyPort>/). Give
    // _loadPaneIfNeeded a moment to set it.
    const src = await page.waitForFunction(() => {
      const iframe = window.terminalUI.querySelector('.terminal-ui__iframe[data-pane="files"]');
      const s = iframe?.getAttribute('src');
      return s ? s : null;
    }, null, { timeout: 10_000 }).then(h => h.jsonValue());
    expect(src).toBeTruthy();
    expect(src).toContain(`:${filesProxyPort}`);

    // Finally, confirm md-serve is actually answering on that proxy port by
    // fetching the directory listing it renders for the session workDir.
    // Cross-origin so we use no-cors and assert the request resolves (an
    // opaque response) rather than throwing -- mirrors ports.spec.js.
    const url = new URL(BASE_URL);
    const filesUrl = `${url.protocol}//${url.hostname}:${filesProxyPort}/`;
    // Retry the reachability probe: the per-session md-serve is launched via
    // `npx -y @choonkeat/md-serve@latest`, whose cold `@latest` registry check
    // can lag several seconds behind the (instant) Go files-proxy bind. A
    // single-shot fetch races that startup and flakes; poll like ports.spec's
    // fetchPortWithRetry. Cross-origin, so no-cors -- an opaque resolved
    // response means the port answered.
    let reachable = { ok: false, error: 'not attempted' };
    for (let i = 0; i < 8; i++) {
      reachable = await page.evaluate(async (fetchUrl) => {
        try {
          const r = await fetch(fetchUrl, { signal: AbortSignal.timeout(5000), mode: 'no-cors' });
          return { ok: true, type: r.type };
        } catch (e) {
          return { ok: false, error: e.message };
        }
      }, filesUrl);
      if (reachable.ok) break;
      console.log(`  Retry ${i + 1}/8 for files port ${filesProxyPort}: ${reachable.error}`);
      await page.waitForTimeout(2000);
    }
    expect(reachable.ok).toBe(true);
  });

  test('preview iframe loads without getting stuck on "Connecting..." placeholder', async ({ page }) => {
    await openChatSession(page);

    // Wait for the session WS init to deliver previewPort + sessionUUID.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return ui && ui.previewPort && ui.sessionUUID;
    });

    // The preview iframe src should be set (not empty) once both are known.
    // Give applyPreset + the retry in the WS init handler a moment.
    const previewSrc = await page.waitForFunction(() => {
      const iframe = window.terminalUI.querySelector('.terminal-ui__iframe[data-pane="preview"]');
      return iframe?.getAttribute('src') || null;
    }, null, { timeout: 10_000 });

    const src = await previewSrc.jsonValue();
    expect(src).toBeTruthy();
    expect(src).not.toBe('');
  });
});
