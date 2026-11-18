package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fixedRNG is a deterministic reader: it turns any partial read into a fixed byte
// stream so a test can reproduce a shuffle without crypto/rand.
type fixedRNG struct{ b []byte; i int }

func (f *fixedRNG) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = f.b[f.i%len(f.b)]
		f.i++
	}
	return len(p), nil
}

// seqRNG is a monotonically changing stream so shuffles differ from each other.
type seqRNG struct{ n uint64 }

func (r *seqRNG) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = byte(r.n * 2654435761 >> 13)
	}
	return len(p), nil
}

func testCfg() Config { return Config{HandSize: 7, TurnTimeout: 20 * time.Second, MinPlayers: 2, MaxPlayers: 10} }

// newTestMatch builds a match with a deterministic deck and a known first turn.
func newTestMatch(t *testing.T, deck []Card, players []PlayerInit, opts ...Option) (*State, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	opts = append([]Option{WithDeck(deck)}, opts...)
	s, err := NewMatch("m1", players, testCfg(), &seqRNG{}, now, opts...)
	if err != nil {
		t.Fatalf("NewMatch: %v", err)
	}
	return s, &now
}

// forceHand overrides a hand for a controlled scenario.
func forceHand(s *State, userID int64, cards ...Card) {
	idx := s.findPlayerIdx(userID)
	s.players[idx].Hand = append([]Card(nil), cards...)
}

func forceTop(s *State, c Card, active Color) {
	s.discard = []Card{c}
	s.activeColor = active
}

func forceTurn(s *State, userID int64, deadline time.Time) {
	s.current = s.findPlayerIdx(userID)
	s.turnDeadline = deadline
}

func twoPlayers() []PlayerInit {
	return []PlayerInit{{UserID: 1, Seat: 0}, {UserID: 2, Seat: 1}}
}

// deckTop is a deck whose last element is drawn first (drawOne pops the tail).
func deckWith(tail ...Card) []Card {
	d := make([]Card, 0, len(tail)+60)
	for i := 0; i < 60; i++ {
		d = append(d, Card{Color: Red, Rank: R0})
	}
	d = append(d, tail...)
	return d
}

// R1: the deck is exactly 108 cards with the counts of 02 §Bộ bài.
func TestR1_DeckHas108Cards(t *testing.T) {
	deck := BuildDeck()
	if len(deck) != 108 {
		t.Fatalf("R1: deck size = %d, want 108", len(deck))
	}
	perColor := map[Color]map[Rank]int{}
	wilds := map[Rank]int{}
	for _, c := range deck {
		if c.Color == None {
			wilds[c.Rank]++
			continue
		}
		if perColor[c.Color] == nil {
			perColor[c.Color] = map[Rank]int{}
		}
		perColor[c.Color][c.Rank]++
	}
	for _, color := range []Color{Red, Yellow, Green, Blue} {
		m := perColor[color]
		if m[R0] != 1 {
			t.Errorf("R1: %s has %d zeros, want 1", color, m[R0])
		}
		for r := R1; r <= R9; r++ {
			if m[r] != 2 {
				t.Errorf("R1: %s %s count = %d, want 2", color, r, m[r])
			}
		}
		for _, r := range []Rank{Skip, Reverse, Draw2} {
			if m[r] != 2 {
				t.Errorf("R1: %s %s count = %d, want 2", color, r, m[r])
			}
		}
	}
	if wilds[Wild] != 4 || wilds[Wild4] != 4 {
		t.Errorf("R1: wilds = %d, wild4 = %d, want 4 and 4", wilds[Wild], wilds[Wild4])
	}
}

