package server

import "time"

// sandboxLogCursor extracts bounded timestamp prefixes of Kubernetes log
// lines. A cursor is advisory: Kubernetes can replay a whole second and its
// logs do not provide offsets or a stable record identifier. Ordinarily only
// complete lines advance the cursor; a partial, earlier line may move it back.
type sandboxLogCursor struct {
	prefix         [40]byte
	n              int
	done           bool
	inLine         bool
	lineTime       time.Time
	lastSeen       time.Time
	lastComplete   time.Time
	minComplete    time.Time
	lastEmitted    time.Time
	sentThisStream bool
	disordered     bool
	blocked        bool
	invalidated    bool
}

func (c *sandboxLogCursor) invalidate() {
	c.blocked = true
	c.invalidated = !c.lastEmitted.IsZero()
}

func (c *sandboxLogCursor) takeInvalidation() bool {
	invalidated := c.invalidated
	c.invalidated = false
	return invalidated
}

func (c *sandboxLogCursor) advance(text string) string {
	if c.blocked {
		return ""
	}
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '\n' {
			if c.lineTime.IsZero() {
				// We cannot safely advance past a line whose timestamp was
				// missing, malformed, or ahead of this server's clock.
				c.invalidate()
				break
			}
			c.lastComplete = c.lineTime
			if c.minComplete.IsZero() || c.lineTime.Before(c.minComplete) {
				c.minComplete = c.lineTime
			}
			c.n, c.done, c.inLine = 0, false, false
			c.lineTime = time.Time{}
			continue
		}
		c.inLine = true
		if c.done {
			continue
		}
		if ch == ' ' {
			c.done = true
			parsed, err := time.Parse(time.RFC3339Nano, string(c.prefix[:c.n]))
			if err != nil || parsed.Year() < 1 || parsed.IsZero() || parsed.After(time.Now()) {
				c.invalidate()
				break
			}
			c.lineTime = parsed.UTC()
			if !c.lastSeen.IsZero() && c.lineTime.Before(c.lastSeen) ||
				c.lastSeen.IsZero() && !c.lastEmitted.IsZero() && c.lineTime.Before(c.lastEmitted.Truncate(time.Second)) {
				c.disordered = true
			}
			c.lastSeen = c.lineTime
			continue
		}
		if c.n == len(c.prefix) {
			c.invalidate()
			break
		}
		c.prefix[c.n] = ch
		c.n++
	}
	if c.blocked {
		return ""
	}
	// A just-observed partial line can be older than a cursor sent in an
	// earlier frame. Immediately move the cursor back to its inclusive time;
	// waiting for its newline would leave a known gap on disconnect.
	if c.disordered && c.inLine && !c.lineTime.IsZero() && (c.minComplete.IsZero() || c.lineTime.Before(c.minComplete)) {
		c.blocked = true
		if c.lineTime.Equal(c.lastEmitted) && c.sentThisStream {
			return ""
		}
		c.lastEmitted = c.lineTime
		c.sentThisStream = true
		return c.lineTime.Format(time.RFC3339Nano)
	}
	if c.lastComplete.IsZero() {
		return ""
	}
	candidate := c.lastComplete
	if c.disordered {
		candidate = c.minComplete
	}
	// A frame can contain a complete line followed by part of another line.
	// Do not advance until the partial timestamp is known. Keep the
	// complete-line candidate across gRPC frames.
	if c.inLine && (c.lineTime.IsZero() || c.lineTime.Before(candidate)) {
		return ""
	}
	c.lastComplete, c.minComplete = time.Time{}, time.Time{}
	if c.disordered {
		// Once source order is known to be unreliable, publish at most the
		// earliest completed position and stop advancing for this stream.
		c.blocked = true
	}
	if candidate.Equal(c.lastEmitted) && c.sentThisStream {
		return ""
	}
	c.lastEmitted = candidate
	c.sentThisStream = true
	return candidate.Format(time.RFC3339Nano)
}
