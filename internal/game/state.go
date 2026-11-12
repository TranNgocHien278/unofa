package game

import (
	"io"
	"sort"
	"time"
)

// Config holds match configuration (docs/spec/07-ops-admin.md §Config game section).
type Config struct {
	HandSize    int
	TurnTimeout time.Duration
	MinPlayers  int
	MaxPlayers  int
}

// DefaultConfig matches the shipped game section.
var DefaultConfig = Config{HandSize: 7, TurnTimeout: 20 * time.Second, MinPlayers: 2, MaxPlayers: 10}

// PlayerInit seeds a seat.
type PlayerInit struct {
	UserID int64
	Seat   int
}

// PlayerState is one seat's runtime state (02 §State ván).
type PlayerState struct {
	UserID         int64
	Seat           int
	Hand           []Card
	UnoCalled      bool
	Status         Status
	DisconnectedAt *time.Time
}

// State is the whole match. Exactly one goroutine owns it (05 §Thứ tự xử lý).
//
// deck is never serialized, never logged, and never leaves this struct
// (02 §Bảng test bắt buộc R25, 05 §Phân kênh).
type State struct {
	id            string
	seq           int
	phase         Phase
	deck          []Card
	discard       []Card
	activeColor   Color
	direction     int
	current       int
	players       []*PlayerState
	pot           int64
	turnDeadline  time.Time

	pendingChallenger *int64
	wild4Player       *int64
	hadMatchingColor  bool

	drawnThisTurn *Card

	unoWindowVictim *int64

	winner *int64
	rng    io.Reader
	cfg    Config

	// deckFixed is set by WithDeck; it is a harness flag, never serialized.
	deckFixed bool
	// discardFixed skips the first flip so a test can set its own top card.
	discardFixed bool
}

// WithTopCard seeds the discard pile and active color, skipping the first flip.
// Test-only: production always flips a real number card (02 §Bắt đầu ván).
func WithTopCard(c Card) Option {
	return func(s *State) {
		s.discard = []Card{c}
		s.activeColor = c.Color
		s.discardFixed = true
	}
}

// Event is one broadcast step. Data is the public payload; Private maps a user id
// to the payload only that user may see (drawn cards, own hand).
type Event struct {
	Name    string
	Seq     int
	Data    any
	Private map[int64]any
}

// Option customizes NewMatch.
type Option func(*State)

// WithDeck replaces the shuffled deck. Test hook: it is the only way to make a
// match deterministic without changing the crypto/rand path production uses.
func WithDeck(cards []Card) Option {
	return func(s *State) {
		s.deck = append([]Card(nil), cards...)
		s.deckFixed = true
	}
}