// R2: the first flipped card is always a number card, even when the deck starts
// with function cards.
func TestR2_FirstFlipIsNumberCard(t *testing.T) {
	// Deck tail is drawn first; make the first five draws function cards.
	tail := []Card{
		{None, Wild}, {None, Wild4}, {Red, Skip}, {Blue, Reverse}, {Green, Draw2},
		{Yellow, R7},
	}
	deck := deckWith(tail...)
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	s, err := NewMatch("m1", twoPlayers(), testCfg(), &seqRNG{}, now, WithDeck(deck))
	if err != nil {
		t.Fatalf("R2: %v", err)
	}
	if top := s.topDiscard(); top.Rank > R9 {
		t.Fatalf("R2: first flip = %v %v, want a number card", top.Color, top.Rank)
	}
}

// R3: REVERSE with two players behaves like SKIP — the player goes again.
func TestR3_ReverseTwoPlayersActsAsSkip(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, Reverse})
	forceHand(s, 2, Card{Blue, R1}, Card{Blue, R2})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "REVERSE"}}, 1, *now); err != nil {
		t.Fatalf("R3: play reverse: %v", err)
	}
	if got := s.players[s.current].UserID; got != 1 {
		t.Fatalf("R3: current = %d, want 1 (same player again)", got)
	}
}

// R4: SKIP and DRAW2 with two players give the turn back to the player who acted.
func TestR4_SkipAndDraw2TwoPlayers(t *testing.T) {
	t.Run("skip", func(t *testing.T) {
		s, now := newTestMatch(t, deckWith(), twoPlayers())
		forceTop(s, Card{Red, R5}, Red)
		forceHand(s, 1, Card{Red, Skip}, Card{Red, R7})
		forceTurn(s, 1, now.Add(20*time.Second))
		if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "SKIP"}}, 1, *now); err != nil {
			t.Fatalf("R4 skip: %v", err)
		}
		if got := s.players[s.current].UserID; got != 1 {
			t.Fatalf("R4 skip: current = %d, want 1", got)
		}
	})
	t.Run("draw2", func(t *testing.T) {
		s, now := newTestMatch(t, deckWith(), twoPlayers())
		forceTop(s, Card{Red, R5}, Red)
		forceHand(s, 1, Card{Red, Draw2}, Card{Red, R7})
		forceHand(s, 2, Card{Blue, R1})
		forceTurn(s, 1, now.Add(20*time.Second))
		if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "DRAW2"}}, 1, *now); err != nil {
			t.Fatalf("R4 draw2: %v", err)
		}
		if got := s.players[s.current].UserID; got != 1 {
			t.Fatalf("R4 draw2: current = %d, want 1", got)
		}
		if n := len(s.players[1].Hand); n != 3 {
			t.Fatalf("R4 draw2: victim hand = %d, want 3 (1 + 2 drawn)", n)
		}
	})
}

// R5: DRAW2 does not stack — the victim cannot answer with their own DRAW2.
func TestR5_Draw2DoesNotStack(t *testing.T) {
	s, now := newTestMatch(t, deckWith(Card{Blue, R9}, Card{Green, R8}), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, Draw2}, Card{Blue, R1})
	forceHand(s, 2, Card{Red, Draw2})
	forceTurn(s, 1, now.Add(20*time.Second))
	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "DRAW2"}}, 1, *now); err != nil {
		t.Fatalf("R5: %v", err)
	}
	// Player 2 drew 2 and lost the turn, so player 1 acts again; player 2's DRAW2
	// stays in hand and was never played as a response.
	if n := len(s.players[1].Hand); n != 3 {
		t.Fatalf("R5: victim hand = %d, want 3", n)
	}
	if got := s.players[s.current].UserID; got != 1 {
		t.Fatalf("R5: current = %d, want 1", got)
	}
}

// R6: after drawing, a different card is refused with MUST_PLAY_DRAWN_CARD.
func TestR6_MustPlayDrawnCard(t *testing.T) {
	s, now := newTestMatch(t, deckWith(Card{Blue, R3}), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, R7})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "draw"}, 1, *now); err != nil {
		t.Fatalf("R6: draw: %v", err)
	}
	_, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "7"}}, 1, *now)
	if err == nil || !strings.Contains(err.Error(), "MUST_PLAY_DRAWN_CARD") {
		t.Fatalf("R6: err = %v, want MUST_PLAY_DRAWN_CARD", err)
	}
}

