package server

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSandboxLogCursorCompleteLinesAndFragments(t *testing.T) {
	c := sandboxLogCursor{}
	for _, chunk := range []string{
		"2020-11-03T12:00:", "00.123456789Z", " he", "llo", ".",
	} {
		require.Empty(t, c.advance(chunk))
	}
	require.Equal(t, "2020-11-03T12:00:00.123456789Z", c.advance("\n2020-11-03T20:00:01+08:00 incomplete"))
	require.Equal(t, "2020-11-03T12:00:01Z", c.advance("\n2020-11-03T20:00:01+08:00 same\n"))
	require.Empty(t, c.advance("2020-11-03T12:00:01Z duplicate\n"))
	require.Equal(t, "2020-11-03T12:00:02Z", c.advance("2020-11-03T12:00:02Z next\n"))
	// Out-of-order timestamps conservatively move the cursor backwards.
	require.Equal(t, "2020-11-03T12:00:00Z", c.advance("2020-11-03T12:00:00Z late\n"))
}

func TestSandboxLogCursorInvalidAndLongLines(t *testing.T) {
	c := sandboxLogCursor{}
	require.Empty(t, c.advance("2020-11-03T12:00:00Z "+strings.Repeat("a", 1024*1024)))
	require.Equal(t, "2020-11-03T12:00:00Z", c.advance("\n"))
	require.Empty(t, c.advance("2020-11-03T12:00:00Z duplicate\n"))
	for _, bad := range []string{"not-a-time line", "0000-01-01T00:00:00Z invalid", "2999-01-01T00:00:00Z future", strings.Repeat("1", 100) + " invalid"} {
		t.Run(bad[:10], func(t *testing.T) {
			cursor := sandboxLogCursor{}
			require.Empty(t, cursor.advance("2020-11-03T12:00:00Z before\n"+bad+"\n2020-11-03T12:00:01Z later\n"))
			require.Empty(t, cursor.advance("2020-11-03T12:00:02Z after\n"))
		})
	}
}

func TestSandboxLogCursorDoesNotSkipPartialEarlierOrUnknownLine(t *testing.T) {
	c := sandboxLogCursor{}
	require.Empty(t, c.advance("2020-11-03T12:00:03Z complete\n2020-11-03T12:"))
	require.Equal(t, "2020-11-03T12:00:01Z", c.advance("00:01Z partial"))
	require.Empty(t, c.advance("\n2020-11-03T12:00:02Z more\n"))
	require.Empty(t, c.advance("2020-11-03T12:00:04Z frozen\n"))

	c = sandboxLogCursor{}
	require.Empty(t, c.advance("2020-11-03T12:00:03Z complete\n2020-11-03T"))
	// A later partial timestamp allows the preceding completed line to be
	// exposed after the timestamp has been received.
	require.Equal(t, "2020-11-03T12:00:03Z", c.advance("12:00:04Z partial"))
	// An unparseable subsequent line cannot advance the cursor.
	require.Empty(t, c.advance("\nbroken\n2020-11-03T12:00:05Z later\n"))
}

func TestSandboxLogCursorRetractsWhenEarlierPartialArrivesLater(t *testing.T) {
	c := sandboxLogCursor{}
	require.Equal(t, "2020-11-03T12:00:03Z", c.advance("2020-11-03T12:00:03Z completed\n"))
	require.Empty(t, c.advance("2020-11-03T12:00:"))
	require.Equal(t, "2020-11-03T12:00:01Z", c.advance("01Z partial"))
	require.Empty(t, c.advance("\n2020-11-03T12:00:04Z later\n"))
}

func TestSandboxLogCursorInvalidatesPreviouslyEmittedPosition(t *testing.T) {
	for _, invalid := range []string{"invalid timestamp\n", "2999-01-01T00:00:00Z future\n", strings.Repeat("1", 41)} {
		c := sandboxLogCursor{}
		require.Equal(t, "2020-11-03T12:00:03Z", c.advance("2020-11-03T12:00:03Z complete\n"))
		require.Empty(t, c.advance(invalid))
		require.True(t, c.takeInvalidation())
		require.False(t, c.takeInvalidation())
		require.Empty(t, c.advance("2020-11-03T12:00:04Z later\n"))
	}
	var noPrevious sandboxLogCursor
	require.Empty(t, noPrevious.advance("invalid timestamp\n"))
	require.False(t, noPrevious.takeInvalidation())
}

func TestSandboxLogCursorResumedPosition(t *testing.T) {
	previous, err := time.Parse(time.RFC3339Nano, "2020-11-03T12:00:03.900Z")
	require.NoError(t, err)
	c := sandboxLogCursor{lastEmitted: previous}
	// The Kubernetes API intentionally replays an entire second on resume.
	require.Equal(t, "2020-11-03T12:00:03.1Z", c.advance("2020-11-03T12:00:03.100Z replayed\n"))
	require.False(t, c.blocked)
	require.Equal(t, "2020-11-03T12:00:04Z", c.advance("2020-11-03T12:00:04Z later\n"))

	c = sandboxLogCursor{lastEmitted: previous}
	require.Equal(t, "2020-11-03T12:00:01Z", c.advance("2020-11-03T12:00:01Z earlier partial"))
	require.True(t, c.blocked)
}
