// Package errs holds the closed error-code set from docs/spec/04-http-api.md §Mã lỗi
// plus the engine-only codes from docs/spec/02-rules-engine.md §Mã lỗi của engine.
// Adding a code means editing the spec first, then this file, then locales/{en,vi}.json.
package errs

import "net/http"

type Code string

const (
	Unauthenticated   Code = "UNAUTHENTICATED"
	SessionExpired    Code = "SESSION_EXPIRED"
	InvalidCredential Code = "INVALID_CREDENTIALS"
	LoginLocked       Code = "LOGIN_LOCKED"
	TermsOutdated     Code = "TERMS_OUTDATED"
	TermsNotAccepted  Code = "TERMS_NOT_ACCEPTED"
	Forbidden         Code = "FORBIDDEN"
	NotFound          Code = "NOT_FOUND"
	UsernameTaken     Code = "USERNAME_TAKEN"
	UsernameInvalid   Code = "USERNAME_INVALID"
	PasswordTooShort  Code = "PASSWORD_TOO_SHORT"
	PasswordTooLong   Code = "PASSWORD_TOO_LONG"
	RateLimited       Code = "RATE_LIMITED"

	InsufficientBalance Code = "INSUFFICIENT_BALANCE"
	TransferLimit       Code = "TRANSFER_LIMIT"
	SelfTransfer        Code = "SELF_TRANSFER"
	UserNotFound        Code = "USER_NOT_FOUND"
	LoanOverdue         Code = "LOAN_OVERDUE"
	LoanActiveExists    Code = "LOAN_ACTIVE_EXISTS"
	LoanNotFound        Code = "LOAN_NOT_FOUND"
	LoanNotActive       Code = "LOAN_NOT_ACTIVE"
	InvalidLoanProduct  Code = "INVALID_LOAN_PRODUCT"
	InvalidStake        Code = "INVALID_STAKE"

	RoomNotFound       Code = "ROOM_NOT_FOUND"
	RoomFull           Code = "ROOM_FULL"
	RoomLocked         Code = "ROOM_LOCKED"
	AlreadyInRoom      Code = "ALREADY_IN_ROOM"
	NotHost            Code = "NOT_HOST"
	NotEnoughPlayers   Code = "NOT_ENOUGH_PLAYERS"
	NotInMatch         Code = "NOT_IN_MATCH"
	MatchEnded         Code = "MATCH_ENDED"
	UserInMatch        Code = "USER_IN_MATCH"
	ServiceUnavailable Code = "SERVICE_UNAVAILABLE"
	Internal           Code = "INTERNAL"

	// Engine-only codes: delivered through the WS error frame.
	NotYourTurn       Code = "NOT_YOUR_TURN"
	ChallengePending  Code = "CHALLENGE_PENDING"
	TurnExpired       Code = "TURN_EXPIRED"
	CardNotInHand     Code = "CARD_NOT_IN_HAND"
	InvalidMove       Code = "INVALID_MOVE"
	MissingColor      Code = "MISSING_COLOR"
	MustPlayDrawnCard Code = "MUST_PLAY_DRAWN_CARD"
	AlreadyDrew       Code = "ALREADY_DREW"
	PassWithoutDraw   Code = "PASS_WITHOUT_DRAW"
	CannotCallUno     Code = "CANNOT_CALL_UNO"
	UnoAlreadyCalled  Code = "UNO_ALREADY_CALLED"
	UnoWindowClosed   Code = "UNO_WINDOW_CLOSED"
)

var status = map[Code]int{
	Unauthenticated: 401, SessionExpired: 401, InvalidCredential: 401,
	LoginLocked: 403, TermsOutdated: 403, TermsNotAccepted: 400, Forbidden: 403,
	NotFound: 404, UsernameTaken: 409, UsernameInvalid: 400,
	PasswordTooShort: 400, PasswordTooLong: 400, RateLimited: 429,
	InsufficientBalance: 400, TransferLimit: 400, SelfTransfer: 400, UserNotFound: 404,
	LoanOverdue: 403, LoanActiveExists: 409, LoanNotFound: 404, LoanNotActive: 400,
	InvalidLoanProduct: 400, InvalidStake: 400,
	RoomNotFound: 404, RoomFull: 409, RoomLocked: 409, AlreadyInRoom: 409,
	NotHost: 403, NotEnoughPlayers: 400, NotInMatch: 403, MatchEnded: 409,
	UserInMatch: 409, ServiceUnavailable: 503, Internal: 500,
}

// Error is a coded error. It never carries a technical message: the wire body is
// {"error": code, "request_id": ...} and the client translates the code.
type Error struct {
	Code       Code
	RetryAfter int // seconds; only set for RATE_LIMITED / LOGIN_LOCKED
}

func (e *Error) Error() string { return string(e.Code) }

func New(c Code) *Error { return &Error{Code: c} }

func NewRetry(c Code, seconds int) *Error { return &Error{Code: c, RetryAfter: seconds} }

// Status returns the HTTP status for a code. Engine-only codes default to 400.
func Status(c Code) int {
	if s, ok := status[c]; ok {
		return s
	}
	return http.StatusBadRequest
}

// As extracts a coded error from err, falling back to INTERNAL.
func As(err error) *Error {
	var e *Error
	if errorsAs(err, &e) {
		return e
	}
	return &Error{Code: Internal}
}