// R7: passing without drawing is refused with PASS_WITHOUT_DRAW.
func TestR7_PassWithoutDraw(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceTurn(s, 1, now.Add(20*time.Second))
	_, err := s.Apply(Action{Action: "pass"}, 1, *now)
	if err == nil || !strings.Contains(err.Error(), "PASS_WITHOUT_DRAW") {
		t.Fatalf("R7: err = %v, want PASS_WITHOUT_DRAW", err)
	}
}

// R8: an empty draw pile is rebuilt from the discard pile minus its top card.
func TestR8_DeckExhaustedRecyclesDiscard(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers(), WithTopCard(Card{Green, R4}))
	forceHand(s, 1, Card{Blue, R1})
	forceTurn(s, 1, now.Add(20*time.Second))
	// Draw pile empty, discard has several cards with a known top.
	s.deck = nil
	top := Card{Green, R4}
	s.discard = []Card{{Red, R1}, {Red, R2}, {Blue, R3}, top}
	s.activeColor = top.Color

	if _, err := s.Apply(Action{Action: "draw"}, 1, *now); err != nil {
		t.Fatalf("R8: draw: %v", err)
	}
	if got := s.topDiscard(); got != top {
		t.Fatalf("R8: top card = %v, want %v unchanged", got, top)
	}
	if len(s.deck) == 0 {
		t.Fatal("R8: draw pile was not refilled")
	}
}

// R9: both piles empty must not panic and the turn can still end.
func TestR9_BothPilesEmptyNoPanic(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers(), WithTopCard(Card{Red, R5}))
	forceHand(s, 1, Card{Blue, R1})
	forceTurn(s, 1, now.Add(20*time.Second))
	// Keep only the top card in discard, empty deck: nothing can be drawn.
	s.deck = nil
	s.discard = []Card{{Red, R5}}

	if _, err := s.Apply(Action{Action: "draw"}, 1, *now); err != nil {
		t.Fatalf("R9: draw: %v", err)
	}
	if _, err := s.Apply(Action{Action: "pass"}, 1, *now); err != nil {
		t.Fatalf("R9: pass after empty draw: %v", err)
	}
	if got := s.players[s.current].UserID; got != 2 {
		t.Fatalf("R9: current = %d, want 2", got)
	}
}

// R10: timeout before drawing penalizes one card and loses the turn.
func TestR10_TimeoutWithoutDrawDrawsOne(t *testing.T) {
	s, now := newTestMatch(t, deckWith(Card{Blue, R9}), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Blue, R1})
	forceTurn(s, 1, now.Add(20*time.Second))

	before := len(s.players[0].Hand)
	evts := s.Tick(now.Add(21 * time.Second))
	if len(evts) == 0 {
		t.Fatal("R10: no events on timeout")
	}
	if got := len(s.players[0].Hand); got != before+1 {
		t.Fatalf("R10: hand = %d, want %d", got, before+1)
	}
	if got := s.players[s.current].UserID; got != 2 {
		t.Fatalf("R10: current = %d, want 2", got)
	}
}

// R11: timeout after drawing loses the turn without drawing again.
func TestR11_TimeoutAfterDrawNoExtraCard(t *testing.T) {
	s, now := newTestMatch(t, deckWith(Card{Blue, R9}), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Blue, R1})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "draw"}, 1, *now); err != nil {
		t.Fatalf("R11: draw: %v", err)
	}
	afterDraw := len(s.players[0].Hand)
	s.Tick(now.Add(21 * time.Second))
	if got := len(s.players[0].Hand); got != afterDraw {
		t.Fatalf("R11: hand grew from %d to %d", afterDraw, got)
	}
	if got := s.players[s.current].UserID; got != 2 {
		t.Fatalf("R11: current = %d, want 2", got)
	}
}

