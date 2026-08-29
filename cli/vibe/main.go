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

type workspaceView struct {
	ID            string `json:"id"`
	WorkContextID string `json:"work_context_id"`
	Repo          string `json:"repo"`
	Path          string `json:"path"`
	Branch        string `json:"branch"`
	BaseCommit    string `json:"base_commit"`
	Status        string `json:"status"`
	ReleasePolicy string `json:"release_policy"`
}

type workspaceResponse struct {
	Workspace workspaceView `json:"workspace"`
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
	if len(args) < 2 {
		return fmt.Errorf("usage: vibe [global flags] <task|workspace|agent> <subcommand> ...")
	}

	switch args[0] {
	case "task":
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
	case "workspace":
		switch args[1] {
		case "allocate":
			return workspaceAllocate(*socket, *identity, *token, args[2:])
		case "show":
			return workspaceShow(*socket, *identity, *token, args[2:])
		case "release":
			return workspaceRelease(*socket, *identity, *token, args[2:])
		default:
			return fmt.Errorf("unknown workspace subcommand %q", args[1])
		}
	case "agent":
		switch args[1] {
		case "run":
			return agentRun(*socket, *identity, *token, args[2:])
		case "show":
			return agentShow(*socket, *identity, *token, args[2:])
		case "cancel":
			return agentCancel(*socket, *identity, *token, args[2:])
		default:
			return fmt.Errorf("unknown agent subcommand %q", args[1])
		}
	case "artifact":
		switch args[1] {
		case "collect-diff":
			return artifactCollectDiff(*socket, *identity, *token, args[2:])
		case "show":
			return artifactShow(*socket, *identity, *token, args[2:])
		default:
			return fmt.Errorf("unknown artifact subcommand %q", args[1])
		}
	case "review":
		switch args[1] {
		case "request":
			return reviewRequest(*socket, *identity, *token, args[2:])
		case "decide":
			return reviewDecide(*socket, *identity, *token, args[2:])
		case "show":
			return reviewShow(*socket, *identity, *token, args[2:])
		default:
			return fmt.Errorf("unknown review subcommand %q", args[1])
		}
	case "session":
		switch args[1] {
		case "seal":
			return sessionSeal(*socket, *identity, *token, args[2:])
		case "show":
			return sessionShow(*socket, *identity, *token, args[2:])
		default:
			return fmt.Errorf("unknown session subcommand %q", args[1])
		}
	case "tool":
		switch args[1] {
		case "run":
			return toolRun(*socket, *identity, *token, args[2:])
		case "show":
			return toolShow(*socket, *identity, *token, args[2:])
		default:
			return fmt.Errorf("unknown tool subcommand %q", args[1])
		}
	default:
		return fmt.Errorf("unknown command %q", args[0])
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

func workspaceAllocate(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("workspace allocate requires <work-context-id>")
	}
	wcID := args[0]
	fs := flag.NewFlagSet("workspace allocate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", "", "repository path")
	baseRef := fs.String("base-ref", "", "base git ref")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *repo == "" {
		return fmt.Errorf("-repo is required")
	}
	payload := map[string]string{"work_context_id": wcID, "repo": *repo}
	if *baseRef != "" {
		payload["base_ref"] = *baseRef
	}
	resp, err := invoke(socket, identity, token, command("workspace.allocate", payload))
	if err != nil {
		return err
	}
	var out workspaceResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("workspace %s  branch %s  path %s  base %s\n", out.Workspace.ID, out.Workspace.Branch, out.Workspace.Path, out.Workspace.BaseCommit)
	return nil
}

func workspaceShow(socket, identity, token string, args []string) error {
	var workspaceID string
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		workspaceID = args[0]
		parseArgs = args[1:]
	}
	fs := flag.NewFlagSet("workspace show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	wcID := fs.String("work-context", "", "work context id")
	jsonOut := fs.Bool("json", false, "print raw response payload")
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	if (workspaceID == "") == (*wcID == "") {
		return fmt.Errorf("exactly one of <workspace-id> or -work-context is required")
	}
	payload := map[string]string{}
	if workspaceID != "" {
		payload["workspace_id"] = workspaceID
	} else {
		payload["work_context_id"] = *wcID
	}
	req := protocol.Envelope{Kind: protocol.KindQuery, Capability: "workspace.get", Major: 1, Payload: protocol.NewPayload(payload)}
	resp, err := invoke(socket, identity, token, req)
	if err != nil {
		return err
	}
	if *jsonOut {
		var pretty any
		if err := json.Unmarshal(resp.Payload, &pretty); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	var out workspaceResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("id %s\n", out.Workspace.ID)
	fmt.Printf("status %s\n", out.Workspace.Status)
	fmt.Printf("branch %s\n", out.Workspace.Branch)
	fmt.Printf("path %s\n", out.Workspace.Path)
	fmt.Printf("base_commit %s\n", out.Workspace.BaseCommit)
	return nil
}

func workspaceRelease(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("workspace release requires <workspace-id>")
	}
	workspaceID := args[0]
	fs := flag.NewFlagSet("workspace release", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	policy := fs.String("policy", "", "release policy preserve|delete")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *policy != "preserve" && *policy != "delete" {
		return fmt.Errorf("-policy must be preserve or delete")
	}
	resp, err := invoke(socket, identity, token, command("workspace.release", map[string]string{"workspace_id": workspaceID, "policy": *policy}))
	if err != nil {
		return err
	}
	var out workspaceResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("status %s  policy %s\n", out.Workspace.Status, out.Workspace.ReleasePolicy)
	return nil
}

func command(capability string, payload any) protocol.Envelope {
	return protocol.Envelope{
		Kind: protocol.KindCommand, Capability: capability, Major: 1,
		Deadline: time.Now().Add(30 * time.Second).Format(time.RFC3339Nano),
		Payload:  protocol.NewPayload(payload),
	}
}

type agentRunView struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Provider      string `json:"provider"`
	FrameCount    int    `json:"frame_count"`
	RawSessionRef string `json:"raw_session_ref"`
}

type agentRunResponse struct {
	AgentRun agentRunView `json:"agent_run"`
	StreamID string       `json:"stream_id"`
}

type agentFrameView struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Index int    `json:"index"`
}

