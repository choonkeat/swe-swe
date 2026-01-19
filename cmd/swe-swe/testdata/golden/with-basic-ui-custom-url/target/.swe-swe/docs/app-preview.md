# App Preview Panel

The terminal UI has a split-pane layout with your app preview on the right side.

## Changing the Preview URL

To update the URL shown in the preview panel, send this escape sequence to the terminal:

```bash
printf '\e]7337;BasicUiUrl=http://localhost:3000\a'
```

Replace `http://localhost:3000` with your app's URL.

## Notes

- The URL must be a valid URL (validated with `new URL()`)
- The sequence is consumed and not displayed in the terminal
- Works with any URL: localhost, remote servers, etc.