// R12: a correct challenge makes the Wild4 player draw four and play continues.
func TestR12_ChallengeCorrectDrawerDrawsFour(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{None, Wild4}, Card{Red, R7})
	forceHand(s, 2, Card{Blue, R1}, Card{Blue, R2})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"NONE", "WILD4"}, ChosenColor: "BLUE"}, 1, *now); err != nil {
		t.Fatalf("R12: play wild4: %v", err)
	}
	if s.phase != AwaitingChallenge {
		t.Fatalf("R12: phase = %v, want awaiting_challenge", s.phase)
	}
	if !s.hadMatchingColor {
		t.Fatal("R12: had_matching_color should be true (hand had RED 7)")
	}

	if _, err := s.Apply(Action{Action: "challenge"}, 2, *now); err != nil {
		t.Fatalf("R12: challenge: %v", err)
	}
	if s.phase != Playing {
		t.Fatalf("R12: phase after challenge = %v, want playing", s.phase)
	}
	// Player 1 kept RED 7 and drew 4.
	if got := len(s.players[0].Hand); got != 5 {
		t.Fatalf("R12: wild4 player hand = %d, want 5", got)
	}
	if got := s.players[s.current].UserID; got != 2 {
		t.Fatalf("R12: current = %d, want 2 (the challenger)", got)
	}
}

// R13: a wrong challenge makes the challenger draw six and lose the turn.
func TestR13_ChallengeWrongChallengerDrawsSix(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), []PlayerInit{{1, 0}, {2, 1}, {3, 2}})
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{None, Wild4}, Card{Blue, R7})
	forceHand(s, 2, Card{Blue, R1}, Card{Blue, R2})
	forceHand(s, 3, Card{Green, R1})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"NONE", "WILD4"}, ChosenColor: "BLUE"}, 1, *now); err != nil {
		t.Fatalf("R13: play: %v", err)
	}
	if s.hadMatchingColor {
		t.Fatal("R13: had_matching_color should be false (no RED in hand)")
	}
	if _, err := s.Apply(Action{Action: "challenge"}, 2, *now); err != nil {
		t.Fatalf("R13: challenge: %v", err)
	}
	if got := len(s.players[1].Hand); got != 8 {
		t.Fatalf("R13: challenger hand = %d, want 8 (2 + 6)", got)
	}
	if got := s.players[s.current].UserID; got != 3 {
		t.Fatalf("R13: current = %d, want 3 (after challenger)", got)
	}
}

// R14: the timeout during a pending challenge behaves as accept, with no extra card.
func TestR14_ChallengeTimeoutActsAsAccept(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{None, Wild4}, Card{Red, R7})
	forceHand(s, 2, Card{Blue, R1}, Card{Blue, R2})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"NONE", "WILD4"}, ChosenColor: "BLUE"}, 1, *now); err != nil {
		t.Fatalf("R14: play: %v", err)
	}
	// Wild4 player hand is 1 (RED 7) here, so no win confirmation.
	s.Tick(now.Add(21 * time.Second))
	if got := len(s.players[1].Hand); got != 6 {
		t.Fatalf("R14: challenger hand = %d, want 6 (2 + 4, no extra penalty)", got)
	}
}

