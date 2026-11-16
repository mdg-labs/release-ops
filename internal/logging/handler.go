package logging

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/mdg-labs/release-ops/internal/renovate/redact"
)

// Placeholder replaces every secret in a log line.
const Placeholder = redact.Placeholder

// sensitiveKeys are the attribute and header names whose values are never logged.
var sensitiveKeys = []string{
	"authorization", "proxy-authorization", "cookie", "set-cookie",
	"x-hub-signature", "x-hub-signature-256",
}

// headerLine matches a header written into a message or a value ("Authorization: ...",
// authorization=...) and takes everything after it up to the end of the line, so a scheme or a
// value with spaces in it does not leave a part behind.
var headerLine = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|cookie|set-cookie|x-hub-signature(?:-256)?)(["']?\s*[:=]\s*)[^\r\n]*`)

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "_", "-"))
	return slices.Contains(sensitiveKeys, key)
}

// Handler wraps another slog.Handler and redacts every message, attribute key and attribute
// value before the record reaches it. It holds no output of its own; a record is never passed
// on as it came in.
type Handler struct {
	inner slog.Handler
	reg   *Registry
	ops   []op
}

// op is a WithAttrs or WithGroup call. They are replayed on the wrapped handler for each
// record, so an attribute added before a secret was registered is still redacted.
type op struct {
	group string
	attrs []slog.Attr
}

// NewHandler returns a Handler that redacts what reg holds, and the shapes of tokens and keys,
// from every record before it goes to inner. A nil reg means Default.
func NewHandler(inner slog.Handler, reg *Registry) *Handler {
	if reg == nil {
		reg = Default
	}
	return &Handler{inner: inner, reg: reg}
}

type revealKey struct{}

// Enabled reports whether the wrapped handler logs at level.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle redacts the record and passes it on.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	redactor := h.reg.redactorNow()
	msg := redactMessage(redactor, r.Message, revealed(ctx))
	out := slog.NewRecord(r.Time, r.Level, msg, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(redactor, a))
		return true
	})

	next := h.inner
	for _, o := range h.ops {
		if o.group != "" {
			next = next.WithGroup(redactString(redactor, o.group))
			continue
		}
		attrs := make([]slog.Attr, len(o.attrs))
		for i, a := range o.attrs {
			attrs[i] = redactAttr(redactor, a)
		}
		next = next.WithAttrs(attrs)
	}
	return next.Handle(ctx, out)
}

// WithAttrs returns a Handler that adds attrs to every record.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &Handler{inner: h.inner, reg: h.reg, ops: append(slices.Clip(h.ops), op{attrs: slices.Clone(attrs)})}
}

// WithGroup returns a Handler that nests the attributes that follow under name.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{inner: h.inner, reg: h.reg, ops: append(slices.Clip(h.ops), op{group: name})}
}

// redactString removes the registered secrets, the secret shapes and header lines from s.
func redactString(r *redact.Redactor, s string) string {
	return headerLine.ReplaceAllString(r.String(s), "${1}${2}"+Placeholder)
}

// redactMessage is redactString for a message, leaving the values in reveal as they are: the
// one-time setup token is printed once, in its own record, and nowhere else.
func redactMessage(r *redact.Redactor, msg string, reveal []string) string {
	if len(reveal) == 0 {
		return redactString(r, msg)
	}
	var b strings.Builder
	for msg != "" {
		at, value := -1, ""
		for _, v := range reveal {
			if i := strings.Index(msg, v); i >= 0 && (at < 0 || i < at) {
				at, value = i, v
			}
		}
		if at < 0 {
			b.WriteString(redactString(r, msg))
			break
		}
		b.WriteString(redactString(r, msg[:at]))
		b.WriteString(value)
		msg = msg[at+len(value):]
	}
	return b.String()
}

func revealed(ctx context.Context) []string {
	v, _ := ctx.Value(revealKey{}).([]string)
	return v
}

// redactAttr returns a with its key and value redacted. A value that is not a plain number,
// bool, time or duration is turned into text first, so what it holds is redacted whatever its
// type. An attribute named like a credential header is masked whole.
func redactAttr(r *redact.Redactor, a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		out := make([]slog.Attr, len(attrs))
		for i, g := range attrs {
			out[i] = redactAttr(r, g)
		}
		return slog.Attr{Key: redactString(r, a.Key), Value: slog.GroupValue(out...)}
	}
	key := redactString(r, a.Key)
	if sensitiveKey(a.Key) {
		return slog.String(key, Placeholder)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(key, redactString(r, a.Value.String()))
	case slog.KindInt64, slog.KindUint64, slog.KindFloat64, slog.KindBool, slog.KindDuration, slog.KindTime:
		return slog.Attr{Key: key, Value: a.Value}
	}
	switch v := a.Value.Any().(type) {
	case nil:
		return slog.Attr{Key: key, Value: a.Value}
	case http.Header:
		return redactAttr(r, slog.Attr{Key: key, Value: headerValue(v)})
	case error:
		return slog.String(key, redactString(r, errorText(v)))
	case []byte:
		return slog.String(key, redactString(r, string(v)))
	default:
		return slog.String(key, redactString(r, fmt.Sprintf("%+v", v)))
	}
}

// errorText is err's text. An error whose Error method panics is replaced by a marker instead of
// the panic reaching the caller of the logger.
func errorText(err error) (text string) {
	defer func() {
		if recover() != nil {
			text = "[error text unavailable]"
		}
	}()
	return err.Error()
}

// headerValue turns a header set into a group with one attribute per header, in name order,
// so that the credential headers are masked by name.
func headerValue(h http.Header) slog.Value {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	slices.Sort(names)
	attrs := make([]slog.Attr, 0, len(names))
	for _, name := range names {
		attrs = append(attrs, slog.String(name, strings.Join(h[name], ", ")))
	}
	return slog.GroupValue(attrs...)
}
