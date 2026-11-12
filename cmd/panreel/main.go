package main

import (
	"github.com/cuikaipeng/panreel/internal/app"
	"log"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
