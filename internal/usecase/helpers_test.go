package usecase

import (
	"errors"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/engine/goast"
)

// newEngine returns the production engine. Tests exercise the real parser rather than a
// stub, because the byte-offset contract is a property of go/parser's position model and
// a stub would not verify it.
func newEngine() domain.Engine { return goast.New() }

// asUsage extracts a usage-classified error.
func asUsage(err error, target *interface{ IsUsage() bool }) bool {
	return errors.As(err, target)
}

// asNotFound extracts a not-found error.
func asNotFound(err error, target *NotFoundSymbol) bool {
	return errors.As(err, target)
}

// asAmbiguous extracts an ambiguity error.
func asAmbiguous(err error, target *AmbiguousSymbol) bool {
	return errors.As(err, target)
}

// asHasDiags extracts the diagnostics-found sentinel.
func asHasDiags(err error, target *HasDiagnostics) bool {
	return errors.As(err, target)
}
