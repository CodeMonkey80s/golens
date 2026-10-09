// Command golens provides structural AST queries for AI coding agents.
//
// It is deliberately thin: compose the engine, parse flags, and translate the returned
// error into the documented exit code. No logic lives here.
//
// This is also where the engine is chosen, so the cli package never imports an engine
// implementation and cobra stays free of go/ast.
package main

import (
	"os"

	"github.com/CodeMonkey80s/golens/internal/cli"
	"github.com/CodeMonkey80s/golens/internal/engine/goast"
)

func main() {
	env := cli.DefaultEnv(goast.New())
	os.Exit(cli.Execute(cli.New(env)))
}
