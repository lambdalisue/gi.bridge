package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/comail/colog"

	"github.com/lambdalisue/gi.bridge/internal/bridge"
)

var (
	appVersion = "dev"
)

func main() {
	colog.Register()
	var (
		version = flag.Bool("version", false, "show version")
		addr    = flag.String("addr", "127.0.0.1:0", "TCP address to listen")
	)
	flag.Parse()
	if *version {
		fmt.Println(appVersion)
		os.Exit(0)
	}

	exitCode, err := run(*addr)
	if err != nil {
		log.Fatalf("error: %s\n", err)
	}
	os.Exit(exitCode)
}

func run(addr string) (int, error) {
	ctx := context.Background()
	b := bridge.New(os.Stdin, os.Stdout)
	if err := b.Start(ctx, addr); err != nil {
		return 1, err
	}
	return 0, nil
}
