package devslog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// A Handler handles log records produced by a Logger.
type Handler struct {
	opts slog.HandlerOptions
	mu   *sync.Mutex
	w    io.Writer
	goas []groupOrAttrs
}

// NewHandler creates a handler that writes to w, using the given options.
// If opts is nil, the default options are used.
func NewHandler(w io.Writer, opts *slog.HandlerOptions) *Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &Handler{
		w:    w,
		opts: *opts,
		mu:   &sync.Mutex{},
	}
}

// Enabled reports whether the handler handles records at the given level.
// The handler ignores records whose level is lower.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

// WithAttrs returns a new handler whose attributes consists of h's attributes
// followed by attrs.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) < 1 {
		return h
	}

	return h.withGroupOrAttrs(groupOrAttrs{attrs: attrs})
}

// WithGroup returns a new Handler with the given group appended to
// the receiver's existing groups.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	return h.withGroupOrAttrs(groupOrAttrs{group: name})
}

// Handle formats its argument Record so that message is followed by each
// of it's attributes on seperate lines.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer

	// From slog handler docs:
	// 	If r.Time is the zero time, ignore the time.
	if !r.Time.IsZero() {
		_, _ = buf.WriteString(r.Time.Format(time.TimeOnly) + " ")
	}
	_, _ = buf.WriteString(text(levelColour(r.Level), r.Level.String()) + " " + r.Message + "\n")

	// In this handler, each attribute that is not one of the built-in attributes
	// is written on its own line. For group attributes, use indentation level to
	// display different levels.
	var indentLevel int
	goas := h.goas
	if r.NumAttrs() == 0 {
		// If the record has no Attrs, remove groups at the end of the list; they are empty.
		for len(goas) > 0 && goas[len(goas)-1].group != "" {
			goas = goas[:len(goas)-1]
		}
	}
	for _, goa := range goas {
		if goa.group != "" {
			_, _ = fmt.Fprintf(&buf, "%*s %s %s:\n", indentLevel*numSpacesPerLevel, "", attrPrefix, gray(goa.group))
			indentLevel++
		} else {
			for _, a := range goa.attrs {
				h.appendAttr(&buf, a, indentLevel)
			}
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&buf, a, indentLevel)
		return true
	})

	h.mu.Lock()
	_, err := h.w.Write(buf.Bytes())
	h.mu.Unlock()

	return err
}

const (
	// attrPrefix denotes that another attribute value will be printed in the
	// output. For this handler, it will be preceded by a newline character.
	attrPrefix = "↳"
	// kvd is the key value delimiter output between an attribute's key and value.
	kvd = ":"
	// numSpacesPerLevel is an indentation value for spacing group attributes.
	numSpacesPerLevel = 4
)

func (h *Handler) appendAttr(buf *bytes.Buffer, a slog.Attr, indentLevel int) {
	// From slog handler docs:
	// 	Attr's values should be resolved.
	a.Value = a.Value.Resolve()

	// From slog handler docs:
	// 	If an Attr's key and value are both the zero value, ignore the Attr.
	if a.Equal(slog.Attr{}) {
		return
	}

	_, _ = fmt.Fprintf(buf, "%*s", indentLevel*numSpacesPerLevel, "")
	switch a.Value.Kind() {
	case slog.KindString:
		_, _ = fmt.Fprintf(buf, " %s %s%s %s\n", attrPrefix, gray(a.Key), kvd, a.Value.String())
	case slog.KindTime:
		// Write times in the same layout as the built-in time attribute.
		_, _ = fmt.Fprintf(buf, " %s %s%s %s\n", attrPrefix, gray(a.Key), kvd, a.Value.Time().Format(time.TimeOnly))
	case slog.KindGroup:
		attrs := a.Value.Group()

		// From slog handler docs:
		// 	If a group has no Attrs (even if it has a non-empty key), ignore it.
		if len(attrs) == 0 {
			return
		}

		// If the key is non-empty, write it out and indent the rest of the attrs.
		// Otherwise, inline the attrs.
		if a.Key != "" {
			_, _ = fmt.Fprintf(buf, " %s %s:\n", attrPrefix, gray(a.Key))
			indentLevel++
		}

		for _, ga := range attrs {
			h.appendAttr(buf, ga, indentLevel)
		}
	default:
		_, _ = fmt.Fprintf(buf, " %s %s%s %s\n", attrPrefix, gray(a.Key), kvd, a.Value)
	}
}

// withGroupOrAttrs is for use in the Handler's WithAttrs or WithGroup methods.
// The slog.Handler docs say that those methods must return a new Handler. So
// this method clones the handler state but makes a deep copy of the goas field
// with a new value at the end. The goal is to avoid potentially shared state
// with another handler instance, should either of them append to the same
// underlying array variable. So avoid that situation by making a deep copy.
func (h *Handler) withGroupOrAttrs(goa groupOrAttrs) *Handler {
	out := *h
	out.goas = make([]groupOrAttrs, len(h.goas)+1)
	copy(out.goas, h.goas)
	out.goas[len(out.goas)-1] = goa
	return &out
}

// groupOrAttrs holds either a group name or a list of slog.Attrs.
// It is lifted from the slog-handler-guide at:
// https://github.com/golang/example/blob/master/slog-handler-guide
type groupOrAttrs struct {
	group string      // group name if non-empty
	attrs []slog.Attr // attrs if non-empty
}

// SetDefault is syntactic sugar for constructing a new devslog handler
// and setting it as the default [slog.Logger]. The top-level slog
// functions [slog.Info], [slog.Debug], etc will all use this handler
// to format the records.
func SetDefault(w io.Writer, opts *slog.HandlerOptions) {
	slog.SetDefault(slog.New(NewHandler(w, opts)))
}
