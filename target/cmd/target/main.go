package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	target "masque-target"
)

func main() {
	var bind, prefix string
	var bufferSize int
	var quiet bool

	flag.StringVar(&bind, "b", "0.0.0.0:7400", "bind UDP listen address")
	flag.StringVar(&prefix, "prefix", "reply: ", "reply prefix")
	flag.IntVar(&bufferSize, "buf", 2048, "UDP read buffer size")
	flag.BoolVar(&quiet, "quiet", false, "disable request logs")
	flag.Parse()

	var logger *log.Logger
	if !quiet {
		logger = target.NewLogger(os.Stdout)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := target.NewServer(target.Config{
		ListenAddr:     bind,
		ReplyPrefix:    prefix,
		ReadBufferSize: bufferSize,
		Logger:         logger,
	})
	if err := server.Serve(ctx); err != nil {
		log.Fatalf("target server failed: %v", err)
	}
}
