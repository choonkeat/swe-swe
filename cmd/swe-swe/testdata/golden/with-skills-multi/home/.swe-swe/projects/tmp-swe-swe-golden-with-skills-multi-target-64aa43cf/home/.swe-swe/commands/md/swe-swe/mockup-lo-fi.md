---
description: Draw a lo-fi wireframe sketch (greyscale, deliberately rough) to explore an idea before any pixel-level design. Breadboard first, then sketch, hand over the file path, offer to commit.
---

Make a **lo-fi wireframe sketch** of what the user describes. Everything you
need -- the philosophy, the guardrails, the CSS skeleton, and the workflow -- is
in THIS prompt. The output is a single self-contained HTML file: it carries its
own CSS in an inline `<style>` block, no external files.

User's sketch request: $ARGUMENTS

## The point (read this before drawing anything)

A **lo-fi wireframe** is a deliberately low-detail drawing made *before* real
design work, so people react to the *idea* -- the structure and the flow -- not
the pixels. The idea comes from 37signals / Shape Up
([ch.6 "Sketch the Elements"](https://basecamp.com/shapeup/1.5-chapter-06)):
keep fidelity low so you do not box in the design too early. If your sketch
looks finished, it has failed its job.

**The gravity in most repos pulls the wrong way.** If the project already has
polished mockups (real shells, brand colour, real copy), do NOT imitate them.
Everything under `mockups/lo-fi/` is the opposite on purpose.

## Hard guardrails (these ARE the fidelity -- do not soften them)

- **Greyscale only.** Black text on off-white paper, greys for boxes and
  placeholders. No brand colour, no accent colour. The one allowed "fill" is a
  dark box for a single primary action.
- **Placeholder blocks, not real copy.** Body text is dashed grey blocks
  (`.scribble`). Only *labels that carry meaning* -- a stage name, a button's
  verb, a section title, a field label -- get real words. Real paragraphs of
  text = too hi-fi.
- **No real components.** No shared card classes, no design-system widgets, no
  real inputs. A button is a thin grey box with a verb; an input is an underline
  or a dashed block with a hint.
- **Thin, plain boxes.** 1px grey borders, small radius, no shadows, no
  gradients, no icons beyond a plain arrow/tick/cross character.
- **Structure over polish.** Getting the *places, the flow, and who does what*
  right matters. Alignment and spacing should be tidy enough to read, but do not
  fuss over pixel proportions.

If you feel the urge to add colour, real copy, a real component, or an icon set,
that urge is the signal you are drifting to hi-fi. Stop and keep it plain.

## Steps

### 1. Breadboard first (text, show it to the user)

Before drawing, write a short breadboard -- the plain-text structure Shape Up
uses before visuals:

- **Places** -- the screens / sections / states.
- **Affordances** -- the things a person can do or see on each place (a button,
  a field, a list).
- **Connections** -- what leads to what.

Keep it to a dozen lines. Show it to the user before you spend effort drawing
(in an agent-chat session use `send_progress` so it does not block). If the
request is a single obvious screen, a 3-line breadboard is fine -- do not
over-produce.

### 2. Draw the sketch

Create `mockups/lo-fi/<YYYY-MM>/<DD>-<broad-category>-<kebab-title>.html`
(create the directories if they do not exist; if the name is taken, suffix `-2`,
`-3`). The broad category is one or two words for the area of the product the
sketch belongs to, e.g. `2026-09/06-onboarding-first-run-wizard.html`.

Start from the **Skeleton** below verbatim, then build the body out of the
sketch primitives, based on the breadboard from step 1.

### 3. Hand over the file path

Do NOT start a web server and do NOT take a screenshot. Just tell the user the
path relative to the repo root, e.g.
`mockups/lo-fi/2026-09/06-onboarding-first-run-wizard.html`. swe-swe turns a
relative path into a link the user clicks to open it in the Files tab.

### 4. Offer next steps

ONE question, not a pile: iterate on the sketch, or commit it. **Do not commit
automatically.** If the user says commit: stage ONLY the new file(s) by explicit
path (`git add <path>` -- never `git add -A` or `git add .`), commit as
`docs(mockups): lo-fi sketch -- <title>`, and do NOT push.

## Skeleton (copy verbatim as the top of the file, then fill the body)

````html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Lo-fi - SKETCH NAME</title>
<!-- Source: /swe-swe:mockup-lo-fi -- lo-fi wireframe. Deliberately rough; do not polish. -->
<style>
  :root{
    --ink:#141414;        /* text */
    --paper:#f7f6f2;      /* off-white */
    --grey:#8b8b86;       /* label grey */
    --line:#9a9a94;       /* box lines */
    --grey-lite:#e2e1db;  /* placeholder fill */
  }
  *{box-sizing:border-box;}
  html,body{margin:0;}
  body{
    background:var(--paper);
    color:var(--ink);
    font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;
    padding:28px 24px 80px;
    line-height:1.35;
  }
  .sheet{max-width:1000px;margin:0 auto;}
  .sheet > h1{font-size:21px;margin:0 0 4px;}
  .sheet > .caption{font-size:13px;color:#555;margin:0 0 22px;}

  /* --- plain box: the wireframe building block --- */
  .box{
    border:1px solid var(--line);
    border-radius:3px;
    background:#fff;
    padding:14px 16px;
  }
  .label{font-size:10px;letter-spacing:.08em;text-transform:uppercase;color:var(--grey);}
  .title{font-weight:600;font-size:14px;margin:2px 0 10px;}

  /* --- scribble = "text we are NOT specifying yet" --- */
  .scribble{height:9px;background:var(--grey-lite);border:1px dashed #cfcec8;border-radius:2px;margin:7px 0;}
  .scribble.short{width:45%;}
  .scribble.mid{width:70%;}
  .scribble.long{width:92%;}

  /* --- button: a thin box with a verb, no colour --- */
  .btn{
    display:inline-block;border:1px solid var(--line);border-radius:3px;
    padding:9px 18px;font-weight:600;font-size:14px;background:#fff;text-align:center;
  }
  .btn.filled{background:var(--ink);color:var(--paper);border-color:var(--ink);}  /* the single primary action */

  /* --- input: an underline / dashed block with a hint, never a real field --- */
  .input{border:0;border-bottom:1px solid var(--line);min-width:160px;
         padding:2px 2px 6px;color:var(--grey);font-size:13px;}

  /* --- margin note (plain, greyscale) --- */
  .note{font-size:12px;color:#555;}

  /* --- flow primitives (for workflow / journey sketches) --- */
  .flow{display:flex;flex-wrap:wrap;align-items:center;gap:10px;}
  .node{min-width:118px;max-width:118px;}
  .arrow{font-size:16px;color:var(--grey);flex:0 0 auto;}
  .diamond{
    min-width:96px;max-width:96px;height:96px;
    display:flex;align-items:center;justify-content:center;text-align:center;
    font-weight:600;font-size:12px;background:#fff;
    border:1px solid var(--line);transform:rotate(45deg);border-radius:3px;
  }
  .diamond > span{transform:rotate(-45deg);display:block;}
  .branches{display:flex;flex-wrap:wrap;gap:14px;margin-top:14px;font-size:12px;color:#333;}
</style>
</head>
<body>
<div class="sheet">
  <h1>SKETCH NAME</h1>
  <p class="caption">Lo-fi wireframe -- rough on purpose. Structure &amp; flow only.</p>

  <!-- BUILD THE SKETCH HERE out of: .box (with .label/.title/.scribble),
       .btn, .input, .flow (.node/.arrow/.diamond/.branches), .note.
       Real words ONLY for meaningful labels; everything else is a .scribble. -->

</div>
</body>
</html>
````

## Notes

- The command works for any lo-fi sketch, not just workflows -- a single screen,
  a form, a dashboard. The flow primitives (`.node`/`.arrow`/`.diamond`) are
  there when the thing you are sketching *is* a flow; ignore them otherwise.
- Keep the sketch on ONE self-contained HTML file. No JS unless the idea
  genuinely needs a click to be understood -- and even then, keep it tiny and
  inline.
- If the user's request is vague, draft a sensible structure from the breadboard
  and sketch it -- do not over-ask. One clarifying question at most, and only if
  you truly cannot proceed.