func agentRun(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("agent run requires <work-context-id>")
	}
	wcID := args[0]
	fs := flag.NewFlagSet("agent run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workspace := fs.String("workspace", "", "workspace path")
	prompt := fs.String("prompt", "", "agent prompt")
	steps := fs.Int("steps", 3, "mock steps")
	failAt := fs.Int("fail-at", 0, "mock failure step")
	writeFile := fs.String("write-file", "", "relative workspace file to append")
	writeContent := fs.String("write-content", "", "content to append")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *workspace == "" || *prompt == "" {
		return fmt.Errorf("-workspace and -prompt are required")
	}
	payload := map[string]any{
		"work_context_id": wcID,
		"workspace_path":  *workspace,
		"prompt":          *prompt,
		"provider":        "mock",
		"mock_steps":      *steps,
		"mock_fail_at":    *failAt,
	}
	if *writeFile != "" {
		payload["mock_write_file"] = *writeFile
	}
	if *writeContent != "" {
		payload["mock_write_content"] = *writeContent
	}
	req := command("agent.run", payload)
	accepted, err := invokeStream(socket, identity, token, req, func(f protocol.Envelope) {
		if f.Kind != protocol.KindStreamData {
			return
		}
		var sf protocol.StreamFrame
		if json.Unmarshal(f.Payload, &sf) != nil {
			return
		}
		var af agentFrameView
		if json.Unmarshal(sf.Data, &af) == nil {
			fmt.Printf("» %s\n", af.Text)
		}
	})
	if err != nil {
		return err
	}
	var out agentRunResponse
	if err := json.Unmarshal(accepted.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("agent_run %s  stream %s\n", out.AgentRun.ID, out.StreamID)
	for i := 0; i < 50; i++ {
		run, err := fetchAgentRun(socket, identity, token, out.AgentRun.ID)
		if err != nil {
			return err
		}
		if run.Status != StatusRunningCLI {
			fmt.Printf("status %s  raw_session_ref %s\n", run.Status, run.RawSessionRef)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("agent run %s did not reach a terminal status", out.AgentRun.ID)
}

const StatusRunningCLI = "RUNNING"

func fetchAgentRun(socket, identity, token, runID string) (agentRunView, error) {
	req := protocol.Envelope{Kind: protocol.KindQuery, Capability: "agent.run.get", Major: 1, Payload: protocol.NewPayload(map[string]string{"agent_run_id": runID})}
	resp, err := invoke(socket, identity, token, req)
	if err != nil {
		return agentRunView{}, err
	}
	var out agentRunResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return agentRunView{}, err
	}
	return out.AgentRun, nil
}

func agentShow(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("agent show requires <agent-run-id>")
	}
	runID := args[0]
	fs := flag.NewFlagSet("agent show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print raw response payload")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	req := protocol.Envelope{Kind: protocol.KindQuery, Capability: "agent.run.get", Major: 1, Payload: protocol.NewPayload(map[string]string{"agent_run_id": runID})}
	resp, err := invoke(socket, identity, token, req)
	if err != nil {
		return err
	}
	if *jsonOut {
		fmt.Println(string(resp.Payload))
		return nil
	}
	var out agentRunResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("id %s\n", out.AgentRun.ID)
	fmt.Printf("status %s\n", out.AgentRun.Status)
	fmt.Printf("provider %s\n", out.AgentRun.Provider)
	fmt.Printf("frame_count %d\n", out.AgentRun.FrameCount)
	fmt.Printf("raw_session_ref %s\n", out.AgentRun.RawSessionRef)
	return nil
}

func agentCancel(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("agent cancel requires <agent-run-id>")
	}
	resp, err := invoke(socket, identity, token, command("agent.run.cancel", map[string]string{"agent_run_id": args[0]}))
	if err != nil {
		return err
	}
	var out agentRunResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("status %s\n", out.AgentRun.Status)
	return nil
}

