package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
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

type claimError struct {
	message string
	code    int
}

func (e *claimError) Error() string {
	return e.message
}

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
	case "tasks":
		if err := executeTasks(ctx, baseURL, token, jsonOutput, args, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "show":
		if err := executeShow(ctx, baseURL, token, jsonOutput, args, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "transition":
		if err := executeTransition(ctx, baseURL, token, args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "claim":
		if err := executeClaim(ctx, baseURL, token, args); err != nil {
			var claimErr *claimError
			if errors.As(err, &claimErr) {
				fmt.Fprintf(os.Stderr, "error: %v\n", claimErr.Error())
				os.Exit(claimErr.code)
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "submit":
		if err := executeSubmit(ctx, baseURL, token, args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "heartbeat":
		if err := executeHeartbeat(ctx, baseURL, token, args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "next":
		if err := executeNext(ctx, baseURL, token, jsonOutput, args); err != nil {
			var claimErr *claimError
			if errors.As(err, &claimErr) {
				fmt.Fprintf(os.Stderr, "error: %v\n", claimErr.Error())
				os.Exit(claimErr.code)
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "promote":
		if err := executePromote(ctx, baseURL, token, args); err != nil {
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

func executeTasks(ctx context.Context, baseURL, token string, jsonOutput bool, args []string, out io.Writer) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectFlag := fs.String("project", "", "project ID")
	stateFlag := fs.String("state", "", "filter by state")
	modelFlag := fs.String("model", "", "filter by model")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if *projectFlag == "" {
		return fmt.Errorf("--project flag is required")
	}

	client := tuiclient.NewHTTPClient(baseURL, token)
	tasks, err := client.ListTasks(ctx, *projectFlag)
	if err != nil {
		return fmt.Errorf("failed to list tasks: %w", err)
	}

	// Filter by state and model
	var filtered []tuiclient.Task
	for _, task := range tasks {
		if *stateFlag != "" && task.State != *stateFlag {
			continue
		}
		if *modelFlag != "" && task.Model != *modelFlag {
			continue
		}
		filtered = append(filtered, task)
	}

	// Output results
	if jsonOutput {
		output, err := json.MarshalIndent(filtered, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Fprintln(out, string(output))
	} else {
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATE\tMODEL\tKIND\tTITLE")
		for _, task := range filtered {
			id := task.ID
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, task.State, task.Model, task.Kind, task.Title)
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

func executeTransition(ctx context.Context, baseURL, token string, args []string) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	if len(args) < 1 {
		return fmt.Errorf("missing task id")
	}

	taskID := args[0]
	var toState string
	var note *string
	var i int

	for i = 1; i < len(args); i++ {
		switch args[i] {
		case "--to":
			i++
			if i >= len(args) {
				return fmt.Errorf("--to requires a value")
			}
			toState = args[i]
		case "--note":
			i++
			if i >= len(args) {
				return fmt.Errorf("--note requires a value")
			}
			note = &args[i]
		case "--json":
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	if toState == "" {
		return fmt.Errorf("--to flag is required")
	}

	client := tuiclient.NewHTTPClient(baseURL, token)
	if err := client.TransitionTask(ctx, taskID, toState, note); err != nil {
		return fmt.Errorf("failed to transition task: %w", err)
	}

	return nil
}

func executeClaim(ctx context.Context, baseURL, token string, args []string) error {
	// Validate configuration
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	// Parse flags
	fs := flag.NewFlagSet("claim", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	agentFlag := fs.String("agent", "", "agent ID")
	modelFlag := fs.String("model", "", "model")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Get positional argument (task ID)
	if fs.NArg() < 1 {
		return fmt.Errorf("task ID is required")
	}
	taskID := fs.Arg(0)

	// Resolve identity
	agentID, model, err := resolveAgentIdentity(*agentFlag, *modelFlag)
	if err != nil {
		return err
	}

	// Create client and claim task
	client := tuiclient.NewHTTPClient(baseURL, token)
	if err := client.ClaimTask(ctx, taskID, agentID, model); err != nil {
		if errors.Is(err, tuiclient.ErrAlreadyClaimed) {
			return &claimError{message: "already claimed", code: 3}
		}
		return err
	}

	return nil
}

func executeSubmit(ctx context.Context, baseURL, token string, args []string) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	resultFlag := fs.String("result", "", "result/writeup")
	verdictFlag := fs.String("verdict", "", "verdict (approve or reject)")
	prFlag := fs.String("pr", "", "PR URL")
	branchFlag := fs.String("branch", "", "branch name")
	noOpFlag := fs.Bool("no-op", false, "mark as already-satisfied (no-op)")
	agentFlag := fs.String("agent", "", "agent ID")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("task ID is required")
	}
	taskID := fs.Arg(0)

	if *resultFlag == "" {
		return fmt.Errorf("--result flag is required")
	}

	agentID := *agentFlag
	if agentID == "" {
		agentID = os.Getenv("AGENT_ID")
	}
	if agentID == "" {
		return fmt.Errorf("agent ID is required (set --agent flag or AGENT_ID environment variable)")
	}

	if *noOpFlag && (*prFlag != "" || *branchFlag != "") {
		return fmt.Errorf("--no-op cannot be combined with --pr or --branch")
	}

	var links []tuiclient.LinkInput
	if *noOpFlag {
		links = []tuiclient.LinkInput{
			{Kind: "no_op", Value: "already-satisfied"},
		}
	} else {
		if *prFlag != "" && *branchFlag != "" {
			links = []tuiclient.LinkInput{
				{Kind: "pr", Value: *prFlag},
				{Kind: "branch", Value: *branchFlag},
			}
		} else if *prFlag != "" || *branchFlag != "" {
			return fmt.Errorf("--pr and --branch must be provided together")
		}
	}

	var verdict *string
	if *verdictFlag != "" {
		if *verdictFlag != "approve" && *verdictFlag != "reject" {
			return fmt.Errorf("verdict must be 'approve' or 'reject', got %q", *verdictFlag)
		}
		verdict = verdictFlag
	}

	client := tuiclient.NewHTTPClient(baseURL, token)
	if err := client.SubmitTask(ctx, taskID, agentID, *resultFlag, verdict, links); err != nil {
		return fmt.Errorf("failed to submit task: %w", err)
	}

	return nil
}

func executeHeartbeat(ctx context.Context, baseURL, token string, args []string) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	fs := flag.NewFlagSet("heartbeat", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	agentFlag := fs.String("agent", "", "agent ID")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("task ID is required")
	}
	taskID := fs.Arg(0)

	// Resolve agent ID
	agentID := *agentFlag
	if agentID == "" {
		agentID = os.Getenv("AGENT_ID")
	}
	if agentID == "" {
		return fmt.Errorf("agent ID is required (set --agent flag or AGENT_ID environment variable)")
	}

	client := tuiclient.NewHTTPClient(baseURL, token)
	if err := client.HeartbeatTask(ctx, taskID, agentID); err != nil {
		return fmt.Errorf("failed to heartbeat task: %w", err)
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

func executeNext(ctx context.Context, baseURL, token string, jsonOutput bool, args []string) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	fs := flag.NewFlagSet("next", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectFlag := fs.String("project", "", "project ID")
	modelFlag := fs.String("model", "", "model")
	kindFlag := fs.String("kind", "", "kind (implement or review)")
	claimFlag := fs.Bool("claim", false, "claim the task")
	agentFlag := fs.String("agent", "", "agent ID")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if *projectFlag == "" {
		return fmt.Errorf("--project flag is required")
	}
	if *modelFlag == "" {
		return fmt.Errorf("--model flag is required")
	}
	if *kindFlag == "" {
		return fmt.Errorf("--kind flag is required")
	}

	client := tuiclient.NewHTTPClient(baseURL, token)
	tasks, err := client.ListTasks(ctx, *projectFlag,
		tuiclient.WithModel(*modelFlag),
		tuiclient.WithKind(*kindFlag),
		tuiclient.WithClaimable(true),
	)
	if err != nil {
		return fmt.Errorf("failed to list tasks: %w", err)
	}

	if len(tasks) == 0 {
		return &claimError{message: "nothing claimable", code: 2}
	}

	task := tasks[0]

	if *claimFlag {
		agentID, model, err := resolveAgentIdentity(*agentFlag, *modelFlag)
		if err != nil {
			return err
		}

		if err := client.ClaimTask(ctx, task.ID, agentID, model); err != nil {
			if errors.Is(err, tuiclient.ErrAlreadyClaimed) {
				return &claimError{message: "raced, none claimed", code: 2}
			}
			return err
		}

		if jsonOutput {
			task, err := client.GetTask(ctx, task.ID)
			if err != nil {
				return fmt.Errorf("failed to get task details: %w", err)
			}
			output, err := json.MarshalIndent(task, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(output))
		} else {
			fmt.Println(task.ID)
		}
	} else {
		if jsonOutput {
			output, err := json.MarshalIndent(task, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(output))
		} else {
			fmt.Println(task.ID)
		}
	}

	return nil
}

func executePromote(ctx context.Context, baseURL, token string, args []string) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	if len(args) < 1 {
		return fmt.Errorf("task ID is required")
	}

	taskID := args[0]

	client := tuiclient.NewHTTPClient(baseURL, token)
	if err := client.PromoteTask(ctx, taskID); err != nil {
		return fmt.Errorf("failed to promote task: %w", err)
	}

	return nil
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
