package game

import (
	"encoding/binary"
	"io"
)

// BuildDeck returns the standard 108-card UNO deck.
func BuildDeck() []Card {
	deck := make([]Card, 0, 108)
	colors := []Color{Red, Yellow, Green, Blue}
	for _, c := range colors {
		deck = append(deck, Card{c, R0})
		for r := R1; r <= R9; r++ {
			deck = append(deck, Card{c, r})
			deck = append(deck, Card{c, r})
		}
		deck = append(deck, Card{c, Skip}, Card{c, Skip})
		deck = append(deck, Card{c, Reverse}, Card{c, Reverse})
		deck = append(deck, Card{c, Draw2}, Card{c, Draw2})
	}
	for range 4 {
		deck = append(deck, Card{None, Wild})
		deck = append(deck, Card{None, Wild4})
	}
	return deck
}

// Shuffle performs Fisher-Yates in-place using bytes from rng.
func Shuffle(deck []Card, rng io.Reader) error {
	for i := len(deck) - 1; i > 0; i-- {
		j, err := randN(rng, i+1)
		if err != nil {
			return err
		}
		deck[i], deck[j] = deck[j], deck[i]
	}
	return nil
}

// randN returns a uniform random int in [0, n) using 8 bytes from rng.
func randN(rng io.Reader, n int) (int, error) {
	if n <= 1 {
		return 0, nil
	}
	var b [8]byte
	if _, err := io.ReadFull(rng, b[:]); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint64(b[:])
	return int(v % uint64(n)), nil
}