// --- M1.4: artifact + tool ---

type artifactView struct {
	ID            string `json:"id"`
	WorkContextID string `json:"work_context_id"`
	Kind          string `json:"kind"`
	BlobURI       string `json:"blob_uri"`
	Summary       struct {
		FilesChanged int      `json:"files_changed"`
		Insertions   int      `json:"insertions"`
		Deletions    int      `json:"deletions"`
		Files        []string `json:"files"`
	} `json:"summary"`
}
type artifactResponse struct {
	Artifact artifactView `json:"artifact"`
}

func artifactCollectDiff(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("artifact collect-diff requires <work-context-id>")
	}
	wcID := args[0]
	fs := flag.NewFlagSet("artifact collect-diff", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workspace := fs.String("workspace", "", "workspace path")
	baseRef := fs.String("base-ref", "", "base ref (default HEAD)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *workspace == "" {
		return fmt.Errorf("-workspace is required")
	}
	payload := map[string]string{"work_context_id": wcID, "workspace_path": *workspace}
	if *baseRef != "" {
		payload["base_ref"] = *baseRef
	}
	resp, err := invoke(socket, identity, token, command("artifact.collect_diff", payload))
	if err != nil {
		return err
	}
	var out artifactResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	a := out.Artifact
	fmt.Printf("artifact %s  files_changed %d  +%d -%d  blob %s\n",
		a.ID, a.Summary.FilesChanged, a.Summary.Insertions, a.Summary.Deletions, a.BlobURI)
	return nil
}

func artifactShow(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("artifact show requires <artifact-id>")
	}
	fs := flag.NewFlagSet("artifact show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print raw response payload")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	req := protocol.Envelope{Kind: protocol.KindQuery, Capability: "artifact.get", Major: 1, Payload: protocol.NewPayload(map[string]string{"artifact_id": args[0]})}
	resp, err := invoke(socket, identity, token, req)
	if err != nil {
		return err
	}
	if *jsonOut {
		fmt.Println(string(resp.Payload))
		return nil
	}
	var out artifactResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	a := out.Artifact
	fmt.Printf("id %s\nkind %s\nblob_uri %s\nfiles_changed %d\n", a.ID, a.Kind, a.BlobURI, a.Summary.FilesChanged)
	return nil
}

type toolRunView struct {
	ID            string   `json:"id"`
	WorkContextID string   `json:"work_context_id"`
	Label         string   `json:"label"`
	Command       []string `json:"command"`
	ExitCode      int      `json:"exit_code"`
	Outcome       string   `json:"outcome"`
	StdoutURI     string   `json:"stdout_uri"`
	StderrURI     string   `json:"stderr_uri"`
	Fingerprint   string   `json:"fingerprint"`
}
type toolRunResponse struct {
	ToolRun toolRunView `json:"tool_run"`
}

func toolRun(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("tool run requires <work-context-id>")
	}
	wcID := args[0]
	// split flags (before "--") from the command argv (after "--")
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep == len(args)-1 {
		return fmt.Errorf("tool run requires `-- <command> [args...]`")
	}
	flagArgs, cmdArgv := args[1:sep], args[sep+1:]
	fs := flag.NewFlagSet("tool run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workspace := fs.String("workspace", "", "workspace path")
	label := fs.String("label", "", "evidence label, e.g. build|test")
	timeoutMS := fs.Int("timeout-ms", 0, "timeout in ms (0 = default)")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *workspace == "" || *label == "" {
		return fmt.Errorf("-workspace and -label are required")
	}
	payload := map[string]any{
		"work_context_id": wcID, "workspace_path": *workspace, "label": *label, "command": cmdArgv,
	}
	if *timeoutMS > 0 {
		payload["timeout_ms"] = *timeoutMS
	}
	resp, err := invoke(socket, identity, token, command("tool.run", payload))
	if err != nil {
		return err
	}
	var out toolRunResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	tr := out.ToolRun
	fp := tr.Fingerprint
	if len(fp) > 12 {
		fp = fp[:12]
	}
	fmt.Printf("tool_run %s  outcome %s  exit %d  fp %s  stdout %s\n", tr.ID, tr.Outcome, tr.ExitCode, fp, tr.StdoutURI)
	return nil
}

