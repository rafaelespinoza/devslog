package devslog

import (
	"bytes"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"testing/slogtest"
	"time"
)

func TestSlogtest(t *testing.T) {
	var buf bytes.Buffer
	newHandler := func(t *testing.T) slog.Handler {
		t.Helper()
		buf.Reset()
		return NewHandler(&buf, nil)
	}
	makeTestResults := func(t *testing.T) map[string]any {
		t.Helper()

		got := buf.String()
		t.Log(got)
		return parseMap(t, strings.Split(got, "\n"))
	}

	slogtest.Run(t, newHandler, makeTestResults)
}

// parseMap formats the output lines into a map for the slogtest tests.
func parseMap(t *testing.T, lines []string) map[string]any {
	t.Helper()

	if len(lines) < 2 {
		return nil
	}

	out := make(map[string]any)

	// The first output line with the built-in attributes has a different format
	// than the newline-delimited attributes afterwards. There are 2 or 3
	// attributes on this line, depending on whether or not the time is present.
	switch firstLineParts := strings.Split(lines[0], " "); len(firstLineParts) {
	case 2:
		out[slog.LevelKey] = stripANSI(firstLineParts[0])
		out[slog.MessageKey] = firstLineParts[1]
	case 3:
		out[slog.TimeKey] = firstLineParts[0]
		out[slog.LevelKey] = stripANSI(firstLineParts[1])
		out[slog.MessageKey] = firstLineParts[2]
	default:
		t.Fatalf("unexpected number of parts for first line (%d), expected 2 or 3", len(firstLineParts))
	}

	// Any attributes added via WithAttr, WithGroup, or with the record via the
	// Handle method are formatted 1 attribute per line.
	attrsMap := mapAttrLines(t, lines[1:])
	maps.Copy(out, attrsMap)

	return out
}

// ansiColors are the same const values in color.go.
var ansiColors = []string{
	resetColour,
	colourRed,
	colourYellow,
	colourWhite,
	colorGray,
}

// stripANSI removes ANSI escape codes from line so that we can focus on the
// data more clearly in these tests.
func stripANSI(line string) string {
	for _, color := range ansiColors {
		line = strings.ReplaceAll(line, color, "")
	}
	return line
}

// mapAttrLines reads attribute lines from the handler output into a map for use
// in slogtest tests. It's not meant to interpet the first line of the output;
// that is reserved for the built-in attribute which have a different format
// than the subsequent lines. This function handles the part of the output that
// has 1 attribute per line: attributes that are *not* the built-in ones.
func mapAttrLines(t *testing.T, lines []string) map[string]any {
	t.Helper()

	out := make(map[string]any)

	// A mapContext combines a reference to the map being built and the expected
	// indentation level of its direct children.
	type mapContext struct {
		m map[string]any
		// indent is the number of leading spaces/indentation units for the
		// map's direct children.
		indent int
	}

	// stack aids in tracking the indentation level and to help us build
	// attributes into the correct map.
	stack := []mapContext{{m: out, indent: 0}}

	for _, rawLine := range lines {
		lineSansANSI := stripANSI(rawLine)

		// Trim leading/trailing whitespace and the const `attrPrefix` from the
		// content but preserve leading spaces for measuring indentation.
		trimmedLine := strings.TrimSpace(lineSansANSI)
		trimmedLine = strings.TrimPrefix(trimmedLine, attrPrefix)
		trimmedLine = strings.TrimSpace(trimmedLine)

		if trimmedLine == "" {
			continue // Skip empty or whitespace-only lines
		}

		// Parse the indentation level, key and value in two stages. Use const
		// values known to be in the output lines.
		// 1. Separate the key from the value. The segment that is separate
		//    from the value would also include indentation, if any. Use the
		//    const, keyValDelimiter.
		// 2. Then separate the indentation from the key. Use const, attrPrefix.
		indentPlusKey, rawValue, found := strings.Cut(lineSansANSI, kvd)
		if !found {
			t.Fatalf("invalid line, did not find keyValDelimiter %q, line: %q", kvd, lineSansANSI)
		}

		indent, rawKey, found := strings.Cut(indentPlusKey, attrPrefix)
		if !found {
			t.Fatalf(
				"invalid line, did not find attrPrefix %q in the space before the keyValDelimiter, line_prefix: %q",
				attrPrefix, indentPlusKey,
			)
		}

		currentIndentation := len(indent)
		key := strings.TrimSpace(rawKey)
		value := strings.TrimSpace(rawValue)

		if key == "" {
			t.Fatalf("key cannot be empty, line: %q", lineSansANSI)
		}

		// Pop contexts from the stack until the current line's indentation is
		// >= the current context's indent. Do this to surface the correct map
		// to place the line's key and value.
		for currentIndentation < stack[len(stack)-1].indent {
			if len(stack) <= 1 {
				// Should not happen for valid input because the top-level map
				// has an indent value of 0.
				break
			}
			stack = stack[:len(stack)-1] // Move up to the parent context
		}

		currentContext := stack[len(stack)-1]

		if value != "" {
			// This is a scalar value.
			currentContext.m[key] = value
		} else {
			// An empty value here indicates a group attribute. It's a parent of
			// some child lines, which will have more indentation than the
			// current line. In the slogtest output map we're building, it's
			// represented as another map.
			newMap := make(map[string]any)

			// Add the new map to the current map in the stack.
			currentContext.m[key] = newMap

			// Push a new map context onto the stack. Its children should have
			// the same indentation + numSpacesPerLevel.
			stack = append(stack, mapContext{
				m:      newMap,
				indent: currentIndentation + numSpacesPerLevel,
			})
		}
	}

	return out
}

func TestHandler(t *testing.T) {
	t.Run("times are written in consistent layout", func(t *testing.T) {
		now := time.Date(2009, time.November, 9, 23, 0, 0, 0, time.UTC)
		rec := slog.NewRecord(now, slog.LevelInfo, "msg", 0)
		rec.AddAttrs(slog.Time("foo", now))
		var buf bytes.Buffer

		err := NewHandler(&buf, nil).Handle(t.Context(), rec)
		if err != nil {
			t.Fatal(err)
		}

		lines := buf.String()
		t.Logf("%s", lines)
		parsedMap := parseMap(t, strings.Split(lines, "\n"))

		gotTime, ok := parsedMap[slog.TimeKey]
		if !ok {
			t.Fatalf("expected to find key %s", slog.TimeKey)
		}
		gotFoo, ok := parsedMap["foo"]
		if !ok {
			t.Fatalf("expected to find key %s", "foo")
		}
		if gotFoo != gotTime {
			t.Errorf("expected for time values to be the same; got %v, expected %v", gotFoo, gotTime)
		}
	})
}
