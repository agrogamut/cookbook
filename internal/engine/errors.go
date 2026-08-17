package engine

import "errors"

// ErrInvalidProfile marks a ChildProfile field the caller must correct -- an unrecognized
// allergen, clinical flag key or cuisine code -- as opposed to a server-side failure.
// Handlers use errors.Is(err, ErrInvalidProfile) to map this to 400 instead of 500: the
// operator typed something the engine can't resolve against the provider masters, and
// that's on the request, not the database or the query.
var ErrInvalidProfile = errors.New("invalid profile")