func toolShow(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("tool show requires <tool-run-id>")
	}
	fs := flag.NewFlagSet("tool show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print raw response payload")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	req := protocol.Envelope{Kind: protocol.KindQuery, Capability: "tool.run.get", Major: 1, Payload: protocol.NewPayload(map[string]string{"tool_run_id": args[0]})}
	resp, err := invoke(socket, identity, token, req)
	if err != nil {
		return err
	}
	if *jsonOut {
		fmt.Println(string(resp.Payload))
		return nil
	}
	var out toolRunResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	tr := out.ToolRun
	fmt.Printf("id %s\nlabel %s\noutcome %s\nexit_code %d\nfingerprint %s\nstdout_uri %s\nstderr_uri %s\n",
		tr.ID, tr.Label, tr.Outcome, tr.ExitCode, tr.Fingerprint, tr.StdoutURI, tr.StderrURI)
	return nil
}

// --- M1.5: review ---
type reviewView struct {
	ID                string `json:"id"`
	WorkContextID     string `json:"work_context_id"`
	AgentRunID        string `json:"agent_run_id"`
	DiffArtifactID    string `json:"diff_artifact_id"`
	Status            string `json:"status"`
	Reviewer          string `json:"reviewer"`
	Notes             string `json:"notes"`
	AcceptanceResults []struct {
		CriterionID  string   `json:"criterion_id"`
		Satisfied    bool     `json:"satisfied"`
		EvidenceRefs []string `json:"evidence_refs"`
		Notes        string   `json:"notes"`
	} `json:"acceptance_results"`
	EvidenceSnapshot []struct {
		Kind          string `json:"kind"`
		Outcome       string `json:"outcome"`
		EvidenceRefID string `json:"evidence_ref_id"`
	} `json:"evidence_snapshot"`
	RequestedAt string `json:"requested_at"`
	DecidedAt   string `json:"decided_at"`
}
type reviewResponse struct {
	Review reviewView `json:"review"`
}

func reviewRequest(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("review request requires <work-context-id>")
	}
	wc := args[0]
	fs := flag.NewFlagSet("review request", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agent := fs.String("agent-run", "", "agent run id")
	diff := fs.String("diff-artifact", "", "diff artifact id")
	var evidence acFlags
	fs.Var(&evidence, "evidence", "kind:outcome (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *diff == "" {
		return fmt.Errorf("-diff-artifact is required")
	}
	es := make([]map[string]string, 0, len(evidence))
	for _, v := range evidence {
		k, o, ok := strings.Cut(v, ":")
		if !ok || k == "" || o == "" {
			return fmt.Errorf("-evidence must be kind:outcome")
		}
		es = append(es, map[string]string{"kind": k, "outcome": o})
	}
	payload := map[string]any{"work_context_id": wc, "diff_artifact_id": *diff, "evidence_snapshot": es}
	if *agent != "" {
		payload["agent_run_id"] = *agent
	}
	resp, err := invoke(socket, identity, token, command("review.request", payload))
	if err != nil {
		return err
	}
	var out reviewResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("review %s  status %s\n", out.Review.ID, out.Review.Status)
	return nil
}
func reviewDecide(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("review decide requires <review-id>")
	}
	id := args[0]
	fs := flag.NewFlagSet("review decide", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	approved := fs.Bool("approved", false, "approve")
	changes := fs.Bool("changes-requested", false, "request changes")
	reviewer := fs.String("reviewer", "", "reviewer")
	notes := fs.String("notes", "", "notes")
	var acc acFlags
	fs.Var(&acc, "acceptance", "ID=pass|fail (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *approved == *changes {
		return fmt.Errorf("exactly one of -approved or -changes-requested is required")
	}
	decision := "CHANGES_REQUESTED"
	if *approved {
		decision = "APPROVED"
	}
	results := make([]map[string]any, 0, len(acc))
	for _, v := range acc {
		cid, val, ok := strings.Cut(v, "=")
		if !ok || cid == "" || (val != "pass" && val != "fail") {
			return fmt.Errorf("-acceptance must be ID=pass|fail")
		}
		results = append(results, map[string]any{"criterion_id": cid, "satisfied": val == "pass"})
	}
	payload := map[string]any{"review_id": id, "decision": decision, "reviewer": *reviewer, "notes": *notes, "acceptance_results": results}
	resp, err := invoke(socket, identity, token, command("review.decide", payload))
	if err != nil {
		return err
	}
	var out reviewResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	fmt.Printf("review %s  status %s\n", out.Review.ID, out.Review.Status)
	return nil
}
func reviewShow(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("review show requires <review-id>")
	}
	fs := flag.NewFlagSet("review show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print raw response payload")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	req := protocol.Envelope{Kind: protocol.KindQuery, Capability: "review.get", Major: 1, Payload: protocol.NewPayload(map[string]string{"review_id": args[0]})}
	resp, err := invoke(socket, identity, token, req)
	if err != nil {
		return err
	}
	if *jsonOut {
		fmt.Println(string(resp.Payload))
		return nil
	}
	var out reviewResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	r := out.Review
	fmt.Printf("id %s\nstatus %s\ndiff_artifact_id %s\nreviewer %s\nnotes %s\n", r.ID, r.Status, r.DiffArtifactID, r.Reviewer, r.Notes)
	return nil
}