// R15: Wild4 as the last card plus accept confirms the win and nobody draws.
func TestR15_Wild4LastCardAcceptWins(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{None, Wild4})
	forceHand(s, 2, Card{Blue, R1}, Card{Blue, R2})
	forceTurn(s, 1, now.Add(20*time.Second))

	evts, err := s.Apply(Action{Action: "play", Card: &WireCard{"NONE", "WILD4"}, ChosenColor: "BLUE"}, 1, *now)
	if err != nil {
		t.Fatalf("R15: play: %v", err)
	}
	if s.phase != AwaitingChallenge {
		t.Fatalf("R15: phase = %v, want awaiting_challenge (win not confirmed yet)", s.phase)
	}
	if len(evts) == 0 || evts[len(evts)-1].Name != "action" {
		t.Fatalf("R15: expected only the play event before confirmation")
	}

	evts, err = s.Apply(Action{Action: "accept"}, 2, *now)
	if err != nil {
		t.Fatalf("R15: accept: %v", err)
	}
	if w, ok := s.Winner(); !ok || w != 1 {
		t.Fatalf("R15: winner = %d (ok=%v), want 1", w, ok)
	}
	if got := len(s.players[1].Hand); got != 2 {
		t.Fatalf("R15: challenger hand = %d, want 2 (nobody draws)", got)
	}
	found := false
	for _, e := range evts {
		if e.Name == "match_end" {
			found = true
		}
	}
	if !found {
		t.Fatal("R15: no match_end event")
	}
}

// R16: Wild4 as the last card plus a correct challenge continues the match.
func TestR16_Wild4LastCardCorrectChallengeContinues(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{None, Wild4}, Card{Red, R2})
	forceHand(s, 2, Card{Blue, R1}, Card{Blue, R2})
	forceTurn(s, 1, now.Add(20*time.Second))

	// Play the Wild4 while holding another card, so it is not the last card yet;
	// then force the last-card state to test the challenge branch.
	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"NONE", "WILD4"}, ChosenColor: "BLUE"}, 1, *now); err != nil {
		t.Fatalf("R16: play: %v", err)
	}
	// Clearing the remaining card models "Wild4 was the last card".
	s.players[0].Hand = nil
	if _, err := s.Apply(Action{Action: "challenge"}, 2, *now); err != nil {
		t.Fatalf("R16: challenge: %v", err)
	}
	if _, ok := s.Winner(); ok {
		t.Fatal("R16: nobody should win when the challenge is correct")
	}
	if s.phase != Playing {
		t.Fatalf("R16: phase = %v, want playing", s.phase)
	}
	if got := len(s.players[0].Hand); got != 4 {
		t.Fatalf("R16: drawer hand = %d, want 4", got)
	}
}

// R17: call_uno is legal with exactly two cards and a playable card on your turn.
func TestR17_CallUnoWithTwoPlayable(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, R7}, Card{Blue, R1})
	forceTurn(s, 1, now.Add(20*time.Second))

	if !s.canCallUno(1) {
		t.Fatal("R17: can_call_uno should be true")
	}
	if _, err := s.Apply(Action{Action: "call_uno"}, 1, *now); err != nil {
		t.Fatalf("R17: call_uno: %v", err)
	}
	if !s.players[0].UnoCalled {
		t.Fatal("R17: uno_called should be set")
	}
}

// R18: the catch window closes once the next player acts.
func TestR18_CatchWindowClosesAfterNextAction(t *testing.T) {
	s, now := newTestMatch(t, deckWith(Card{Green, R9}), []PlayerInit{{1, 0}, {2, 1}, {3, 2}})
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, R7}, Card{Blue, R1})
	forceHand(s, 2, Card{Blue, R2})
	forceHand(s, 3, Card{Green, R3})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "7"}}, 1, *now); err != nil {
		t.Fatalf("R18: play: %v", err)
	}
	if s.canCatchUno(2) == nil {
		t.Fatal("R18: window should be open for player 2")
	}
	// Player 2 acts, which closes the window.
	if _, err := s.Apply(Action{Action: "draw"}, 2, *now); err != nil {
		t.Fatalf("R18: draw: %v", err)
	}
	_, err := s.Apply(Action{Action: "catch_uno"}, 3, *now)
	if err == nil || !strings.Contains(err.Error(), "UNO_WINDOW_CLOSED") {
		t.Fatalf("R18: err = %v, want UNO_WINDOW_CLOSED", err)
	}
}

