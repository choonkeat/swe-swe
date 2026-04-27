import { test, expect } from '@playwright/test';
import crypto from 'crypto';

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';
const PUBLIC_HOSTNAME = process.env.SWE_PUBLIC_HOSTNAME || '';

// One spec, three assertion modes:
// - SWE_PUBLIC_HOSTNAME unset (default): preview/agent-chat URLs are
//   port-based (regression gate). publicHostname on the WS status frame
//   must be empty so getPreviewBaseUrl() falls through to legacy behavior.
// - SWE_PUBLIC_HOSTNAME=<host>: getPreviewBaseUrl() returns
//   "https://{previewPort}.{host}" (no proxyPortOffset). The hostname
//   reaches the server via env (default) or via state file when
//   SWE_TUNNEL_VIA=state-file (e2e-up.sh writes the file and does NOT
//   pass SWE_PUBLIC_HOSTNAME to the container -- both routes converge
//   on the same WS status frame, so the assertion is identical).
//
// Run via:
//   make e2e-up-simple && make e2e-test-simple && make e2e-down            # regression mode
//   SWE_PUBLIC_HOSTNAME=fake-tunnel.example.com make e2e-up-simple && \
//     SWE_PUBLIC_HOSTNAME=fake-tunnel.example.com make e2e-test-simple && \
//     make e2e-down                                                        # env mode
//   SWE_PUBLIC_HOSTNAME=fake-tunnel.example.com SWE_TUNNEL_VIA=state-file make e2e-up-simple && \
//     SWE_PUBLIC_HOSTNAME=fake-tunnel.example.com make e2e-test-simple && \
//     make e2e-down                                                        # state-file mode

async function login(page) {
    await page.goto('/swe-swe-auth/login');
    await page.fill('input[type="password"]', PASSWORD);
    await Promise.all([
        page.waitForNavigation(),
        page.click('button[type="submit"]'),
    ]);
}

async function createSessionAndWaitForStatus(page) {
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode`);

    return page.waitForFunction(() => {
        const ui = window.terminalUI;
        if (!ui) return null;
        if (typeof ui.publicHostname !== 'string') return null;
        if (!ui.previewPort) return null;
        return {
            publicHostname: ui.publicHostname,
            previewPort: ui.previewPort,
            previewProxyPort: ui.previewProxyPort,
            agentChatPort: ui.agentChatPort,
            agentChatProxyPort: ui.agentChatProxyPort,
            sessionUUID: ui.sessionUUID,
            previewBaseUrl: ui.getPreviewBaseUrl(),
        };
    }, { timeout: 60_000 }).then(h => h.jsonValue());
}

test.describe('tunnel-mode URL templating', () => {
    test('frontend reflects SWE_PUBLIC_HOSTNAME in WS status and getPreviewBaseUrl', async ({ page }) => {
        await login(page);
        const status = await createSessionAndWaitForStatus(page);

        // Sanity checks regardless of mode.
        expect(status.previewPort).toBeGreaterThan(0);
        expect(status.sessionUUID).toBeTruthy();

        if (PUBLIC_HOSTNAME === '') {
            // Regression mode: server should not have synthesized a hostname.
            expect(status.publicHostname).toBe('');
            // Preview base URL must NOT match subdomain shape.
            expect(status.previewBaseUrl).not.toMatch(/^https?:\/\/\d+\./);
            // It is one of: path-based (/proxy/<uuid>/preview) or port-based
            // (host:proxyPort). Both are acceptable in legacy mode.
            const isPathBased = status.previewBaseUrl.includes('/proxy/');
            const isPortBased = status.previewProxyPort && status.previewBaseUrl.includes(`:${status.previewProxyPort}`);
            expect(isPathBased || isPortBased).toBe(true);
        } else {
            // Subdomain mode.
            expect(status.publicHostname).toBe(PUBLIC_HOSTNAME);
            // Preview URL is "{protocol}//{previewPort}.{publicHostname}" --
            // raw target port (no proxyPortOffset).
            const expected = new RegExp(
                `^https?://${status.previewPort}\\.${PUBLIC_HOSTNAME.replace(/\./g, '\\.')}$`,
            );
            expect(status.previewBaseUrl).toMatch(expected);
            // Crucially: the proxyPort offset must NOT appear in the URL.
            if (status.previewProxyPort) {
                expect(status.previewBaseUrl).not.toContain(String(status.previewProxyPort));
            }
        }
    });
});
