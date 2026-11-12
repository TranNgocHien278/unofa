// Package game is the pure rules engine: cards, deck, and the per-match state
// machine. It performs no I/O and never serializes Deck or another player's hand
// (docs/spec/02-rules-engine.md §State ván). One goroutine per match drives it.
package game

import (
	"fmt"
	"strconv"
)

type Color uint8

const (
	Red Color = iota
	Yellow
	Green
	Blue
	None
)

var colorNames = [5]string{"RED", "YELLOW", "GREEN", "BLUE", "NONE"}

func (c Color) String() string {
	if int(c) < len(colorNames) {
		return colorNames[c]
	}
	return "NONE"
}

func ParseColor(s string) (Color, bool) {
	for i, n := range colorNames {
		if n == s {
			return Color(i), true
		}
	}
	return None, false
}

// MarshalJSON emits the wire name, e.g. "RED". NONE is never written to clients.
func (c Color) MarshalJSON() ([]byte, error) { return []byte(`"` + c.String() + `"`), nil }

type Rank uint8

const (
	R0 Rank = iota
	R1
	R2
	R3
	R4
	R5
	R6
	R7
	R8
	R9
	Skip
	Reverse
	Draw2
	Wild
	Wild4
)

func (r Rank) String() string {
	switch {
	case r <= R9:
		return strconv.Itoa(int(r))
	case r == Skip:
		return "SKIP"
	case r == Reverse:
		return "REVERSE"
	case r == Draw2:
		return "DRAW2"
	case r == Wild:
		return "WILD"
	case r == Wild4:
		return "WILD4"
	}
	return "?"
}

// Label is the English aria-label fragment used by the frontend, e.g. "Wild Draw Four".
func (r Rank) Label() string {
	switch r {
	case Skip:
		return "Skip"
	case Reverse:
		return "Reverse"
	case Draw2:
		return "Draw Two"
	case Wild:
		return "Wild"
	case Wild4:
		return "Wild Draw Four"
	}
	return r.String()
}

func ParseRank(s string) (Rank, bool) {
	switch s {
	case "SKIP":
		return Skip, true
	case "REVERSE":
		return Reverse, true
	case "DRAW2":
		return Draw2, true
	case "WILD":
		return Wild, true
	case "WILD4":
		return Wild4, true
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 9 {
		return Rank(n), true
	}
	return 0, false
}

func (r Rank) MarshalJSON() ([]byte, error) { return []byte(`"` + r.String() + `"`), nil }

type Card struct {
	Color Color `json:"color"`
	Rank  Rank  `json:"rank"`
}

// WireCard is the JSON shape sent to clients. Wild cards carry color "NONE".
type WireCard struct {
	Color string `json:"color"`
	Rank  string `json:"rank"`
}

func ToWire(c Card) WireCard { return WireCard{Color: c.Color.String(), Rank: c.Rank.String()} }

func ToWireAll(cs []Card) []WireCard {
	out := make([]WireCard, len(cs))
	for i, c := range cs {
		out[i] = ToWire(c)
	}
	return out
}

func FromWire(w WireCard) (Card, error) {
	col, ok := ParseColor(w.Color)
	if !ok {
		return Card{}, fmt.Errorf("bad color %q", w.Color)
	}
	rank, ok := ParseRank(w.Rank)
	if !ok {
		return Card{}, fmt.Errorf("bad rank %q", w.Rank)
	}
	if (rank == Wild || rank == Wild4) && col != None {
		return Card{}, fmt.Errorf("wild card with color %q", w.Color)
	}
	if rank != Wild && rank != Wild4 && col == None {
		return Card{}, fmt.Errorf("colored card without color")
	}
	return Card{Color: col, Rank: rank}, nil
}

type Phase uint8

const (
	Playing Phase = iota
	AwaitingChallenge
	Ended
)

func (p Phase) String() string {
	switch p {
	case Playing:
		return "playing"
	case AwaitingChallenge:
		return "awaiting_challenge"
	case Ended:
		return "ended"
	}
	return "playing"
}

type Status uint8

const (
	Connected Status = iota
	Disconnected
	Forfeit
)

func (s Status) String() string {
	switch s {
	case Connected:
		return "connected"
	case Disconnected:
		return "disconnected"
	case Forfeit:
		return "forfeit"
	}
	return "connected"
}

// Action is a client action as received on the wire.
type Action struct {
	Ref         string    `json:"ref"`
	MatchID     string    `json:"match_id"`
	Action      string    `json:"action"`
	Card        *WireCard `json:"card,omitempty"`
	ChosenColor string    `json:"chosen_color,omitempty"`
}