// NewMatch deals a new match. rng must be a crypto-grade source
// (02 §Nguồn ngẫu nhiên); tests may pass a deterministic reader.
func NewMatch(id string, players []PlayerInit, cfg Config, rng io.Reader, now time.Time, opts ...Option) (*State, error) {
	if cfg.HandSize == 0 {
		cfg.HandSize = DefaultConfig.HandSize
	}
	if cfg.TurnTimeout == 0 {
		cfg.TurnTimeout = DefaultConfig.TurnTimeout
	}
	if cfg.MinPlayers == 0 {
		cfg.MinPlayers = DefaultConfig.MinPlayers
	}
	if cfg.MaxPlayers == 0 {
		cfg.MaxPlayers = DefaultConfig.MaxPlayers
	}
	if len(players) < cfg.MinPlayers || len(players) > cfg.MaxPlayers {
		return nil, errNotEnoughPlayers
	}

	s := &State{id: id, cfg: cfg, rng: rng, direction: 1, phase: Playing}

	sort.Slice(players, func(i, j int) bool { return players[i].Seat < players[j].Seat })
	for _, pi := range players {
		s.players = append(s.players, &PlayerState{UserID: pi.UserID, Seat: pi.Seat, Status: Connected})
	}

	for _, o := range opts {
		o(s)
	}
	if !s.deckFixed {
		deck := BuildDeck()
		if err := Shuffle(deck, rng); err != nil {
			return nil, err
		}
		s.deck = deck
	}

	// Test hook: an explicitly empty deck skips the deal and the flip so a unit test
	// can drive a hand-crafted pile. Production always ships 108 cards.
	if s.deckFixed && len(s.deck) == 0 && s.discardFixed {
		s.turnDeadline = now.Add(cfg.TurnTimeout)
		s.seq = 1
		return s, nil
	}

	for _, p := range s.players {
		for i := 0; i < cfg.HandSize; i++ {
			c, ok := s.drawOne()
			if !ok {
				break
			}
			p.Hand = append(p.Hand, c)
		}
	}

	// The first flip must be a number card; anything else goes back and the pile
	// is reshuffled (02 §Bắt đầu ván step 3).
	for {
		c, ok := s.drawOne()
		if !ok {
			return nil, errEmptyPiles
		}
		if c.Rank <= R9 {
			s.discard = append(s.discard, c)
			s.activeColor = c.Color
			break
		}
		s.deck = append(s.deck, c)
		if err := Shuffle(s.deck, rng); err != nil {
			return nil, err
		}
	}

	start, err := randN(rng, len(s.players))
	if err != nil {
		return nil, err
	}
	s.current = start

	dirBit, err := randN(rng, 2)
	if err != nil {
		return nil, err
	}
	if dirBit == 1 {
		s.direction = -1
	}

	s.turnDeadline = now.Add(cfg.TurnTimeout)
	s.seq = 1
	return s, nil
}

// ID returns the match id.
func (s *State) ID() string { return s.id }

// Phase returns playing | awaiting_challenge | ended.
func (s *State) Phase() Phase { return s.phase }

// Seq returns the last emitted sequence number.
func (s *State) Seq() int { return s.seq }

// Pot returns the pot amount the engine was told to report.
func (s *State) Pot() int64 { return s.pot }

// SetPot records the pot after the caller moves the bets (the engine never moves money).
func (s *State) SetPot(pot int64) { s.pot = pot }

// Winner returns the confirmed winner.
func (s *State) Winner() (int64, bool) {
	if s.winner == nil {
		return 0, false
	}
	return *s.winner, true
}

// TurnDeadline returns the absolute deadline of the current turn or decision.
func (s *State) TurnDeadline() time.Time { return s.turnDeadline }

// DrawPileCount reports how many cards are left to draw, without exposing them.
func (s *State) DrawPileCount() int { return len(s.deck) }

// --- helpers ---

func (s *State) liveIndices() []int {
	out := make([]int, 0, len(s.players))
	for i, p := range s.players {
		if p.Status != Forfeit {
			out = append(out, i)
		}
	}
	return out
}

func (s *State) liveCount() int { return len(s.liveIndices()) }

func (s *State) findPlayerIdx(userID int64) int {
	for i, p := range s.players {
		if p.UserID == userID {
			return i
		}
	}
	return -1
}

// nextLiveFrom returns the live player index `steps` along direction from fromIdx.
func (s *State) nextLiveFrom(fromIdx, steps int) int {
	live := s.liveIndices()
	n := len(live)
	if n == 0 {
		return fromIdx
	}
	pos := 0
	for i, idx := range live {
		if idx == fromIdx {
			pos = i
			break
		}
	}
	pos = ((pos+s.direction*steps)%n + n) % n
	return live[pos]
}

func (s *State) advanceCurrentBy(steps int) {
	s.current = s.nextLiveFrom(s.current, steps)
}

