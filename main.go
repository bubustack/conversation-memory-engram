package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	sdk "github.com/bubustack/bubu-sdk-go"
	conversationmemory "github.com/bubustack/conversation-memory-engram/pkg/engram"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := sdk.StartStreaming(ctx, conversationmemory.New()); err != nil {
		log.Fatalf("conversation-memory engram failed: %v", err)
	}
}
