/**
 * The Preview tab's "Check" report.
 *
 * Preview is two pages, one inside the other: the proxy's outer page, and the
 * app inside it. When the tab stayed white on an iPad behind Cloudflare there
 * was no developer console to see which level stopped, or what the network
 * had done to swe-swe's replies. This collects both into plain lines, so one
 * screenshot of the report answers it.
 *
 * @module preview-check
 */

function describeReply(label, url, resp) {
    const h = (name) => resp.headers.get(name);
    return [
        `${label}: status ${resp.status}`,
        `  X-Frame-Options: ${h('X-Frame-Options') || '(none)'}`,
        `  Content-Security-Policy: ${h('Content-Security-Policy') || '(none)'}`,
        `  marker: ${h('X-Agent-Reverse-Proxy') ? 'present' : 'missing'}`,
    ];
}

/**
 * @param {object} opts
 * @param {HTMLIFrameElement} opts.pane - the Preview tab's iframe
 * @param {object} opts.ui - the TerminalUI instance (read-only)
 * @param {typeof fetch} opts.fetchImpl
 * @param {string} opts.userAgent
 * @returns {Promise<string[]>}
 */
export async function collectPreviewCheck({ pane, ui, fetchImpl, userAgent }) {
    const lines = [];
    ui = ui || {};
    lines.push(`mode: ${ui._proxyMode || '(not chosen)'}, gave up waiting: ${!!ui._previewProbeGaveUp}, ` +
        `placeholder: ${!!ui._previewWaiting}, app running: ${ui._previewAppUp}`);

    const paneSrc = pane && pane.getAttribute('src');
    lines.push(`pane: ${paneSrc || '(not loaded)'}`);

    let innerUrl = null;
    if (paneSrc) {
        try {
            const shellWin = pane.contentWindow;
            lines.push(`outer page: ${shellWin.location.href}`);
            const inner = shellWin.document.getElementById('inner');
            lines.push(`inner src: ${inner ? inner.getAttribute('src') || '(empty)' : '(no inner frame)'}`);
            if (inner) {
                try {
                    const w = inner.contentWindow;
                    innerUrl = w.location.href;
                    lines.push(`inner page: ${innerUrl}`);
                    const body = w.document.body;
                    lines.push(`inner shows: ${body ? (body.innerText || '(empty)').trim().slice(0, 80) : '(no body)'}`);
                } catch (e) {
                    lines.push(`inner: cannot read (${e.message})`);
                }
            }
        } catch (e) {
            lines.push(`outer page: cannot read (${e.message})`);
        }
    }

    // What the network hands the browser for each level, headers included:
    // a gateway that adds, swaps or drops headers shows up here.
    if (paneSrc) {
        const appUrl = paneSrc.replace(/\/__agent-reverse-proxy-debug__\/shell.*$/, '/');
        for (const [label, url] of [['outer reply', paneSrc], ['app reply', appUrl]]) {
            try {
                const resp = await fetchImpl(url, { credentials: 'include', cache: 'no-store' });
                lines.push(...describeReply(label, url, resp));
            } catch (e) {
                lines.push(`${label}: failed (${e.message})`);
            }
        }
    }

    lines.push(`browser: ${userAgent}`);
    return lines;
}
