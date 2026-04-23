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
