package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// importLayout is the fixture for the import confinement: a repository (root)
// with a nested workDir, an outer ancestor above it, a HOME beside it all, and
// an "outside" directory no workspace file may reach. workDir's CLAUDE.md is
// left for the tests to write, so they can make it a symlink.
type importLayout struct {
	tmp, home, outside, outer, root, workDir string
}

func newImportLayout(t *testing.T) importLayout {
	t.Helper()
	tmp := t.TempDir()
	l := importLayout{tmp: tmp, home: filepath.Join(tmp, "home")}
	l.outside = filepath.Join(tmp, "outside")
	l.outer = filepath.Join(l.home, "lab")
	l.root = filepath.Join(l.outer, "repo")
	l.workDir = filepath.Join(l.root, "sub")
	t.Setenv("HOME", l.home)

	writeFile(t, filepath.Join(l.outside, "env-sub.md"), "OUTSIDE-ENV-SUB")
	writeFile(t, filepath.Join(l.outside, "env-root.md"), "OUTSIDE-ENV-ROOT")
	writeFile(t, filepath.Join(l.outside, "env-esc.md"), "OUTSIDE-ENV-ESCAPE")
	writeFile(t, filepath.Join(l.home, "outside", "secret.md"), "OUTSIDE-SECRET")
	writeFile(t, filepath.Join(l.outside, "wd-claude.md"), "WORKDIR-CLAUDE-THROUGH-A-LINK")
	writeFile(t, filepath.Join(l.outside, "cfg-claude.md"), "WORKDIR-CONFIG-THROUGH-A-LINK")
	writeFile(t, filepath.Join(l.root, "CLAUDE.md"), "ROOT-BODY")
	writeFile(t, filepath.Join(l.workDir, "shared", "in.md"), "INSIDE-IMPORT")
	writeFile(t, filepath.Join(l.home, ".claude", "CLAUDE.md"), "USER-BODY")
	writeFile(t, filepath.Join(l.outer, "CLAUDE.md"), "OUTER-BODY")
	return l
}

// workspaceOpts is the workspace policy with imports on.
func workspaceOpts(l importLayout) MemoryOptions {
	return MemoryOptions{WalkUp: true, SkipUser: true, SkipOuter: true, Imports: true, Root: l.root}
}

// A workspace file may import inside its root; every way out of it — an
// absolute path, a ~/ path, a ../ escape — is skipped, and the skipped files
// are no dependencies: no mtime, no content. The inner ancestor's file is
// confined like workDir's.
func TestMemoryWorkspaceImportsStayInsideTheRoot(t *testing.T) {
	l := newImportLayout(t)

	writeFile(t, filepath.Join(l.workDir, "CLAUDE.md"),
		"SUB-BODY\n\n@./shared/in.md\n\n@"+l.outside+"/env-sub.md\n\n@~/outside/secret.md\n\n@../../outside/env-esc.md")
	// The inner ancestor escapes too.
	writeFile(t, filepath.Join(l.root, "CLAUDE.md"), "ROOT-BODY\n\n@"+l.outside+"/env-root.md")

	got, mtimes := LoadMemory(l.workDir, workspaceOpts(l))

	for _, want := range []string{"SUB-BODY", "INSIDE-IMPORT", "ROOT-BODY"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s missing:\n%s", want, got)
		}
	}
	for _, leaked := range []string{"OUTSIDE-ENV-SUB", "OUTSIDE-ENV-ROOT", "OUTSIDE-ENV-ESCAPE", "OUTSIDE-SECRET"} {
		if strings.Contains(got, leaked) {
			t.Errorf("%s reached the prompt:\n%s", leaked, got)
		}
	}
	for _, skipped := range []string{
		filepath.Join(l.outside, "env-sub.md"),
		filepath.Join(l.outside, "env-root.md"),
		filepath.Join(l.outside, "env-esc.md"),
		filepath.Join(l.home, "outside", "secret.md"),
	} {
		if _, ok := mtimes[skipped]; ok {
			t.Errorf("a skipped import was recorded as a dependency: %s", skipped)
		}
	}
}