// drawOne pops one card, recycling the discard pile except its top card when the
// draw pile runs out (D12).
func (s *State) drawOne() (Card, bool) {
	if len(s.deck) == 0 {
		if len(s.discard) <= 1 {
			return Card{}, false
		}
		top := s.discard[len(s.discard)-1]
		recycled := make([]Card, len(s.discard)-1)
		copy(recycled, s.discard[:len(s.discard)-1])
		s.discard = []Card{top}
		s.deck = recycled
		_ = Shuffle(s.deck, s.rng)
	}
	if len(s.deck) == 0 {
		return Card{}, false
	}
	c := s.deck[len(s.deck)-1]
	s.deck = s.deck[:len(s.deck)-1]
	return c, true
}

// drawNFor draws up to n cards into a hand and clears the UNO flag when the hand
// grows back above one card (02 §UNO §Cờ).
func (s *State) drawNFor(idx, n int) []Card {
	p := s.players[idx]
	drawn := make([]Card, 0, n)
	for i := 0; i < n; i++ {
		c, ok := s.drawOne()
		if !ok {
			break
		}
		p.Hand = append(p.Hand, c)
		drawn = append(drawn, c)
	}
	if len(p.Hand) > 1 {
		p.UnoCalled = false
	}
	return drawn
}

func removeFromHand(hand []Card, c Card) ([]Card, bool) {
	for i, h := range hand {
		if h == c {
			out := make([]Card, len(hand)-1)
			copy(out, hand[:i])
			copy(out[i:], hand[i+1:])
			return out, true
		}
	}
	return hand, false
}

func handHas(hand []Card, c Card) bool {
	for _, h := range hand {
		if h == c {
			return true
		}
	}
	return false
}

func (s *State) topDiscard() Card {
	if len(s.discard) == 0 {
		return Card{}
	}
	return s.discard[len(s.discard)-1]
}

func (s *State) setWinner(userID int64) {
	s.phase = Ended
	s.winner = &userID
	s.unoWindowVictim = nil
}

func (s *State) nextSeq() int {
	s.seq++
	return s.seq
}

func (s *State) evt(name string, data any) Event {
	return Event{Name: name, Seq: s.nextSeq(), Data: data}
}

func (s *State) evtPrivate(name string, data any, priv map[int64]any) Event {
	return Event{Name: name, Seq: s.nextSeq(), Data: data, Private: priv}
}

func (s *State) matchEnd(winner int64, reason string) Event {
	return s.evt("match_end", map[string]any{
		"winner_id": winner,
		"pot":       s.pot,
		"reason":    reason,
	})
}

// Apply validates and applies one client action (02 §Thứ tự verify).
func (s *State) Apply(a Action, userID int64, now time.Time) ([]Event, error) {
	return Verify(s, a, userID, now)
}

// Tick handles the 20s timer, which the match goroutine feeds in as a message.
func (s *State) Tick(now time.Time) []Event {
	if s.phase == Ended || now.Before(s.turnDeadline) {
		return nil
	}
	if s.phase == AwaitingChallenge {
		evts, err := s.resolveChallenge(decisionTimeout, now)
		if err != nil {
			return nil
		}
		return evts
	}
	return s.applyTurnTimeout(now)
}

// applyTurnTimeout penalizes the current player: draw one if they have not drawn
// yet this turn, then lose the turn. Never auto-plays a card (02 §Hết giờ).
func (s *State) applyTurnTimeout(now time.Time) []Event {
	cur := s.players[s.current]
	priv := map[int64]any{}
	drew := false
	if s.drawnThisTurn == nil {
		if drawn := s.drawNFor(s.current, 1); len(drawn) > 0 {
			priv[cur.UserID] = map[string]any{"cards": ToWireAll(drawn)}
			drew = true
		}
	}
	s.drawnThisTurn = nil
	s.unoWindowVictim = nil

	s.advanceCurrentBy(1)
	s.turnDeadline = now.Add(s.cfg.TurnTimeout)

	return []Event{s.evtPrivate("action", map[string]any{
		"player_id": cur.UserID,
		"action":    "timeout",
		"drew":      drew,
		"at":        now.Format(time.RFC3339),
	}, priv)}
}

