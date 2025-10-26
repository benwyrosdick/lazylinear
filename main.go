package main

import (
	"log"

	"github.com/benwyrosdick/lazylinear/internal/api"
	"github.com/benwyrosdick/lazylinear/internal/config"
	"github.com/benwyrosdick/lazylinear/internal/ui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: could not load config: %v", err)
		cfg = &config.Config{}
	}

	client := api.NewClient(cfg.APIKey)

	ui, err := ui.NewUI(client)
	if err != nil {
		log.Fatal(err)
	}

	if err := ui.Run(); err != nil {
		log.Fatal(err)
	}
}
