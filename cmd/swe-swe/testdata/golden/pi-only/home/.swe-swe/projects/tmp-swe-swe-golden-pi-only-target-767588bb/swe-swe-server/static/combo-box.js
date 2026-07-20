/**
 * <combo-box> -- minimal combobox web component.
 * Forked from /repos/combobox/workspace/src/combo-box.js
 * Added CSS custom property support for dark theme theming.
 *
 * Usage:
 *   <!-- standalone -->
 *   <combo-box placeholder="Pick one">
 *     <option value="a">Alpha</option>
 *     <option value="b">Beta</option>
 *   </combo-box>
 *
 *   <!-- upgrade an existing <select> -->
 *   <combo-box upgrade="select#my-select"></combo-box>
 *
 *   <!-- upgrade an <input> + <datalist> (allows free entry) -->
 *   <combo-box upgrade="input#my-input"></combo-box>
 *
 *   <!-- standalone with free entry -->
 *   <combo-box free-entry placeholder="Type or pick...">
 *     <option value="a">Alpha</option>
 *   </combo-box>
 */

const STYLES = /* css */ `
  :host {
    display: inline-block;
    position: relative;
    font-family: inherit;
    font-size: inherit;
  }

  :host([disabled]) {
    opacity: 0.5;
    pointer-events: none;
  }

  * { box-sizing: border-box; }

  .input-wrap {
    display: flex;
    align-items: center;
    border: 1px solid var(--combo-border, #bbb);
    border-radius: var(--combo-radius, 4px);
    background: var(--combo-bg, #fff);
    padding: 0;
    cursor: pointer;
  }

  .input-wrap:focus-within {
    border-color: var(--combo-focus-border, #4a90d9);
    box-shadow: 0 0 0 2px var(--combo-focus-shadow, rgba(74, 144, 217, 0.25));
  }

  input {
    flex: 1;
    border: none;
    outline: none;
    padding: var(--combo-input-padding, 6px 8px);
    font: inherit;
    background: transparent;
    color: var(--combo-text, inherit);
    min-width: 0;
  }

  input::placeholder {
    color: var(--combo-placeholder, #999);
  }

  .arrow {
    /* Generous padding: the glyph is ~10px, far under any touch minimum. The
       rest of the field opens the listbox too (see _onWrapClick), so this only
       has to be big enough to aim at when closing. */
    padding: 10px 12px;
    user-select: none;
    color: var(--combo-arrow, #666);
    font-size: 0.7em;
    transition: transform 0.15s;
  }

  :host([open]) .arrow {
    transform: rotate(180deg);
  }

  .listbox {
    display: none;
    position: fixed;
    z-index: 9999;
    max-height: 200px;
    overflow-y: auto;
    border: 1px solid var(--combo-border, #bbb);
    border-radius: var(--combo-radius, 4px);
    background: var(--combo-listbox-bg, #fff);
    box-shadow: 0 4px 12px var(--combo-listbox-shadow, rgba(0,0,0,0.12));
    list-style: none;
    padding: 4px 0;
  }

  :host([open]) .listbox {
    display: block;
  }

  .option {
    padding: 6px 10px;
    cursor: pointer;
    /* Wrap long values (e.g. git URLs) instead of clipping to an ellipsis. */
    white-space: normal;
    overflow-wrap: anywhere;
    color: var(--combo-text, inherit);
  }

  /* Divider between rows so wrapped multi-line options stay tellable apart.
     Off by default (transparent); consumers opt in via --combo-divider. */
  .option + .option {
    border-top: 1px solid var(--combo-divider, transparent);
  }

  /* Two-line option: bold primary label over a dimmed secondary line. */
  .opt-name {
    display: block;
  }

  .opt-detail {
    display: block;
    margin-top: 1px;
    font-size: 0.85em;
    color: var(--combo-detail, #888);
    overflow-wrap: anywhere;
  }

  .option[aria-selected="true"] {
    background: var(--combo-selected-bg, #4a90d9);
    color: var(--combo-selected-text, #fff);
  }

  .option[aria-selected="true"] .opt-detail {
    color: inherit;
    opacity: 0.85;
  }

  .option:not([aria-selected="true"]):hover {
    background: var(--combo-hover-bg, #f0f0f0);
  }

  .option[hidden] {
    display: none;
  }

  .no-results {
    padding: 6px 10px;
    color: var(--combo-placeholder, #999);
    font-style: italic;
    display: none;
  }

  .no-results[data-visible] {
    display: block;
  }

  mark {
    background: var(--combo-highlight, #fde68a);
    color: inherit;
    border-radius: 2px;
    padding: 0;
  }

  .option[aria-selected="true"] mark {
    background: rgba(255,255,255,0.3);
  }
`;

