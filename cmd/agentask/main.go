package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	isClient, verb := parseCommand(os.Args)
	if isClient {
		runClient(verb, os.Args[2:])
	} else {
		runServer()
	}
}

func parseCommand(args []string) (isClient bool, verb string) {
	if len(args) > 1 && args[1] != "server" {
		return true, args[1]
	}
	return false, ""
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
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			break
		}
	}

	// Read configuration from environment
	baseURL := os.Getenv("AGENTASK_URL")
	token := os.Getenv("AGENTASK_TOKEN")
	ctx := context.Background()

	// Dispatch to verb handler
	switch verb {
	case "projects":
		if err := executeProjects(ctx, baseURL, token, jsonOutput, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "show":
		if err := executeShow(ctx, baseURL, token, jsonOutput, args, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command '%s'\n", verb)
		os.Exit(1)
	}
}

func executeProjects(ctx context.Context, baseURL, token string, jsonOutput bool, out io.Writer) error {
	// Validate configuration
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	// Create client and list projects
	client := tuiclient.NewHTTPClient(baseURL, token)
	projects, err := client.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to list projects: %w", err)
	}

	// Output results
	if jsonOutput {
		output, err := json.MarshalIndent(projects, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Fprintln(out, string(output))
	} else {
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tREPO\tCREATED AT")
		for _, p := range projects {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Repo, p.CreatedAt)
		}
		w.Flush()
	}

	return nil
}

func executeShow(ctx context.Context, baseURL, token string, jsonOutput bool, args []string, out io.Writer) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	taskID := ""
	for _, arg := range args {
		if arg != "--json" {
			taskID = arg
			break
		}
	}

	if taskID == "" {
		return fmt.Errorf("task id required")
	}

	client := tuiclient.NewHTTPClient(baseURL, token)
	task, err := client.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	if jsonOutput {
		output, err := json.MarshalIndent(task, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Fprintln(out, string(output))
	} else {
		fmt.Fprintf(out, "ID: %s\n", task.ID)
		fmt.Fprintf(out, "State: %s\n", task.State)
		fmt.Fprintf(out, "Model: %s\n", task.Model)
		fmt.Fprintf(out, "Kind: %s\n", task.Kind)
		fmt.Fprintf(out, "Title: %s\n", task.Title)
		fmt.Fprintf(out, "Spec: %s\n", task.Spec)
		if task.TargetTaskID != nil {
			fmt.Fprintf(out, "Target Task ID: %s\n", *task.TargetTaskID)
		}
		if len(task.Links) > 0 {
			fmt.Fprintf(out, "Links:\n")
			for _, link := range task.Links {
				fmt.Fprintf(out, "  - %s: %s\n", link.Kind, link.Value)
			}
		}
	}

	return nil
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

func resolveAgentIdentity(agentFlag, modelFlag string) (agentID, model string, err error) {
	// Resolve agent ID: prefer flag, fallback to AGENT_ID env
	if agentFlag != "" {
		agentID = agentFlag
	} else {
		agentID = os.Getenv("AGENT_ID")
	}

	// Resolve model: prefer flag, fallback to AGENT_MODEL env
	if modelFlag != "" {
		model = modelFlag
	} else {
		model = os.Getenv("AGENT_MODEL")
	}

	// Validate required fields
	if agentID == "" {
		return "", "", fmt.Errorf("agent ID is required (set --agent flag or AGENT_ID environment variable)")
	}
	if model == "" {
		return "", "", fmt.Errorf("model is required (set --model flag or AGENT_MODEL environment variable)")
	}

	return agentID, model, nil
}
