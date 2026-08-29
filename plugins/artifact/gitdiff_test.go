package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepoWithChange(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	_ = os.WriteFile(filepath.Join(dir, "Calc.java"), []byte("class Calc { int add(int a,int b){return a+b;} }\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	_ = os.WriteFile(filepath.Join(dir, "Calc.java"), []byte("class Calc { int add(int a,int b){ if(a<0) throw new RuntimeException(); return a+b;} }\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "CalcTest.java"), []byte("// new test\n"), 0o644)
	return dir
}

func TestCollectDiffCapturesTrackedAndUntracked(t *testing.T) {
	ws := gitRepoWithChange(t)
	patch, sum, err := collectDiff(ws, "")
	if err != nil {
		t.Fatalf("collectDiff: %v", err)
	}
	if !strings.Contains(patch, "Calc.java") || !strings.Contains(patch, "RuntimeException") {
		t.Fatalf("patch missing the tracked change:\n%s", patch)
	}
	if sum.FilesChanged < 1 || sum.Insertions < 1 {
		t.Fatalf("summary: %+v", sum)
	}
	joined := strings.Join(sum.Files, ",")
	if !strings.Contains(joined, "Calc.java") || !strings.Contains(joined, "CalcTest.java") {
		t.Fatalf("files list missing an entry: %v", sum.Files)
	}
}

func TestCollectDiffCleanTree(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "e"}} {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	_, sum, err := collectDiff(dir, "")
	if err != nil || sum.FilesChanged != 0 || len(sum.Files) != 0 {
		t.Fatalf("clean tree summary: %+v err=%v", sum, err)
	}
}