// R19: catching twice penalizes once; the second attempt is UNO_ALREADY_CALLED.
func TestR19_CatchUnoTwice(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), []PlayerInit{{1, 0}, {2, 1}, {3, 2}})
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, R7}, Card{Blue, R1})
	forceHand(s, 2, Card{Blue, R2})
	forceHand(s, 3, Card{Green, R3})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "7"}}, 1, *now); err != nil {
		t.Fatalf("R19: play: %v", err)
	}
	if _, err := s.Apply(Action{Action: "catch_uno"}, 2, *now); err != nil {
		t.Fatalf("R19: first catch: %v", err)
	}
	handAfter := len(s.players[0].Hand)
	if handAfter != 3 {
		t.Fatalf("R19: victim hand = %d, want 3 (1 + 2)", handAfter)
	}
	_, err := s.Apply(Action{Action: "catch_uno"}, 3, *now)
	if err == nil {
		t.Fatal("R19: second catch should fail")
	}
	if got := len(s.players[0].Hand); got != handAfter {
		t.Fatalf("R19: victim hand changed to %d on second catch", got)
	}
}

// R20: uno_called clears when the hand grows back above one card.
func TestR20_UnoCalledClearsWhenHandGrows(t *testing.T) {
	s, now := newTestMatch(t, deckWith(Card{Green, R9}), []PlayerInit{{1, 0}, {2, 1}, {3, 2}})
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, R7}, Card{Blue, R1})
	forceHand(s, 2, Card{Blue, R2})
	forceHand(s, 3, Card{Green, R3})
	forceTurn(s, 1, now.Add(20*time.Second))

	if _, err := s.Apply(Action{Action: "call_uno"}, 1, *now); err != nil {
		t.Fatalf("R20: call_uno: %v", err)
	}
	if !s.players[0].UnoCalled {
		t.Fatal("R20: flag should be set")
	}
	// Player 2 draws nothing useful, player 3 draws; then player 1 is caught and
	// draws two, which must clear the flag.
	forceTurn(s, 3, now.Add(20*time.Second))
	s.drawNFor(0, 2)
	if s.players[0].UnoCalled {
		t.Fatal("R20: flag should clear when the hand grows above one")
	}
}

// R21: forfeiting with three players puts the hand under the top discard and drops
// the player out of the turn order.
func TestR21_ForfeitPutsHandUnderTopCard(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), []PlayerInit{{1, 0}, {2, 1}, {3, 2}})
	top := Card{Red, R5}
	forceTop(s, top, Red)
	forceHand(s, 1, Card{Red, R7}, Card{Blue, R1})
	forceHand(s, 2, Card{Blue, R2})
	forceHand(s, 3, Card{Green, R3})
	forceTurn(s, 1, now.Add(20*time.Second))
	discardBefore := len(s.discard)

	s.Forfeit(1, *now)
	if got := s.topDiscard(); got != top {
		t.Fatalf("R21: top card = %v, want %v", got, top)
	}
	if got := len(s.discard); got != discardBefore+2 {
		t.Fatalf("R21: discard = %d, want %d", got, discardBefore+2)
	}
	if s.players[0].Status != Forfeit {
		t.Fatal("R21: player should be forfeit")
	}
	if got := s.players[s.current].UserID; got == 1 {
		t.Fatal("R21: a forfeit player must not be current")
	}
	// Turn order skipped the forfeit seat.
	if got := s.liveCount(); got != 2 {
		t.Fatalf("R21: live = %d, want 2", got)
	}
}

