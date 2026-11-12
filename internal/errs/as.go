package errs

import "errors"

func errorsAs(err error, target **Error) bool { return errors.As(err, target) }
