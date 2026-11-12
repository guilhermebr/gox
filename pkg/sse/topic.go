package sse

import (
	"fmt"
	"strings"
)

// selector is one ?topic= value of a subscription: "*" (every topic), an
// exact topic, or a template whose {name} segments match any one segment
// and whose trailing {name...} matches the rest, as in net/http patterns:
// "/orders/{id}", "https://example.com/users/{id}/{rest...}".
type selector struct {
	raw      string
	all      bool
	segments []string // nil for an exact topic
}

func parseSelector(s string) (selector, error) {
	if s == "" {
		return selector{}, fmt.Errorf("empty topic")
	}
	if s == "*" {
		return selector{raw: s, all: true}, nil
	}
	if !strings.ContainsAny(s, "{}") {
		return selector{raw: s}, nil
	}
	segs := strings.Split(s, "/")
	for i, seg := range segs {
		if !strings.ContainsAny(seg, "{}") {
			continue
		}
		name, ok := strings.CutPrefix(seg, "{")
		if ok {
			name, ok = strings.CutSuffix(name, "}")
		}
		if !ok || name == "" || strings.ContainsAny(name, "{}") {
			return selector{}, fmt.Errorf("%q: a {name} must fill a whole segment", s)
		}
		if strings.HasSuffix(name, "...") && i != len(segs)-1 {
			return selector{}, fmt.Errorf("%q: {%s} must be the last segment", s, name)
		}
	}
	return selector{raw: s, segments: segs}, nil
}

// topic is a published topic, split into segments once per update so
// template selectors do not split it again for every subscriber.
type topic struct {
	raw   string
	parts []string
}

func splitTopics(topics []string) []topic {
	out := make([]topic, len(topics))
	for i, t := range topics {
		out[i] = topic{raw: t, parts: strings.Split(t, "/")}
	}
	return out
}

func (s selector) match(t topic) bool {
	switch {
	case s.all:
		return true
	case s.segments == nil:
		return s.raw == t.raw
	}
	parts := t.parts
	for i, seg := range s.segments {
		if strings.HasPrefix(seg, "{") {
			if strings.HasSuffix(seg, "...}") {
				return i < len(parts)
			}
			if i >= len(parts) || parts[i] == "" {
				return false
			}
			continue
		}
		if i >= len(parts) || parts[i] != seg {
			return false
		}
	}
	return len(parts) == len(s.segments)
}