class ComboBox extends HTMLElement {
  static observedAttributes = ["upgrade", "placeholder", "value", "free-entry"];

  #options = [];    // { value, label }
  #value = "";
  #activeIndex = -1;
  #open = false;

  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.shadowRoot.innerHTML = `
      <style>${STYLES}</style>
      <div class="input-wrap" part="input-wrap">
        <input
          part="input"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded="false"
          aria-haspopup="listbox"
          autocomplete="off"
        />
        <span class="arrow" part="arrow">\u25BC</span>
      </div>
      <div class="listbox" role="listbox" part="listbox">
        <div class="no-results">No results</div>
      </div>
    `;

    this._input = this.shadowRoot.querySelector("input");
    this._listbox = this.shadowRoot.querySelector(".listbox");
    this._noResults = this.shadowRoot.querySelector(".no-results");
    this._arrow = this.shadowRoot.querySelector(".arrow");
    this._wrap = this.shadowRoot.querySelector(".input-wrap");

    this._onInputInput = this._onInputInput.bind(this);
    this._onInputKeydown = this._onInputKeydown.bind(this);
    this._onInputFocus = this._onInputFocus.bind(this);
    this._onDocClick = this._onDocClick.bind(this);
    this._onWrapClick = this._onWrapClick.bind(this);
  }

  connectedCallback() {
    this._input.addEventListener("input", this._onInputInput);
    this._input.addEventListener("keydown", this._onInputKeydown);
    this._input.addEventListener("focus", this._onInputFocus);
    this._wrap.addEventListener("click", this._onWrapClick);
    document.addEventListener("click", this._onDocClick, true);

    // Upgrade from <select> or <input list="..."> if specified
    const sel = this.getAttribute("upgrade");
    if (sel) {
      this._upgrade(sel);
    } else {
      // Read inline <option> children
      this._readChildOptions();
    }

    if (this.hasAttribute("placeholder")) {
      this._input.placeholder = this.getAttribute("placeholder");
    }

    if (this.hasAttribute("value")) {
      this.value = this.getAttribute("value");
    }
  }

  disconnectedCallback() {
    this._input.removeEventListener("input", this._onInputInput);
    this._input.removeEventListener("keydown", this._onInputKeydown);
    this._input.removeEventListener("focus", this._onInputFocus);
    this._wrap.removeEventListener("click", this._onWrapClick);
    document.removeEventListener("click", this._onDocClick, true);
  }

  attributeChangedCallback(name, _old, val) {
    if (name === "placeholder") this._input.placeholder = val || "";
    if (name === "value") this.value = val;
    if (name === "upgrade" && val) this._upgrade(val);
  }

  // --- Public API ---

  // Commit whatever is typed in the input right now, without waiting for the
  // listbox to close. In free-entry mode the value only becomes real on close,
  // so anything reading .value on submit must call this first -- otherwise a
  // user who types a branch and clicks Start straight away sends nothing.
  commit() {
    if (this.freeEntry) this._acceptFreeText();
  }

  get value() {
    return this.#value;
  }

  set value(v) {
    this.#value = v;
    const opt = this.#options.find((o) => o.value === v);
    this._input.value = opt ? opt.label : v;
  }

  get options() {
    return [...this.#options];
  }

  get freeEntry() {
    return this.hasAttribute("free-entry");
  }

  set freeEntry(v) {
    v ? this.setAttribute("free-entry", "") : this.removeAttribute("free-entry");
  }

  setOptions(opts) {
    this.#options = opts.map((o) =>
      typeof o === "string"
        ? { value: o, label: o }
        : { value: o.value, label: o.label, detail: o.detail || "" }
    );
    this._renderOptions();
  }

  // --- Upgrade <select> or <input list="..."> ---

  _upgrade(selector) {
    const el = document.querySelector(selector);
    if (!el) {
      console.warn(`<combo-box>: no element found for "${selector}"`);
      return;
    }

    if (el.tagName === "SELECT") {
      this._upgradeSelect(el);
    } else if (el.tagName === "INPUT") {
      this._upgradeInput(el);
    } else {
      console.warn(`<combo-box>: unsupported element <${el.tagName.toLowerCase()}>`);
    }
  }

  _upgradeSelect(select) {
    const opts = [];
    for (const opt of select.options) {
      if (opt.disabled) continue;
      opts.push({ value: opt.value, label: opt.textContent, detail: opt.dataset.detail || "" });
    }
    this.#options = opts;
    this._renderOptions();

    if (!this.hasAttribute("placeholder")) {
      const first = select.options[0];
      if (first && (first.value === "" || first.disabled)) {
        this._input.placeholder = first.textContent;
      }
    }

    if (select.value) {
      this.value = select.value;
    }

    select.hidden = true;
    select.setAttribute("aria-hidden", "true");

    this.addEventListener("change", (e) => {
      select.value = e.detail.value;
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });

    this._upgradedEl = select;
  }

  _upgradeInput(input) {
    // Enable free entry for input+datalist
    this.freeEntry = true;

    // Pull options from associated <datalist>
    const listId = input.getAttribute("list");
    const datalist = listId && document.getElementById(listId);
    if (datalist) {
      const opts = [];
      for (const opt of datalist.querySelectorAll("option")) {
        opts.push({ value: opt.value, label: opt.textContent.trim() || opt.value });
      }
      this.#options = opts;
      this._renderOptions();
    }

    // Inherit placeholder
    if (!this.hasAttribute("placeholder") && input.placeholder) {
      this._input.placeholder = input.placeholder;
    }

    // Sync initial value
    if (input.value) {
      this.value = input.value;
    }

    // Hide original input (and datalist)
    input.hidden = true;
    input.setAttribute("aria-hidden", "true");
    if (datalist) datalist.hidden = true;

    // Sync back
    this.addEventListener("change", (e) => {
      input.value = e.detail.value;
      input.dispatchEvent(new Event("input", { bubbles: true }));
      input.dispatchEvent(new Event("change", { bubbles: true }));
    });

    this._upgradedEl = input;
  }

  // --- Read inline <option> children ---

  _readChildOptions() {
    const opts = [];
    for (const el of this.querySelectorAll("option")) {
      opts.push({ value: el.value || el.textContent, label: el.textContent, detail: el.dataset.detail || "" });
    }
    if (opts.length) {
      this.#options = opts;
      this._renderOptions();
    }
  }

  // --- Render ---

  // Fuzzy match: find `needle` chars in order within `haystack`.
  // Returns null (no match) or { positions: [...], score }.
  // Score: lower is tighter. Exact substring = 0, spread chars = higher.
  _fuzzyMatch(haystack, needle) {
    const hLower = haystack.toLowerCase();
    const nLower = needle.toLowerCase();

    // Try starting from every occurrence of the first character
    // and pick the match path with the tightest score.
    let best = null;
    let startFrom = 0;

    while (true) {
      const firstIdx = hLower.indexOf(nLower[0], startFrom);
      if (firstIdx === -1) break;

      const positions = [firstIdx];
      let hi = firstIdx + 1;
      let valid = true;

      for (let ni = 1; ni < nLower.length; ni++) {
        const idx = hLower.indexOf(nLower[ni], hi);
        if (idx === -1) { valid = false; break; }
        positions.push(idx);
        hi = idx + 1;
      }

      if (valid) {
        const span = positions[positions.length - 1] - positions[0];
        const startPenalty = positions[0] * 0.1;
        const score = span + startPenalty;
        if (!best || score < best.score) {
          best = { positions, score };
        }
      }

      startFrom = firstIdx + 1;
    }

    return best;
  }

  _renderOptions(filter = "") {
    // Remove old option elements
    this._listbox.querySelectorAll(".option").forEach((el) => el.remove());

    const lc = filter.toLowerCase();

    // Build scored list. Match against the label AND the detail line (e.g. the
    // full git URL) so typing any part of either still surfaces the option.
    const scored = [];
    this.#options.forEach((opt, i) => {
      if (!lc) {
        scored.push({ opt, i, mLabel: null, mDetail: null, score: 0 });
      } else {
        const mLabel = this._fuzzyMatch(opt.label, lc);
        const mDetail = opt.detail ? this._fuzzyMatch(opt.detail, lc) : null;
        if (mLabel || mDetail) {
          const score = Math.min(
            mLabel ? mLabel.score : Infinity,
            mDetail ? mDetail.score : Infinity
          );
          scored.push({ opt, i, mLabel, mDetail, score });
        }
      }
    });

    // Sort by score (tightest first), stable by original order
    if (lc) scored.sort((a, b) => a.score - b.score || a.i - b.i);

    const frag = document.createDocumentFragment();

    for (const { opt, i, mLabel, mDetail } of scored) {
      const div = document.createElement("div");
      div.className = "option";
      div.setAttribute("role", "option");
      div.dataset.index = i;
      div.dataset.value = opt.value;

      if (opt.detail) {
        // Two-line: primary label over dimmed detail, each highlighted on its own.
        const name = document.createElement("span");
        name.className = "opt-name";
        if (mLabel) name.innerHTML = this._highlightPositions(opt.label, mLabel.positions);
        else name.textContent = opt.label;

        const detail = document.createElement("span");
        detail.className = "opt-detail";
        if (mDetail) detail.innerHTML = this._highlightPositions(opt.detail, mDetail.positions);
        else detail.textContent = opt.detail;

        div.append(name, detail);
      } else if (mLabel) {
        div.innerHTML = this._highlightPositions(opt.label, mLabel.positions);
      } else {
        div.textContent = opt.label;
      }

      div.addEventListener("click", (e) => {
        e.stopPropagation();
        this._select(i);
      });

      frag.appendChild(div);
    }

    this._listbox.insertBefore(frag, this._noResults);

    if (scored.length === 0 && filter) {
      this._noResults.textContent = this.freeEntry ? "No suggestions \u2014 press Enter to use as-is" : "No results";
      this._noResults.setAttribute("data-visible", "");
    } else {
      this._noResults.removeAttribute("data-visible");
    }

    this.#activeIndex = -1;
  }

  _highlightPositions(label, positions) {
    const posSet = new Set(positions);
    let html = "";
    let inMark = false;

    for (let i = 0; i < label.length; i++) {
      const hit = posSet.has(i);
      if (hit && !inMark) { html += "<mark>"; inMark = true; }
      if (!hit && inMark) { html += "</mark>"; inMark = false; }
      html += this._esc(label[i]);
    }
    if (inMark) html += "</mark>";
    return html;
  }

  _esc(str) {
    return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  _getVisibleOptions() {
    return [...this._listbox.querySelectorAll(".option:not([hidden])")];
  }

  // --- Selection ---

  _select(index) {
    const opt = this.#options[index];
    if (!opt) return;
    this.#value = opt.value;
    this._input.value = opt.label;
    this._close();
    this.dispatchEvent(
      new CustomEvent("change", {
        detail: { value: opt.value, label: opt.label },
        bubbles: true,
      })
    );
  }

  // --- Open / Close ---

  _open() {
    if (this.#open) return;
    this.#open = true;
    this.setAttribute("open", "");
    this._input.setAttribute("aria-expanded", "true");
    this._positionListbox();
    this._renderOptions(this._input.value === this._labelForValue(this.#value) ? "" : this._input.value);
  }

  _positionListbox() {
    const rect = this._wrap.getBoundingClientRect();
    this._listbox.style.left = rect.left + "px";
    this._listbox.style.top = (rect.bottom + 2) + "px";
    this._listbox.style.width = rect.width + "px";
  }

  _close() {
    if (!this.#open) return;
    this.#open = false;
    this.removeAttribute("open");
    this._input.setAttribute("aria-expanded", "false");
    this.#activeIndex = -1;
    this._clearHighlight();

    // In free-entry mode, accept whatever text is in the input
    if (this.freeEntry) {
      this._acceptFreeText();
    }
  }

  _acceptFreeText() {
    const text = this._input.value;
    // Check if it matches a known option label
    const match = this.#options.find((o) => o.label.toLowerCase() === text.toLowerCase());
    const newValue = match ? match.value : text;
    if (newValue !== this.#value) {
      this.#value = newValue;
      this.dispatchEvent(
        new CustomEvent("change", {
          detail: { value: newValue, label: text },
          bubbles: true,
        })
      );
    }
  }

  _toggle() {
    this.#open ? this._close() : this._open();
  }

  _labelForValue(v) {
    const opt = this.#options.find((o) => o.value === v);
    return opt ? opt.label : "";
  }

  // --- Highlight ---

  _highlight(index) {
    const visible = this._getVisibleOptions();
    if (!visible.length) return;
    this._clearHighlight();
    const clamped = ((index % visible.length) + visible.length) % visible.length;
    this.#activeIndex = clamped;
    const el = visible[clamped];
    el.setAttribute("aria-selected", "true");
    el.scrollIntoView({ block: "nearest" });
  }

  _clearHighlight() {
    this._listbox
      .querySelectorAll('.option[aria-selected="true"]')
      .forEach((el) => el.removeAttribute("aria-selected"));
  }

  // --- Events ---

  // The whole field is a hit target, not just the arrow. Focus alone opens the
  // listbox, so a first click into the text area arrives already-open and must
  // not toggle -- that would close what the click just opened. A click once the
  // input already holds focus (the common "it did nothing" case) has no focus
  // event to ride, so it opens here. Only the arrow toggles both ways.
  _onWrapClick(e) {
    if (e.composedPath().includes(this._arrow)) {
      e.stopPropagation();
      this._toggle();
      this._input.focus();
      return;
    }
    this._input.focus();
    if (!this.#open) this._open();
  }

  _onInputFocus() {
    this._open();
  }

  _onInputInput() {
    const filter = this._input.value;
    this._renderOptions(filter);
    if (!this.#open) this._open();
  }

  _onInputKeydown(e) {
    const visible = this._getVisibleOptions();

    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        if (!this.#open) { this._open(); return; }
        this._highlight(this.#activeIndex + 1);
        break;
      case "ArrowUp":
        e.preventDefault();
        if (!this.#open) { this._open(); return; }
        this._highlight(this.#activeIndex - 1);
        break;
      case "Enter":
        e.preventDefault();
        if (this.#open && this.#activeIndex >= 0 && visible[this.#activeIndex]) {
          const idx = parseInt(visible[this.#activeIndex].dataset.index, 10);
          this._select(idx);
        } else if (this.#open && this.freeEntry) {
          this._close();
        }
        break;
      case "Escape":
        // Only swallow Escape while our own listbox is open. Otherwise it
        // belongs to whatever contains us -- a dialog, typically -- and
        // stopping it would strand the user with no keyboard way out. When we
        // do consume it, stopPropagation matters: without it the same keypress
        // reaches the dialog's document handler and discards the whole form.
        if (!this.#open) break;
        e.preventDefault();
        e.stopPropagation();
        this._close();
        break;
      case "Tab":
        this._close();
        break;
    }
  }

  _onDocClick(e) {
    if (!this.#open) return;
    if (!this.contains(e.target) && !this.shadowRoot.contains(e.target)) {
      this._close();
    }
  }
}

customElements.define("combo-box", ComboBox);
