#!/usr/bin/env bash
# colors.sh — Display readable ANSI color pairs with sufficient contrast.
# Shows all fg/bg combinations from the 16 basic ANSI colors that meet
# WCAG AA contrast ratio (>= 4.5:1), commonly used in TUI coding agents.

# Standard ANSI 16-color palette RGB values and contrast computation
# done in a single awk pass, then rendered with ANSI escapes.

awk 'BEGIN {
  # 16 standard ANSI colors: R G B
  split("0 170 0 170 0 170 0 170 85 255 85 255 85 255 85 255", R)
  split("0 0 170 170 0 0 170 170 85 85 255 255 85 85 255 255", G)
  split("0 0 0 0 170 170 170 170 85 85 85 85 255 255 255 255", B)
  split("black red green yellow blue magenta cyan white br-black br-red br-green br-yellow br-blue br-magenta br-cyan br-white", names)

  # Precompute relative luminance (WCAG 2.0)
  for (i = 1; i <= 16; i++) {
    r = R[i] / 255.0; g = G[i] / 255.0; b = B[i] / 255.0
    rl = (r <= 0.03928) ? r / 12.92 : exp(2.4 * log((r + 0.055) / 1.055))
    gl = (g <= 0.03928) ? g / 12.92 : exp(2.4 * log((g + 0.055) / 1.055))
    bl = (b <= 0.03928) ? b / 12.92 : exp(2.4 * log((b + 0.055) / 1.055))
    lum[i] = 0.2126 * rl + 0.7152 * gl + 0.0722 * bl
  }

  # ANSI escape code helpers: fg 0-7→30-37, 8-15→90-97; bg 0-7→40-47, 8-15→100-107
  min_ratio = 4.5
  count = 0

  printf "%-12s  %-12s  %5s  %-s\n", "FOREGROUND", "BACKGROUND", "RATIO", "SAMPLE"
  for (k = 0; k < 72; k++) sep = sep "─"
  print sep

  for (fg = 1; fg <= 16; fg++) {
    for (bg = 1; bg <= 16; bg++) {
      if (fg == bg) continue

      l1 = lum[fg]; l2 = lum[bg]
      if (l1 < l2) { t = l1; l1 = l2; l2 = t }
      ratio = (l1 + 0.05) / (l2 + 0.05)

      if (ratio >= min_ratio) {
        # Build ANSI codes (0-indexed color)
        fc = fg - 1; bc = bg - 1
        fcode = (fc < 8) ? 30 + fc : 90 + fc - 8
        bcode = (bc < 8) ? 40 + bc : 100 + bc - 8

        sample = "  The quick brown fox jumps  "
        printf "%-12s  %-12s  %5.1f  \033[%d;%dm%s\033[0m\n", \
          names[fg], names[bg], ratio, fcode, bcode, sample
        count++
      }
    }
  }

  print sep
  printf "%d readable pairs (WCAG AA contrast ratio >= %.1f:1)\n", count, min_ratio
}'
