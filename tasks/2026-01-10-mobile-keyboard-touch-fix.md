# Fix: Mobile Keyboard Not Receiving Touch Events

## Problem

After merging the mobile touch scroll proxy feature, tapping the mobile keyboard buttons focuses xterm instead of triggering the button actions.

## Root Cause

The touch scroll proxy has `position: absolute; inset: 0;` which covers the **entire** `.terminal-ui` container, including the mobile keyboard at the bottom. On touch devices, the proxy has `pointer-events: auto` and a click handler that calls `this.term.focus()`.

**Prototype structure** (worked):
```html
<div class="container">
    <div class="terminal-wrapper">
        <div id="terminal"></div>
        <div class="touch-scroll-proxy">...</div>  <!-- constrained to wrapper -->
    </div>
    <div class="mobile-keyboard">...</div>  <!-- outside wrapper, not covered -->
</div>
```

**Our structure** (broken):
```html
<div class="terminal-ui">
    <div class="terminal-ui__terminal"></div>
    <div class="touch-scroll-proxy">...</div>  <!-- inset:0 covers entire parent -->
    <div class="mobile-keyboard">...</div>  <!-- covered by proxy above -->
</div>
```

## Options

### Option A: Z-index fix (quick)

Add higher z-index and pointer-events to mobile keyboard:

```css
.mobile-keyboard {
    position: relative;
    z-index: 20;  /* higher than proxy's z-index: 10 */
    pointer-events: auto;
}
```

**Pros:** Minimal change, one-line fix
**Cons:** Relies on z-index layering, less explicit

### Option B: Restructure HTML (architectural)

Wrap terminal and proxy in a sub-container like the prototype:

```html
<div class="terminal-ui">
    <div class="terminal-ui__viewport">  <!-- new wrapper -->
        <div class="terminal-ui__terminal"></div>
        <div class="touch-scroll-proxy">...</div>
    </div>
    <div class="mobile-keyboard">...</div>
</div>
```

With CSS:
```css
.terminal-ui__viewport {
    position: relative;
    flex: 1;
    min-height: 0;
}
```

**Pros:** Matches validated prototype structure, proxy naturally constrained
**Cons:** More changes, need to move styles from `__terminal` to `__viewport`

## Recommendation

**Option A** for now - it's a simple fix that works. Option B can be done later if we encounter more layering issues.

## Current Status

Option A has been applied but not yet tested.