// Confinement follows the chain: an inside file imported by a workspace file
// is confined too, so a chain of inside imports is cut at the first hop that
// leaves the root.
func TestMemoryImportChainIsCutAtTheOutsideHop(t *testing.T) {
	l := newImportLayout(t)
	writeFile(t, filepath.Join(l.workDir, "CLAUDE.md"), "SUB-BODY @./shared/in.md")
	writeFile(t, filepath.Join(l.workDir, "shared", "in.md"), "INSIDE-IMPORT @../chain2.md")
	writeFile(t, filepath.Join(l.workDir, "chain2.md"), "CHAIN2 @"+l.outside+"/env-sub.md")

	got, mtimes := LoadMemory(l.workDir, workspaceOpts(l))

	for _, want := range []string{"SUB-BODY", "INSIDE-IMPORT", "CHAIN2"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s missing:\n%s", want, got)
		}
	}
	if strings.Contains(got, "OUTSIDE-ENV-SUB") {
		t.Errorf("the outside hop of the chain reached the prompt:\n%s", got)
	}
	if _, ok := mtimes[filepath.Join(l.outside, "env-sub.md")]; ok {
		t.Errorf("the outside hop was recorded as a dependency")
	}
}

// The user scope and the outer ancestors are the operator's own: their
// imports are not confined, and each loads exactly once — the workspace copy
// of the same import contributes nothing. The operator policy is what leaves
// those two scopes on.
func TestMemoryOperatorImportsStayFree(t *testing.T) {
	l := newImportLayout(t)
	operator := MemoryOptions{WalkUp: true, SkipWorkspace: true, Imports: true, Root: l.root}

	t.Run("user scope", func(t *testing.T) {
		writeFile(t, filepath.Join(l.home, ".claude", "CLAUDE.md"), "USER-BODY @"+l.outside+"/env-sub.md")
		got, _ := LoadMemory(l.workDir, operator)
		if n := strings.Count(got, "OUTSIDE-ENV-SUB"); n != 1 {
			t.Errorf("the operator's import loaded %d times, want exactly the user scope's one:\n%s", n, got)
		}
	})

	t.Run("outer ancestor", func(t *testing.T) {
		writeFile(t, filepath.Join(l.outer, "CLAUDE.md"), "OUTER-BODY @"+l.outside+"/env-root.md")
		got, _ := LoadMemory(l.workDir, operator)
		if n := strings.Count(got, "OUTSIDE-ENV-ROOT"); n != 1 {
			t.Errorf("the outer ancestor's import loaded %d times, want exactly its own one:\n%s", n, got)
		}
	})
}

// With no Root, imports keep today's freedom, workspace policy or not.
func TestMemoryImportsUnchangedWithoutRoot(t *testing.T) {
	l := newImportLayout(t)
	writeFile(t, filepath.Join(l.workDir, "CLAUDE.md"),
		"SUB-BODY\n\n@./shared/in.md\n\n@"+l.outside+"/env-sub.md\n\n@~/outside/secret.md")

	opts := workspaceOpts(l)
	opts.Root = ""
	got, mtimes := LoadMemory(l.workDir, opts)

	for _, want := range []string{"SUB-BODY", "INSIDE-IMPORT", "OUTSIDE-ENV-SUB", "OUTSIDE-SECRET"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s missing without a Root:\n%s", want, got)
		}
	}
	for _, loaded := range []string{filepath.Join(l.outside, "env-sub.md"), filepath.Join(l.home, "outside", "secret.md")} {
		if _, ok := mtimes[loaded]; !ok {
			t.Errorf("%s was loaded but not tracked: %v", loaded, mtimes)
		}
	}
}

// A Root that bounds nothing confines nothing, even though it is set.
func TestMemoryImportsUnrelatedRootConfinesNothing(t *testing.T) {
	l := newImportLayout(t)
	writeFile(t, filepath.Join(l.workDir, "CLAUDE.md"), "SUB-BODY @./shared/in.md")

	opts := workspaceOpts(l)
	opts.Root = l.outside
	got, _ := LoadMemory(l.workDir, opts)

	for _, want := range []string{"SUB-BODY", "INSIDE-IMPORT"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s missing with an unrelated Root:\n%s", want, got)
		}
	}
}

