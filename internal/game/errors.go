package game

import "github.com/unofa/unofa/internal/errs"

// Engine errors (02 §Mã lỗi của engine). They travel as the WS error frame code.
var (
	errNotInMatch        = errs.New(errs.NotInMatch)
	errNotYourTurn       = errs.New(errs.NotYourTurn)
	errChallengePending  = errs.New(errs.ChallengePending)
	errTurnExpired       = errs.New(errs.TurnExpired)
	errCardNotInHand     = errs.New(errs.CardNotInHand)
	errInvalidMove       = errs.New(errs.InvalidMove)
	errMissingColor      = errs.New(errs.MissingColor)
	errMustPlayDrawn     = errs.New(errs.MustPlayDrawnCard)
	errAlreadyDrew       = errs.New(errs.AlreadyDrew)
	errPassWithoutDraw   = errs.New(errs.PassWithoutDraw)
	errCannotCallUno     = errs.New(errs.CannotCallUno)
	errUnoAlreadyCalled  = errs.New(errs.UnoAlreadyCalled)
	errUnoWindowClosed   = errs.New(errs.UnoWindowClosed)
	errMatchEnded        = errs.New(errs.MatchEnded)
	errNotEnoughPlayers  = errs.New(errs.NotEnoughPlayers)
	errEmptyPiles        = errs.New(errs.Internal)
)
