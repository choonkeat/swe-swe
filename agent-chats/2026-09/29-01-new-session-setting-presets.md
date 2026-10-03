<!-- agent-chat export
title: New Session Setting Presets
date: 2026-09-29
index: 01
slug: new-session-setting-presets
session: 28e287e86de50f0a
agent: claude
version: 0.14.0 (9ae2ce3)
-->

# New Session Setting Presets

_2026-09-29 · 01 · claude · agent-chat 0.14.0 (9ae2ce3)_

**USER**

> Wondering if there’s a way to easily apply different sets of New Session settings. Propose options and /swe-swe:mockup-lo-fi if needed

<small>took 6.4s</small><br>
**AGENT**

> Reading the New Session dialog code to see which settings it has today.

<small>took 6.5s</small><br>
**AGENT**

> Step 1 of 3: listing every setting the New Session window asks for (about 3 minutes).

<small>took 10.5s</small><br>
**AGENT**

> Step 2 of 3: drawing a rough sketch. Plan for it:
>
> 1. Top of the New Session window: a "Preset" picker (a preset = a saved bundle of settings with a name).
> 2. Picking one fills in: project, branch, agent, colour, extra agent options, terminal or chat.
> 3. Bottom: "Save these settings as a preset".
> 4. A separate "Manage presets" list: rename, delete, copy link.

<small>took 36.5s</small><br>
**AGENT**

> Open the sketch: mockups/lo-fi/2026-09/29-new-session-setting-presets.html
>
> Step 3 of 3 done: 3 options, plus the sketch for option 1.
>
> **Settings the New Session window has today:** project (Where), branch, agent, colour, extra agent options, and whether it opens as terminal or chat. Only colour and past projects are remembered today, and only in this browser.
>
> **Option 1: Named presets (recommended)**
> - A row of buttons along the top of the window: "Bug fix", "Docs", and so on. A preset is a saved bundle of settings with a name.
> - Click one and every field fills in. You can still change any field before Start.
> - "Save as preset..." at the bottom saves the current fields. You pick which fields it keeps. Unticked ones are left alone.
> - Size: about 1 to 2 days of work.
>
> **Option 2: A link for each setup**
> - A web address that opens New Session already filled in. Bookmark it, or send it to your phone.
> - The "+ New" button on a past recording already fills in the window this way, so most of the plumbing exists.
> - Size: about half a day. In the sketch it is the "Copy link" button, so it can ship first and grow into Option 1.
>
> **Option 3: Remember last time, per project**
> - Pick a project and it brings back the agent, extra options and terminal/chat you used last time for that project.
> - No new buttons. Only covers "same as last time", though, not two different setups for one project.
> - Size: about half a day.
>
> Limit: presets would live in this browser only (same as colour today). They won't follow you to another device unless you use Option 2's link.
>
> Next step (1 minute): open the sketch and say "iterate" with changes, or "commit" to save it to git.

[Quick replies]
- Commit the sketch
- Iterate on the sketch
- Build option 2 first
- Build option 1

**USER**

> Giving it a name at Session settings modal instead. Choose what to keep eg may want env and model but not worktree branch name? /swe-swe:mockup-lo-fi

<small>took 3.4s</small><br>
**AGENT**

> Step 1 of 3: reading what the Session settings window holds, so the "save as preset" sketch lists the right fields.

<small>took 12.7s</small><br>
**AGENT**

> Step 2 of 3: drawing it. Plan:
>
> 1. Session settings window gets a new "Save as preset" tab.
> 2. You type a name, then tick what to keep. Each line shows this session's actual value.
> 3. Ticked by default: agent, model and other agent options, environment variables, colour, terminal/chat. Unticked by default: branch name.
> 4. New Session window shows your saved presets as buttons along the top.

<small>took 24.6s</small><br>
**AGENT**

> Open the new sketch: mockups/lo-fi/2026-09/29-session-settings-save-preset.html
>
> Step 3 of 3 done: sketch drawn. The first sketch is still in the same folder.
>
> **What it shows:**
> 1. Session settings gets a 7th tab, "Save as preset". A preset is a saved bundle of settings with a name.
> 2. You type a name, then tick what to keep. Each line shows this session's actual value, so you can see what you're saving.
> 3. Ticked by default: project, agent, model and other agent options, environment variables (4 variables, values hidden), colour, terminal or chat.
> 4. Unticked by default: branch name and session name. Those stay blank in New Session for you to type.
> 5. Under that is a list of saved presets, with "Update from this session" and delete buttons.
> 6. New Session shows the presets as buttons along the top. Click one, type a branch, Start.
>
> **One decision for you:** today, environment variables are stored per project (Settings says "Variables for this repo"). If a preset keeps them too, there are two copies, and changing one won't update the other. The other choice is for a preset to only point at the project's current variables.
>
> Next step (1 minute): reply "copy" or "point" for the environment variables, or "commit" to save both sketches to git.

[Quick replies]
- Point at project's variables
- Copy into preset
- Commit both sketches
- Iterate on sketch

**USER**

> For `New Session` to support the environment variables of these presets.. i guess `New Session` itself should start supporting environment variables under Advanced first? Discuss

<small>took 3.0s</small><br>
**AGENT**

> Step 1 of 2: checking how New Session handles environment variables today.

<small>took 16.8s</small><br>
**AGENT**

> Yes, add environment variables to New Session's Advanced section first. It's the smaller step, and presets need it anyway.
>
> Step 2 of 2 done: checked how New Session handles environment variables today.
>
> **What happens today (you can't see it):**
> 1. The project's variables saved in Session settings are sent along when you press Start. The box is hidden, so you can't see them or change them.
> 2. The file `.swe-swe/env` in the project is also loaded. New Session only shows a one-line hint about it.
> 3. The "Archive chat log" checkbox is really a hidden variable that gets added to the same list.
>
> **Proposal: a variables box in "Appearance & advanced":**
> 1. It opens already filled with this project's saved variables, so you can see what will be sent.
> 2. Edits apply to this new session only.
> 3. A tick box, "Also save for this project", turns an edit into a lasting change. It's off by default.
> 4. A one-line note underneath says `.swe-swe/env` is also loaded, and lists which names it sets.
> 5. Size: about half a day, plus about 2 hours of tests.
>
> **How this answers the preset question from last time:** a preset just fills this box, the same way it fills the branch or options box. So "copy vs point" becomes simple:
> - The project's saved variables still load first.
> - A preset only holds its extra or changed lines, added on top of the project's variables.
> - Changing the project's variables later still reaches sessions started from the preset.
>
> Order of work:
> 1. Variables box in New Session (about half a day).
> 2. Save as preset in Session settings, and the preset buttons in New Session (about 1 to 2 days).
>
> Next step (1 minute): reply "sketch it" and I'll draw the Advanced section with the variables box, or "go" and I'll start building step 1.

[Quick replies]
- Sketch it
- Go, build step 1
- Commit both sketches first
- Discuss more

**USER**

> /commit-log-then-end

<small>took 1.8s</small><br>
**AGENT**

> Saving this session's chat log, then ending the session.