// R22: the last remaining player wins the pot even while disconnected.
func TestR22_LastStandingWinsWhileDisconnected(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), []PlayerInit{{1, 0}, {2, 1}, {3, 2}})
	forceTop(s, Card{Red, R5}, Red)
	forceTurn(s, 1, now.Add(20*time.Second))
	s.SetPot(150)

	s.Disconnect(2, *now)
	evts := s.Forfeit(1, *now)
	evts = append(evts, s.Forfeit(3, *now)...)

	if w, ok := s.Winner(); !ok || w != 2 {
		t.Fatalf("R22: winner = %d (ok=%v), want 2", w, ok)
	}
	found := false
	for _, e := range evts {
		if e.Name == "match_end" {
			found = true
		}
	}
	if !found {
		t.Fatal("R22: no match_end")
	}
	if s.players[1].Status != Disconnected {
		t.Fatalf("R22: winner status = %v, want disconnected", s.players[1].Status)
	}
}

// R23: a play arriving exactly at the deadline yields one outcome, not two.
// The real guarantee is the match goroutine's single channel (05 §Thứ tự xử lý);
// at the engine level, whoever is processed first consumes the turn.
func TestR23_PlayAtDeadlineSingleOutcome(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), twoPlayers())
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, R7}, Card{Blue, R1})
	forceHand(s, 2, Card{Blue, R2})
	deadline := now.Add(20 * time.Second)
	forceTurn(s, 1, deadline)

	// A play one second before the deadline is accepted.
	if _, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "7"}}, 1, deadline.Add(-time.Second)); err != nil {
		t.Fatalf("R23: play before deadline: %v", err)
	}
	// The tick that follows must not also penalize the same turn.
	seqAfterPlay := s.Seq()
	if evts := s.Tick(deadline.Add(time.Second)); len(evts) != 0 {
		t.Fatalf("R23: tick produced %d events after the turn was consumed", len(evts))
	}
	if s.Seq() != seqAfterPlay {
		t.Fatalf("R23: seq changed on a stale tick")
	}
}

// R24: seq increases strictly, by one, without gaps, on every emitted event.
func TestR24_SeqMonotonic(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), []PlayerInit{{1, 0}, {2, 1}, {3, 2}})
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, R7}, Card{Blue, R1}, Card{Blue, R4})
	forceHand(s, 2, Card{Blue, R2}, Card{Green, R3})
	forceHand(s, 3, Card{Green, R3}, Card{Yellow, R4})
	forceTurn(s, 1, now.Add(20*time.Second))

	seen := []int{}
	collect := func(evts []Event) {
		for _, e := range evts {
			seen = append(seen, e.Seq)
		}
	}
	evts, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "7"}}, 1, *now)
	if err != nil {
		t.Fatalf("R24: play: %v", err)
	}
	collect(evts)
	evts, err = s.Apply(Action{Action: "draw"}, 2, *now)
	if err != nil {
		t.Fatalf("R24: draw: %v", err)
	}
	collect(evts)
	collect(s.Disconnect(3, *now))
	collect(s.Reconnect(3, *now))

	for i := 1; i < len(seen); i++ {
		if seen[i] != seen[i-1]+1 {
			t.Fatalf("R24: seq jumped %d -> %d", seen[i-1], seen[i])
		}
	}
	if seen[0] != 2 {
		t.Fatalf("R24: first emitted seq = %d, want 2 (match_start is 1)", seen[0])
	}
}

