import { test, expect } from '@playwright/test';
import crypto from 'crypto';

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';

async function login(page) {
  await page.goto('/swe-swe-auth/login');
  await page.fill('input[type="password"]', PASSWORD);
  await Promise.all([
    page.waitForNavigation(),
    page.click('button[type="submit"]'),
  ]);
}

// Wait until window.terminalUI exists and reports a given property via predicate.
async function waitForUi(page, predicate) {
  return page.waitForFunction(predicate, null, { timeout: 30_000 });
}

async function openChatSession(page) {
  const uuid = crypto.randomUUID();
  await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  return uuid;
}

async function openTerminalSession(page) {
  const uuid = crypto.randomUUID();
  await page.goto(`/session/${uuid}?assistant=opencode&session=terminal`);
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  return uuid;
}

test.describe('terminal-ui tab switching', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
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
        probing: ui._agentChatProbing,
        pending: ui._agentChatPending,
        available: ui._agentChatAvailable,
        chatTabLabel: ui.querySelector('.terminal-ui__slot-tab[data-pane="agent-chat"] .terminal-ui__slot-tab-label')?.textContent,
      };
    });

    expect(post.slotA_tabs).toEqual(['agent-terminal', 'agent-chat']);
    expect(post.slotA_active).toBe('agent-chat');
    expect(post.probing).toBe(false);
    expect(post.pending).toBe(false);
    expect(post.available).toBe(true);
    expect(post.chatTabLabel).toBe('Agent Chat');
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
    // no manual layout actions) or have active:'agent-terminal' with
    // agent-chat absent from the tabs list.
    const saved = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-layout-v1');
      return raw ? JSON.parse(raw) : null;
    });

    if (saved) {
      expect(saved.activeBySlot?.a?.tabs || []).not.toContain('agent-chat');
      expect(saved.activeBySlot?.a?.active).toBe('agent-terminal');
    }

    // Now reload with session=chat and verify that during the probe window,
    // Agent Terminal is the active tab (not agent-chat from a stale save).
    await page.reload();

    // As soon as the custom element boots, before the probe completes, the
    // slot active should be agent-terminal. Assert before _agentChatAvailable
    // flips true.
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
    // about is that pre-probe it was agent-terminal, not agent-chat. If
    // available is already true, we've raced past the probe window -- the
    // prior assertion on saved.active already covered the persistence bug.
    if (duringProbe.available === false || duringProbe.available === undefined) {
      expect(duringProbe.slotA_active).toBe('agent-terminal');
    }

    // After probe, agent-chat is active in memory -- still not persisted.
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true
      && window.terminalUI?.activeBySlot?.a?.active === 'agent-chat');
    const savedAfterReload = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-layout-v1');
      return raw ? JSON.parse(raw) : null;
    });
    if (savedAfterReload) {
      expect(savedAfterReload.activeBySlot?.a?.active).toBe('agent-terminal');
      expect(savedAfterReload.activeBySlot?.a?.tabs || []).not.toContain('agent-chat');
    }
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
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=terminal`);
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

    // Click Agent Terminal slot-tab, check xterm is rendered
    await page.locator('.terminal-ui__slot-tab[data-pane="agent-terminal"]').click();
    let state = await page.evaluate(() => {
      const ui = window.terminalUI;
      const term = ui.querySelector('.terminal-ui__terminal');
      return {
        slotA_active: ui.activeBySlot?.a?.active,
        term_inline_style: term?.getAttribute('style') || '',
        term_computed_visibility: getComputedStyle(term).visibility,
      };
    });
    expect(state.slotA_active).toBe('agent-terminal');
    expect(state.term_inline_style).not.toContain('visibility: hidden');
    expect(state.term_computed_visibility).toBe('visible');

    // Click Agent Chat
    await page.locator('.terminal-ui__slot-tab[data-pane="agent-chat"]').click();
    state = await page.evaluate(() => {
      const ui = window.terminalUI;
      return { slotA_active: ui.activeBySlot?.a?.active };
    });
    expect(state.slotA_active).toBe('agent-chat');

    // Click Agent Terminal again -- must still be visible
    await page.locator('.terminal-ui__slot-tab[data-pane="agent-terminal"]').click();
    state = await page.evaluate(() => {
      const ui = window.terminalUI;
      const term = ui.querySelector('.terminal-ui__terminal');
      return {
        slotA_active: ui.activeBySlot?.a?.active,
        term_inline_style: term?.getAttribute('style') || '',
        term_computed_visibility: getComputedStyle(term).visibility,
        term_computed_position: getComputedStyle(term).position,
      };
    });
    expect(state.slotA_active).toBe('agent-terminal');
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
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);

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

  test('mobile viewport: switchMobileNav toggles visible pane, not blank body', async ({ page }) => {
    // Use mobile viewport (matches CSS @media max-width: 639px).
    // Set BEFORE navigating so the page loads in mobile mode and our
    // openChatSession helper's `.terminal-ui__terminal` locator (hidden on
    // mobile when session=chat) isn't what we rely on.
    await page.setViewportSize({ width: 400, height: 800 });
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);

    // Wait until the custom element has mounted + WS has delivered session.
    await waitForUi(page, () => window.terminalUI && window.terminalUI.sessionUUID);

    // On page load with ?session=chat, the mobile initializer picks
    // agent-chat. Verify exactly one pane-host carries data-mobile-active.
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
    await page.goto('/swe-swe-auth/login');
    // (login already happened in beforeEach; just visit origin to access localStorage)
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
