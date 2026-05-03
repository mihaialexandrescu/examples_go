package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ollama/ollama/api"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	test_ollama_connection(ctx)

}

func test_ollama_connection(ctx context.Context) {
	val, found := os.LookupEnv("OLLAMA_HOST")
	fmt.Println("OLLAMA_HOST", val, found)

	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatalf("failed to instantiate Ollama Client: %s", err)
	}

	vers, err := client.Version(ctx)
	if err != nil {
		log.Printf("get version error: %s\n", err)
	}
	fmt.Printf("Ollama version: %#v\n", vers)

	la, err := client.List(ctx)
	if err != nil {
		log.Printf("list locally available models error: %s\n", err)
	}
	fmt.Printf("Ollama list locally available models: %+v\n", la)

	lr, err := client.ListRunning(ctx)
	if err != nil {
		log.Printf("list running models error: %s\n", err)
	}
	fmt.Printf("Ollama list running models: %+v\n", lr)
}
