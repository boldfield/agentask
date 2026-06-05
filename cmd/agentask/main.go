package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	fmt.Printf("agentask version %s\n", version)
	os.Exit(0)
}
