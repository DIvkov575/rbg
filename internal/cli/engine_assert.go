package cli

import "github.com/divkov575/rbg/internal/engine"

// Compile-time proof that the real Engine satisfies the CLI's Ops surface.
var _ Ops = (*engine.Engine)(nil)
