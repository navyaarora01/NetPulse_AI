package main

import (
	"context"

	"netpulse/internal/app"
	"netpulse/internal/cache"
	"netpulse/internal/httpapi"
	"netpulse/internal/messaging"
)

func main() {
	ctx, cancel := app.Context()
	defer cancel()
	c, s, err := app.Setup(ctx)
	app.Fatal(err)
	defer s.Pool.Close()
	bus, err := messaging.New(c, "", "")
	app.Fatal(err)
	defer bus.Client.Close()
	cc := cache.New(c.RedisAddr, c.RedisPassword, c.RedisDB, c.CacheTTL)
	defer cc.Client.Close()
	a := &httpapi.API{Store: s, Bus: bus, Cache: cc, Config: c, Ready: func(ctx context.Context) error {
		if err := s.Pool.Ping(ctx); err != nil {
			return err
		}
		return bus.Client.Ping(ctx)
	}}
	app.Fatal(app.Serve(ctx, c.Addr, a.Handler()))
}
