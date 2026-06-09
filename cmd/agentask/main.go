package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/boldfield/agentask/internal/api"
	"github.com/boldfield/agentask/internal/store"
	"github.com/boldfield/agentask/internal/tuiclient"
)

const version = "0.5.0"

func main() {
	// Determine if this is a server or client command
	if len(os.Args) > 1 && os.Args[1] == "server" {
		runServer()
	} else if len(os.Args) > 1 && os.Args[1] != "server" {
		runClient(os.Args[1], os.Args[2:])
	} else {
		// Default to server for backward compatibility
		runServer()
	}
}

func runServer() {
	// Print version
	fmt.Printf("agentask version %s\n", version)

	// Read configuration from environment
	authToken := os.Getenv("AGENTASK_TOKEN")
	if authToken == "" {
		log.Fatal("AGENTASK_TOKEN environment variable not set")
	}

	dbPath := os.Getenv("AGENTASK_DB")
	if dbPath == "" {
		dbPath = "agentask.db"
	}

	addr := os.Getenv("AGENTASK_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	leaseTTLStr := os.Getenv("AGENTASK_LEASE_TTL")
	if leaseTTLStr == "" {
		leaseTTLStr = "5m"
	}
	leaseTTL, err := time.ParseDuration(leaseTTLStr)
	if err != nil {
		log.Fatalf("failed to parse AGENTASK_LEASE_TTL: %v", err)
	}

	maxReviewRoundsStr := os.Getenv("AGENTASK_MAX_REVIEW_ROUNDS")
	if maxReviewRoundsStr == "" {
		maxReviewRoundsStr = "5"
	}
	maxReviewRounds, err := strconv.Atoi(maxReviewRoundsStr)
	if err != nil {
		log.Fatalf("failed to parse AGENTASK_MAX_REVIEW_ROUNDS: %v", err)
	}

	// Parse model allowlist
	allowedModels := parseAllowedModels(os.Getenv("AGENTASK_MODELS"))
	if len(allowedModels) == 0 {
		log.Fatal("AGENTASK_MODELS configuration resulted in empty allowlist")
	}

	// Open the store
	s, err := store.Open(dbPath, allowedModels)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Create API server
	apiServer := api.New(s, authToken, leaseTTL, maxReviewRounds)

	// Start HTTP server
	log.Printf("listening on %s", addr)
	if err := apiServer.ListenAndServe(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func runClient(verb string, args []string) {
	// Parse --json flag
	var jsonOutput bool
	var filteredArgs []string
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	// Read configuration from environment
	baseURL := os.Getenv("AGENTASK_URL")
	if baseURL == "" {
		fmt.Fprintf(os.Stderr, "error: AGENTASK_URL environment variable not set\n")
		os.Exit(1)
	}

	token := os.Getenv("AGENTASK_TOKEN")
	if token == "" {
		fmt.Fprintf(os.Stderr, "error: AGENTASK_TOKEN environment variable not set\n")
		os.Exit(1)
	}

	// Create client
	client := tuiclient.NewHTTPClient(baseURL, token)
	ctx := context.Background()

	// Dispatch to verb handler
	switch verb {
	case "projects":
		handleProjects(ctx, client, jsonOutput)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command '%s'\n", verb)
		os.Exit(1)
	}
}

func handleProjects(ctx context.Context, client *tuiclient.HTTPClient, jsonOutput bool) {
	projects, err := client.ListProjects(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		output, err := json.MarshalIndent(projects, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to marshal JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(output))
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tREPO\tCREATED AT")
		for _, p := range projects {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Repo, p.CreatedAt)
		}
		w.Flush()
	}
}

func parseAllowedModels(modelsStr string) []string {
	const defaultModels = "haiku,sonnet,opus"
	if modelsStr == "" {
		modelsStr = defaultModels
	}

	seen := make(map[string]bool)
	var result []string
	for _, model := range strings.Split(modelsStr, ",") {
		model = strings.TrimSpace(model)
		if model != "" && !seen[model] {
			seen[model] = true
			result = append(result, model)
		}
	}
	return result
}
