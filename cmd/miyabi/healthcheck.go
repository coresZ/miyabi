package main

import (
	"github.com/ppxb/miyabi/internal/app"
)

func checkHealth(listen string) error {
	return app.CheckHealth(listen)
}