// Disconnect marks a seat disconnected; hand, seat and pot are untouched.
func (s *State) Disconnect(userID int64, now time.Time) []Event {
	idx := s.findPlayerIdx(userID)
	if idx < 0 || s.players[idx].Status != Connected {
		return nil
	}
	s.players[idx].Status = Disconnected
	t := now
	s.players[idx].DisconnectedAt = &t
	return []Event{s.evt("player_status", map[string]any{
		"user_id": userID,
		"status":  "disconnected",
	})}
}

// Reconnect restores a disconnected seat.
func (s *State) Reconnect(userID int64, now time.Time) []Event {
	idx := s.findPlayerIdx(userID)
	if idx < 0 || s.players[idx].Status != Disconnected {
		return nil
	}
	s.players[idx].Status = Connected
	s.players[idx].DisconnectedAt = nil
	return []Event{s.evt("player_status", map[string]any{
		"user_id": userID,
		"status":  "connected",
	})}
}

// Forfeit removes a seat for good: the hand goes under the top discard card, the
// seat leaves the turn order, and the last surviving player wins.
func (s *State) Forfeit(userID int64, now time.Time) []Event {
	if s.phase == Ended {
		return nil
	}
	idx := s.findPlayerIdx(userID)
	if idx < 0 || s.players[idx].Status == Forfeit {
		return nil
	}

	var evts []Event

	// A challenger who leaves while deciding is treated as accepting.
	if s.phase == AwaitingChallenge && s.pendingChallenger != nil && *s.pendingChallenger == userID {
		resolved, err := s.resolveChallenge(decisionAccept, now)
		if err == nil {
			evts = append(evts, resolved...)
		}
		if s.phase == Ended {
			return evts
		}
		idx = s.findPlayerIdx(userID)
		if idx < 0 {
			return evts
		}
	}
	// The Wild4 player leaving while the decision is pending settles as accept too.
	if s.phase == AwaitingChallenge && s.wild4Player != nil && *s.wild4Player == userID {
		resolved, err := s.resolveChallenge(decisionAccept, now)
		if err == nil {
			evts = append(evts, resolved...)
		}
		if s.phase == Ended {
			return evts
		}
		idx = s.findPlayerIdx(userID)
		if idx < 0 {
			return evts
		}
	}

	p := s.players[idx]

	// Hand goes under the top discard so a later pile recycle still uses it.
	if len(p.Hand) > 0 {
		if len(s.discard) > 0 {
			top := s.discard[len(s.discard)-1]
			merged := make([]Card, 0, len(s.discard)+len(p.Hand))
			merged = append(merged, s.discard[:len(s.discard)-1]...)
			merged = append(merged, p.Hand...)
			merged = append(merged, top)
			s.discard = merged
		} else {
			s.discard = append(s.discard, p.Hand...)
		}
		p.Hand = nil
	}

	wasCurrent := s.current == idx
	p.Status = Forfeit
	p.DisconnectedAt = nil
	if s.unoWindowVictim != nil && *s.unoWindowVictim == userID {
		s.unoWindowVictim = nil
	}

	evts = append(evts, s.evt("player_status", map[string]any{
		"user_id": userID,
		"status":  "forfeit",
	}))

	live := s.liveCount()
	if live <= 1 {
		if live == 0 {
			s.phase = Ended
			s.winner = nil
			evts = append(evts, s.evt("match_end", map[string]any{
				"winner_id": nil,
				"pot":       s.pot,
				"reason":    "burn",
			}))
			return evts
		}
		survivor := s.players[s.liveIndices()[0]].UserID
		s.setWinner(survivor)
		evts = append(evts, s.matchEnd(survivor, "last_standing"))
		return evts
	}

	if wasCurrent {
		s.current = s.nextLiveFrom(idx, 1)
		s.drawnThisTurn = nil
		s.turnDeadline = now.Add(s.cfg.TurnTimeout)
	}
	return evts
}
