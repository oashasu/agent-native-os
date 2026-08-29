package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type acFlags []string

func (a *acFlags) String() string { return strings.Join(*a, ",") }
func (a *acFlags) Set(v string) error {
	*a = append(*a, v)
	return nil
}

type taskView struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Status             string `json:"status"`
	Version            int    `json:"version"`
	WorkContextID      string `json:"work_context_id"`
	AcceptanceCriteria []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"acceptance_criteria"`
}

type workContextView struct {
	ID           string `json:"id"`
	Repo         string `json:"repo"`
	Version      int    `json:"version"`
	EvidenceRefs []any  `json:"evidence_refs"`
}

type workResponse struct {
	Task        taskView        `json:"task"`
	WorkContext workContextView `json:"work_context"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	global := flag.NewFlagSet("vibe", flag.ContinueOnError)
	global.SetOutput(os.Stderr)
	socket := global.String("socket", "/tmp/agent-native-os-m1.sock", "kernel socket")
	identity := global.String("identity", "local-cli", "client identity")
	token := global.String("token", os.Getenv("VIBE_CLIENT_TOKEN"), "client authentication token")
	if err := global.Parse(args); err != nil {
		return err
	}
	args = global.Args()
	if *token == "" {
		return fmt.Errorf("-token or VIBE_CLIENT_TOKEN required")
	}
	if len(args) < 2 || args[0] != "task" {
		return fmt.Errorf("usage: vibe [global flags] task <create|show|transition> ...")
	}

	switch args[1] {
	case "create":
		return taskCreate(*socket, *identity, *token, args[2:])
	case "show":
		return taskShow(*socket, *identity, *token, args[2:])
	case "transition":
		return taskTransition(*socket, *identity, *token, args[2:])
	default:
		return fmt.Errorf("unknown task subcommand %q", args[1])
	}
}

func taskCreate(socket, identity, token string, args []string) error {
	fs := flag.NewFlagSet("task create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	title := fs.String("title", "", "task title")
	goal := fs.String("goal", "", "task goal")
	scope := fs.String("scope", "", "task scope")
	repo := fs.String("repo", "", "repository path")
	var acs acFlags
	fs.Var(&acs, "ac", "acceptance criterion ID=TEXT (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *title == "" || *goal == "" || *repo == "" {
		return fmt.Errorf("-title, -goal and -repo are required")
	}
	criteria := make([]map[string]string, 0, len(acs))
	for _, raw := range acs {
		id, text, ok := strings.Cut(raw, "=")
		if !ok || id == "" || text == "" {
			return fmt.Errorf("-ac must be ID=TEXT")
		}
		criteria = append(criteria, map[string]string{"id": id, "text": text})
	}
	payload := map[string]any{
		"title": *title, "goal": *goal, "scope": *scope, "repo": *repo,
		"acceptance_criteria": criteria,
	}
	resp, err := invoke(socket, identity, token, command("work.create", payload))
	if err != nil {
		return err
	}
	var out workResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("task %s  wc %s  status %s  version %d\n", out.Task.ID, out.WorkContext.ID, out.Task.Status, out.Task.Version)
	return nil
}

func taskShow(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("task show requires <task-id>")
	}
	taskID := args[0]
	jsonOut := false
	for _, arg := range args[1:] {
		if arg == "-json" {
			jsonOut = true
		} else {
			return fmt.Errorf("unknown task show argument %q", arg)
		}
	}
	req := protocol.Envelope{Kind: protocol.KindQuery, Capability: "work.get", Major: 1, Payload: protocol.NewPayload(map[string]string{"task_id": taskID})}
	resp, err := invoke(socket, identity, token, req)
	if err != nil {
		return err
	}
	if jsonOut {
		var pretty any
		if err := json.Unmarshal(resp.Payload, &pretty); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	var out workResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("id %s\n", out.Task.ID)
	fmt.Printf("title %s\n", out.Task.Title)
	fmt.Printf("status %s\n", out.Task.Status)
	fmt.Printf("version %d\n", out.Task.Version)
	fmt.Printf("repo %s\n", out.WorkContext.Repo)
	fmt.Printf("acceptance: %d criteria\n", len(out.Task.AcceptanceCriteria))
	fmt.Printf("evidence: %d refs\n", len(out.WorkContext.EvidenceRefs))
	return nil
}

func taskTransition(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("task transition requires <work-context-id>")
	}
	wcID := args[0]
	fs := flag.NewFlagSet("task transition", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	to := fs.String("to", "", "target status")
	expected := fs.Int("expected-version", -1, "expected task version")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *to == "" || *expected < 0 {
		return fmt.Errorf("-to and -expected-version are required")
	}
	payload := map[string]any{"work_context_id": wcID, "to": *to, "expected_version": *expected}
	resp, err := invoke(socket, identity, token, command("work.transition", payload))
	if err != nil {
		return err
	}
	var out workResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("status %s  version %d\n", out.Task.Status, out.Task.Version)
	return nil
}

func command(capability string, payload any) protocol.Envelope {
	return protocol.Envelope{
		Kind: protocol.KindCommand, Capability: capability, Major: 1,
		Deadline: time.Now().Add(30 * time.Second).Format(time.RFC3339Nano),
		Payload:  protocol.NewPayload(payload),
	}
}
