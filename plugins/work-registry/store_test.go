package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return s, dir
}

func TestCreateThenReadBack(t *testing.T) {
	s, _ := newStore(t)
	task, wc, replay, err := s.CreateTask(CreateInput{
		Title: "harden add", Goal: "reject illegal input", Repo: "fixtures/sample-java-project",
		Acceptance:     []AcceptanceCriterion{{ID: "AC1", Text: "mvn test PASS"}},
		IdempotencyKey: "",
	})
	if err != nil || replay {
		t.Fatalf("create: err=%v replay=%v", err, replay)
	}
	if task.Status != StatusPlanned || task.Version != 1 || task.WorkContextID != wc.ID {
		t.Fatalf("task: %+v", task)
	}
	if wc.TaskID != task.ID || wc.Repo != "fixtures/sample-java-project" || wc.Version != 1 {
		t.Fatalf("wc: %+v", wc)
	}
	got, gwc, ok := s.GetByTask(task.ID)
	if !ok || got.ID != task.ID || gwc.ID != wc.ID {
		t.Fatalf("get: %+v %+v ok=%v", got, gwc, ok)
	}
}

func TestIdempotentCreate(t *testing.T) {
	s, _ := newStore(t)
	in := CreateInput{Title: "x", Goal: "y", Repo: "r", IdempotencyKey: "k1"}
	t1, _, _, _ := s.CreateTask(in)
	t2, _, replay, _ := s.CreateTask(in)
	if !replay || t2.ID != t1.ID {
		t.Fatalf("idempotent create should return the same task: replay=%v %s vs %s", replay, t1.ID, t2.ID)
	}
}

func TestProjectionRebuildsFromLog(t *testing.T) {
	s, dir := newStore(t)
	task, wc, _, _ := s.CreateTask(CreateInput{Title: "x", Goal: "y", Repo: "r"})
	if _, _, err := s.Transition(wc.ID, StatusInProgress, task.Version); err != nil {
		t.Fatalf("transition: %v", err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, _, ok := reloaded.GetByTask(task.ID)
	if !ok || got.Status != StatusInProgress || got.Version != 2 {
		t.Fatalf("projection not rebuilt: %+v ok=%v", got, ok)
	}
	if got2, _, err := reloaded.Transition(wc.ID, StatusInReview, got.Version); err != nil || got2.Version != 3 {
		t.Fatalf("post-reload transition: task=%+v err=%v", got2, err)
	}

	f, err := os.Open(filepath.Join(dir, "work-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	var seqs []int64
	for scan.Scan() {
		var rec struct {
			Seq int64 `json:"seq"`
		}
		if err := json.Unmarshal(scan.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, rec.Seq)
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 {
		t.Fatalf("log seq must remain monotonic across reload: %v", seqs)
	}
}
