---
description: Explain $ARGUMENTS item-by-item over agent-chat -- one send_message per item, with Elm type snippets or diff markup where applicable to aid clarity
---

Use `send_message` to explain `$ARGUMENTS` item-by-item, using Elm type snippets
or diff markup where applicable to aid clarity.

One item per `send_message` (it blocks until the reader says "Next"); number the
items "n of N"; end the last one with the next decision, if there is one.