type recoveryCheckpointView struct {
	HeadCommit string `json:"head_commit"`
	BaseCommit string `json:"base_commit"`
	Branch     string `json:"branch"`
	Dirty      bool   `json:"dirty"`
}

type sessionRecordView struct {
	ID             string `json:"id"`
	WorkContextID  string `json:"work_context_id"`
	AgentRunID     string `json:"agent_run_id"`
	ArchiveRef     string `json:"archive_ref"`
	ArchiveHash    string `json:"archive_hash"`
	SealedAt       string `json:"sealed_at"`
	EventSelection struct {
		CorrelationID string `json:"correlation_id"`
		EventCount    int    `json:"event_count"`
	} `json:"event_selection"`
	RecoveryCheckpoint recoveryCheckpointView `json:"recovery_checkpoint"`
}

type sessionResponse struct {
	SessionRecord sessionRecordView `json:"session_record"`
}

func first12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func sessionSeal(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("session seal requires <work-context-id>")
	}
	fs := flag.NewFlagSet("session seal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agentRunID := fs.String("agent-run", "", "agent run id")
	workspace := fs.String("workspace", "", "workspace path")
	correlation := fs.String("correlation", "", "correlation id (defaults to work context id)")
	diffArtifact := fs.String("diff-artifact", "", "diff artifact id")
	taskID := fs.String("task", "", "task id")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *agentRunID == "" || *workspace == "" {
		return fmt.Errorf("session seal requires -agent-run and -workspace")
	}
	corr := *correlation
	if corr == "" {
		corr = args[0]
	}
	payload := map[string]any{
		"work_context_id": args[0],
		"agent_run_id":    *agentRunID,
		"workspace_path":  *workspace,
		"correlation_id":  corr,
	}
	if *diffArtifact != "" {
		payload["diff_artifact_id"] = *diffArtifact
	}
	if *taskID != "" {
		payload["task_id"] = *taskID
	}
	resp, err := invoke(socket, identity, token, command("session.seal", payload))
	if err != nil {
		return err
	}
	var out sessionResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	r := out.SessionRecord
	fmt.Printf("session %s  events %d  archive %s  hash %s  head %s\n",
		r.ID, r.EventSelection.EventCount, r.ArchiveRef,
		first12(r.ArchiveHash), first12(r.RecoveryCheckpoint.HeadCommit))
	return nil
}

func sessionShow(socket, identity, token string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("session show requires <session-id>")
	}
	fs := flag.NewFlagSet("session show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print raw response payload")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	req := protocol.Envelope{Kind: protocol.KindQuery, Capability: "session.get", Major: 1,
		Payload: protocol.NewPayload(map[string]string{"session_id": args[0]})}
	resp, err := invoke(socket, identity, token, req)
	if err != nil {
		return err
	}
	if *jsonOut {
		fmt.Println(string(resp.Payload))
		return nil
	}
	var out sessionResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	r := out.SessionRecord
	fmt.Printf("id %s\nwork_context_id %s\nagent_run_id %s\narchive_ref %s\narchive_hash %s\nevents %d\nsealed_at %s\nhead_commit %s\ndirty %v\n",
		r.ID, r.WorkContextID, r.AgentRunID, r.ArchiveRef, r.ArchiveHash,
		r.EventSelection.EventCount, r.SealedAt,
		r.RecoveryCheckpoint.HeadCommit, r.RecoveryCheckpoint.Dirty)
	return nil
}
