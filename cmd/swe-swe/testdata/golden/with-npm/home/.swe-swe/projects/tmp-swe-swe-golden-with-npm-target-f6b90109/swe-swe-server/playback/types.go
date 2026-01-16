package playback

// PlaybackFrame represents a single frame of terminal content at a specific timestamp
type PlaybackFrame struct {
	Timestamp float64 `json:"timestamp"` // Cumulative time in seconds from start
	Content   string  `json:"content"`   // Terminal content (with ANSI codes preserved)
}