// R25: no emitted payload and no view ever contains a deck array or another
// player's hand.
func TestR25_NoDeckOrForeignHandLeak(t *testing.T) {
	s, now := newTestMatch(t, deckWith(), []PlayerInit{{1, 0}, {2, 1}, {3, 2}})
	forceTop(s, Card{Red, R5}, Red)
	forceHand(s, 1, Card{Red, R7}, Card{Blue, R1})
	forceHand(s, 2, Card{Green, R8}, Card{Yellow, R3})
	forceHand(s, 3, Card{Blue, R4}, Card{Blue, R6})
	forceTurn(s, 1, now.Add(20*time.Second))

	var payloads []string
	marshal := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("R25: marshal: %v", err)
		}
		return string(b)
	}

	evts, err := s.Apply(Action{Action: "play", Card: &WireCard{"RED", "7"}}, 1, *now)
	if err != nil {
		t.Fatalf("R25: play: %v", err)
	}
	for _, e := range evts {
		payloads = append(payloads, marshal(e.Data))
		for _, p := range e.Private {
			payloads = append(payloads, marshal(p))
		}
	}
	evts, err = s.Apply(Action{Action: "draw"}, 2, *now)
	if err != nil {
		t.Fatalf("R25: draw: %v", err)
	}
	for _, e := range evts {
		payloads = append(payloads, marshal(e.Data))
		for _, p := range e.Private {
			payloads = append(payloads, marshal(p))
		}
	}
	for _, uid := range []int64{1, 2, 3} {
		payloads = append(payloads, marshal(s.View(uid, *now)))
	}

	for _, p := range payloads {
		low := strings.ToLower(p)
		if strings.Contains(low, `"deck"`) || strings.Contains(low, `"discard"`) {
			t.Fatalf("R25: payload leaked the pile: %s", p)
		}
	}
	// Player 1's view must not carry player 2's or 3's cards.
	v := marshal(s.View(1, *now))
	if strings.Contains(v, "GREEN") || strings.Contains(v, "YELLOW") {
		t.Fatalf("R25: player 1 view contained another player's cards: %s", v)
	}
	// The public draw event must not carry the drawn card.
	for _, e := range evts {
		if e.Name != "action" {
			continue
		}
		data := marshal(e.Data)
		if strings.Contains(data, `"card"`) && strings.Contains(data, `"action":"draw"`) {
			t.Fatalf("R25: draw event carried a card: %s", data)
		}
	}
	_ = bytes.MinRead
}

// Guard: the shuffle must not be a no-op.
func TestShuffleChangesOrder(t *testing.T) {
	a := BuildDeck()
	b := append([]Card(nil), a...)
	if err := Shuffle(b, &seqRNG{}); err != nil {
		t.Fatalf("shuffle: %v", err)
	}
	same := 0
	for i := range a {
		if a[i] == b[i] {
			same++
		}
	}
	if same == len(a) {
		t.Fatal("shuffle left the deck unchanged")
	}
	// Same multiset.
	counts := map[Card]int{}
	for _, c := range a {
		counts[c]++
	}
	for _, c := range b {
		counts[c]--
	}
	for c, n := range counts {
		if n != 0 {
			t.Fatalf("shuffle lost or duplicated %v (%d)", c, n)
		}
	}
}

// Guard: a fixed RNG reproduces the same match, which the deck hook relies on.
func TestFixedRNGDeterministic(t *testing.T) {
	mk := func() string {
		now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
		s, err := NewMatch("m", twoPlayers(), testCfg(), &fixedRNG{b: []byte{1, 2, 3, 4, 5}}, now)
		if err != nil {
			t.Fatalf("NewMatch: %v", err)
		}
		b, _ := json.Marshal(s.View(1, now))
		return string(b)
	}
	if mk() != mk() {
		t.Fatal("same RNG produced different matches")
	}
	_ = fmt.Sprint()
}

// TestView_DrewThisTurn: the flag is set only for the current player after a draw
// and clears when the turn passes.
func TestView_DrewThisTurn(t *testing.T) {
	s, start := newTestMatch(t, deckWith(), twoPlayers())
	now := start.Add(time.Second)
	cur := s.players[s.current].UserID
	if s.View(cur, now).You.DrewThisTurn {
		t.Fatal("drew_this_turn before drawing")
	}
	if _, err := s.Apply(Action{Action: "draw"}, cur, now); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if !s.View(cur, now).You.DrewThisTurn {
		t.Fatal("drew_this_turn not set after draw")
	}
	if _, err := s.Apply(Action{Action: "pass"}, cur, now); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if s.View(cur, now).You.DrewThisTurn {
		t.Fatal("drew_this_turn still set after pass")
	}
}
