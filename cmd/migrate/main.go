package main

import (
	"netpulse/internal/app"
)

func main() {
	ctx, cancel := app.Context()
	defer cancel()
	_, s, err := app.Setup(ctx)
	app.Fatal(err)
	defer s.Pool.Close()
	app.Fatal(s.Migrate(ctx))
}
