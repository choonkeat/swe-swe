package playback

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// RenderPlaybackHTML generates an HTML page with animated terminal playback.
// Uses xterm.js for terminal rendering and includes playback controls.
// If cols/rows are 0, the terminal will auto-fit to the container.
func RenderPlaybackHTML(frames []PlaybackFrame, name, backURL string, cols, rows uint16) (string, error) {
	// Encode frames as base64 to avoid escaping issues
	framesJSON, err := json.Marshal(frames)
	if err != nil {
		return "", err
	}
	framesBase64 := base64.StdEncoding.EncodeToString(framesJSON)

	// Calculate total duration
	var totalDuration float64
	if len(frames) > 0 {
		totalDuration = frames[len(frames)-1].Timestamp
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s - Playback</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css" />
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    html, body {
      height: 100%%;
      background: #1e1e1e;
      color: #d4d4d4;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    }
    .playback-container {
      display: flex;
      flex-direction: column;
      height: 100%%;
      max-width: 1200px;
      margin: 0 auto;
      padding: 16px;
    }
    .header {
      display: flex;
      align-items: center;
      gap: 16px;
      margin-bottom: 16px;
      flex-shrink: 0;
    }
    .back-link {
      color: #9cdcfe;
      text-decoration: none;
      font-size: 14px;
    }
    .back-link:hover { text-decoration: underline; }
    .title {
      font-size: 18px;
      font-weight: 600;
      color: #fff;
    }
    .terminal-wrapper {
      flex: 1;
      min-height: 0;
      background: #000;
      border-radius: 8px;
      overflow: hidden;
      display: flex;
      flex-direction: column;
    }
    #terminal {
      flex: 1;
      padding: 8px;
    }
    .controls {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 12px 16px;
      background: #2d2d2d;
      border-top: 1px solid #404040;
      flex-shrink: 0;
    }
    .play-btn {
      width: 36px;
      height: 36px;
      border: none;
      border-radius: 50%%;
      background: #007acc;
      color: #fff;
      font-size: 14px;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;
      transition: background 0.15s;
    }
    .play-btn:hover { background: #0098ff; }
    .progress-container {
      flex: 1;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .progress-bar {
      flex: 1;
      height: 6px;
      background: #404040;
      border-radius: 3px;
      cursor: pointer;
      position: relative;
    }
    .progress-fill {
      height: 100%%;
      background: #007acc;
      border-radius: 3px;
      width: 0%%;
      transition: width 0.1s linear;
    }
    .time-display {
      font-size: 12px;
      font-family: monospace;
      color: #888;
      min-width: 100px;
      text-align: right;
    }
    .speed-select {
      padding: 4px 8px;
      font-size: 12px;
      background: #404040;
      color: #d4d4d4;
      border: 1px solid #555;
      border-radius: 4px;
      cursor: pointer;
    }
    .speed-select:focus { outline: none; border-color: #007acc; }
  </style>
</head>
<body>
  <div class="playback-container">
    <div class="header">
      <a href="%s" class="back-link">← Back</a>
      <span class="title">%s</span>
    </div>
    <div class="terminal-wrapper">
      <div id="terminal"></div>
      <div class="controls">
        <button id="playBtn" class="play-btn" title="Play/Pause">▶</button>
        <div class="progress-container">
          <div id="progressBar" class="progress-bar">
            <div id="progressFill" class="progress-fill"></div>
          </div>
          <span id="timeDisplay" class="time-display">0:00 / 0:00</span>
        </div>
        <select id="speedSelect" class="speed-select" title="Playback speed">
          <option value="0.5">0.5x</option>
          <option value="1" selected>1x</option>
          <option value="2">2x</option>
          <option value="4">4x</option>
          <option value="8">8x</option>
        </select>
      </div>
    </div>
  </div>

  <script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/xterm-addon-fit@0.8.0/lib/xterm-addon-fit.js"></script>
  <script>
    // Decode frames
    const framesBase64 = '%s';
    const framesJson = new TextDecoder().decode(
      Uint8Array.from(atob(framesBase64), c => c.charCodeAt(0))
    );
    const frames = JSON.parse(framesJson);
    const totalDuration = %f;

    // Terminal dimensions from recording (0 means auto-fit)
    const recordedCols = %d;
    const recordedRows = %d;

    // Initialize terminal with recorded dimensions if available
    const termOptions = {
      fontSize: 14,
      fontFamily: "'SF Mono', 'Menlo', 'Consolas', monospace",
      cursorBlink: false,
      disableStdin: true,
      theme: {
        background: '#000000',
        foreground: '#d4d4d4',
      },
      allowProposedApi: true,
    };
    if (recordedCols > 0) termOptions.cols = recordedCols;
    if (recordedRows > 0) termOptions.rows = recordedRows;

    const xterm = new Terminal(termOptions);
    const fitAddon = new FitAddon.FitAddon();
    xterm.loadAddon(fitAddon);
    xterm.open(document.getElementById('terminal'));

    // Only auto-fit if no recorded dimensions
    if (recordedCols === 0 && recordedRows === 0) {
      fitAddon.fit();
    }

    // Remove textarea to allow page interaction
    const textarea = document.querySelector('.xterm textarea');
    if (textarea) textarea.remove();

    // Playback state
    let isPlaying = false;
    let currentTime = 0;
    let frameIndex = 0;
    let playbackSpeed = 1;
    let lastTimestamp = 0;
    let animationId = null;

    // DOM elements
    const playBtn = document.getElementById('playBtn');
    const progressBar = document.getElementById('progressBar');
    const progressFill = document.getElementById('progressFill');
    const timeDisplay = document.getElementById('timeDisplay');
    const speedSelect = document.getElementById('speedSelect');

    function formatTime(seconds) {
      const mins = Math.floor(seconds / 60);
      const secs = Math.floor(seconds %% 60);
      return mins + ':' + secs.toString().padStart(2, '0');
    }

    function updateProgress() {
      const percent = totalDuration > 0 ? (currentTime / totalDuration) * 100 : 0;
      progressFill.style.width = percent + '%%';
      timeDisplay.textContent = formatTime(currentTime) + ' / ' + formatTime(totalDuration);
    }

    function renderFramesUpTo(targetTime) {
      while (frameIndex < frames.length && frames[frameIndex].timestamp <= targetTime) {
        xterm.write(frames[frameIndex].content);
        frameIndex++;
      }
    }

    function reset() {
      xterm.reset();
      frameIndex = 0;
      currentTime = 0;
      updateProgress();
    }

    function seekTo(time) {
      if (time < currentTime) {
        // Need to reset and replay from start
        xterm.reset();
        frameIndex = 0;
        currentTime = 0;
      }
      currentTime = Math.min(time, totalDuration);
      renderFramesUpTo(currentTime);
      updateProgress();
    }

    function playbackLoop(timestamp) {
      if (!isPlaying) return;

      if (lastTimestamp === 0) {
        lastTimestamp = timestamp;
      }

      const delta = (timestamp - lastTimestamp) / 1000 * playbackSpeed;
      lastTimestamp = timestamp;
      currentTime += delta;

      if (currentTime >= totalDuration) {
        currentTime = totalDuration;
        isPlaying = false;
        playBtn.textContent = '▶';
      }

      renderFramesUpTo(currentTime);
      updateProgress();

      if (isPlaying) {
        animationId = requestAnimationFrame(playbackLoop);
      }
    }

    function togglePlay() {
      if (currentTime >= totalDuration) {
        reset();
      }
      isPlaying = !isPlaying;
      playBtn.textContent = isPlaying ? '⏸' : '▶';
      lastTimestamp = 0;
      if (isPlaying) {
        animationId = requestAnimationFrame(playbackLoop);
      } else if (animationId) {
        cancelAnimationFrame(animationId);
      }
    }

    // Event listeners
    playBtn.addEventListener('click', togglePlay);

    speedSelect.addEventListener('change', function() {
      playbackSpeed = parseFloat(this.value);
    });

    progressBar.addEventListener('click', function(e) {
      const rect = this.getBoundingClientRect();
      const percent = (e.clientX - rect.left) / rect.width;
      seekTo(percent * totalDuration);
    });

    // Keyboard shortcuts
    document.addEventListener('keydown', function(e) {
      if (e.code === 'Space') {
        e.preventDefault();
        togglePlay();
      } else if (e.code === 'ArrowLeft') {
        seekTo(currentTime - 5);
      } else if (e.code === 'ArrowRight') {
        seekTo(currentTime + 5);
      }
    });

    // Handle resize (only auto-fit if no recorded dimensions)
    window.addEventListener('resize', function() {
      if (recordedCols === 0 && recordedRows === 0) {
        fitAddon.fit();
      }
    });

    // Initial state
    updateProgress();

    // Render first frame immediately
    if (frames.length > 0) {
      renderFramesUpTo(0.001);
    }
  </script>
</body>
</html>`, name, backURL, name, framesBase64, totalDuration, cols, rows)

	return html, nil
}

// RenderStaticHTML generates HTML showing the final state (no animation).
// Used when timing data is not available.
func RenderStaticHTML(content []byte, name, backURL string) string {
	// Strip metadata and clean the content
	cleaned := StripScriptMetadata(content)
	cleanedStr := cleanContent(cleaned)
	contentBase64 := base64.StdEncoding.EncodeToString([]byte(cleanedStr))

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s - Recording</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css" />
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    html, body {
      height: 100%%;
      background: #1e1e1e;
      color: #d4d4d4;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    }
    .container {
      display: flex;
      flex-direction: column;
      height: 100%%;
      max-width: 1200px;
      margin: 0 auto;
      padding: 16px;
    }
    .header {
      display: flex;
      align-items: center;
      gap: 16px;
      margin-bottom: 16px;
    }
    .back-link {
      color: #9cdcfe;
      text-decoration: none;
      font-size: 14px;
    }
    .back-link:hover { text-decoration: underline; }
    .title { font-size: 18px; font-weight: 600; color: #fff; }
    .notice {
      padding: 8px 12px;
      background: #332b00;
      border: 1px solid #665500;
      border-radius: 4px;
      color: #ccaa00;
      font-size: 13px;
      margin-bottom: 16px;
    }
    #terminal {
      flex: 1;
      background: #000;
      border-radius: 8px;
      padding: 8px;
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="header">
      <a href="%s" class="back-link">← Back</a>
      <span class="title">%s</span>
    </div>
    <div class="notice">Timing data not available. Showing final state.</div>
    <div id="terminal"></div>
  </div>
  <script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/xterm-addon-fit@0.8.0/lib/xterm-addon-fit.js"></script>
  <script>
    const contentBase64 = '%s';
    const content = new TextDecoder().decode(
      Uint8Array.from(atob(contentBase64), c => c.charCodeAt(0))
    );
    const xterm = new Terminal({
      fontSize: 14,
      cursorBlink: false,
      disableStdin: true,
      theme: { background: '#000000', foreground: '#d4d4d4' },
    });
    const fitAddon = new FitAddon.FitAddon();
    xterm.loadAddon(fitAddon);
    xterm.open(document.getElementById('terminal'));
    fitAddon.fit();
    xterm.write(content);
    const textarea = document.querySelector('.xterm textarea');
    if (textarea) textarea.remove();
    window.addEventListener('resize', () => fitAddon.fit());
  </script>
</body>
</html>`, name, backURL, name, contentBase64)
}