// workDir is the root itself: its imports are still confined to it.
func TestMemoryImportsWhenWorkDirIsRoot(t *testing.T) {
	l := newImportLayout(t)
	writeFile(t, filepath.Join(l.root, "CLAUDE.md"), "ROOT-AS-WORKDIR @"+l.outside+"/env-sub.md")

	opts := workspaceOpts(l)
	opts.Root = l.root
	got, _ := LoadMemory(l.root, opts)

	if !strings.Contains(got, "ROOT-AS-WORKDIR") {
		t.Errorf("the file is missing:\n%s", got)
	}
	if strings.Contains(got, "OUTSIDE-ENV-SUB") {
		t.Errorf("the import escaped the root:\n%s", got)
	}
}

// A workspace file that is itself a symlink out of the root is skipped; one
// linked inside loads; and with no Root everything loads as today. The
// skipped file stays a discovery candidate, so re-pointing the link inside is
// still seen by the cache.
func TestMemoryWorkspaceSymlinkedFiles(t *testing.T) {
	l := newImportLayout(t)
	link := func(target, path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	wdClaude := filepath.Join(l.workDir, "CLAUDE.md")
	cfgClaude := filepath.Join(l.workDir, ".claude", "CLAUDE.md")
	link(filepath.Join(l.outside, "wd-claude.md"), wdClaude)
	link(filepath.Join(l.outside, "cfg-claude.md"), cfgClaude)

	got, mtimes := LoadMemory(l.workDir, workspaceOpts(l))
	for _, leaked := range []string{"WORKDIR-CLAUDE-THROUGH-A-LINK", "WORKDIR-CONFIG-THROUGH-A-LINK"} {
		if strings.Contains(got, leaked) {
			t.Errorf("%s reached the prompt through a symlink:\n%s", leaked, got)
		}
	}
	for _, skipped := range []string{wdClaude, cfgClaude} {
		if _, ok := mtimes[skipped]; ok {
			t.Errorf("a skipped symlinked file was recorded as read: %s", skipped)
		}
	}

	// Linked inside: the same mechanism loads the file.
	if err := os.Remove(wdClaude); err != nil {
		t.Fatal(err)
	}
	link(filepath.Join(l.workDir, "shared", "in.md"), wdClaude)
	got, _ = LoadMemory(l.workDir, workspaceOpts(l))
	if !strings.Contains(got, "INSIDE-IMPORT") {
		t.Errorf("a workspace file linked inside the root was dropped:\n%s", got)
	}

	// No Root: the out-of-tree link loads, as it always has.
	if err := os.Remove(wdClaude); err != nil {
		t.Fatal(err)
	}
	link(filepath.Join(l.outside, "wd-claude.md"), wdClaude)
	unscoped := workspaceOpts(l)
	unscoped.Root = ""
	got, _ = LoadMemory(l.workDir, unscoped)
	if !strings.Contains(got, "WORKDIR-CLAUDE-THROUGH-A-LINK") {
		t.Errorf("without a Root the symlinked file must load as today:\n%s", got)
	}

	// The skipped file is still a candidate: pointing it inside is seen.
	pointed := workspaceOpts(l)
	a := NewAssemblerWithOptions(l.workDir, AssembleOptions{ProjectInstructions: true, Memory: pointed})
	if got := a.Assemble(); strings.Contains(got, "WORKDIR-CLAUDE-THROUGH-A-LINK") {
		t.Fatalf("the out-of-tree link loaded:\n%s", got)
	}
	if _, ok := MemoryCandidateMtimes(l.workDir, pointed)[wdClaude]; !ok {
		t.Error("a skipped symlinked file is no longer a discovery candidate")
	}
	if err := os.Remove(wdClaude); err != nil {
		t.Fatal(err)
	}
	link(filepath.Join(l.workDir, "shared", "in.md"), wdClaude)
	bumpMtime(t, wdClaude) // a retarget can land on the same mtime
	if got := a.Assemble(); !strings.Contains(got, "INSIDE-IMPORT") {
		t.Errorf("re-pointing the link inside was not picked up:\n%s", got)
	}
}
