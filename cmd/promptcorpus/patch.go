package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// applyDiffToPromptsCopy copies the contents of srcDir into a fresh
// temp directory under a "prompts/" subdirectory and applies the
// supplied unified diff to that copy via "git apply -p1". The original
// srcDir is never written to. Callers must invoke the returned cleanup
// function to remove the temp tree.
//
// The diff is expected to use repo-root-relative paths of the form
// "a/prompts/<file>" / "b/prompts/<file>" — that is what the improver
// is told to emit — so we recreate a "prompts/" subdir inside the temp
// root and run git apply from there.
//
// LLMs sometimes emit a single diff containing multiple "--- a/<file>"
// sections for the same file (alternate or layered phrasings of the
// same edit). git apply chokes on those — it treats the second
// "--- a/..." as part of the first hunk's body and reports "corrupt
// patch". To recover, we split the diff on file-header boundaries and
// apply each section independently. Failure of one section is logged
// but does not abort the rest; the overall call only errors out if no
// section applied successfully.
func applyDiffToPromptsCopy(srcDir, diff string) (patchedDir string, cleanup func(), err error) {
	tmpRoot, err := os.MkdirTemp("", "promptcorpus-patched-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(tmpRoot) }

	promptsCopy := filepath.Join(tmpRoot, "prompts")
	if err := copyDirContents(srcDir, promptsCopy); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("copy prompts dir: %w", err)
	}

	sections := splitDiffByFileHeader(diff)
	if len(sections) == 0 {
		cleanup()
		return "", nil, fmt.Errorf("git apply: diff contained no '--- a/' file header")
	}

	var (
		applied  int
		lastErr  error
		failures []string
	)
	for i, section := range sections {
		if err := runGitApply(tmpRoot, section); err != nil {
			lastErr = err
			failures = append(failures, fmt.Sprintf("section %d: %v", i+1, err))
			log.Printf("git apply skipped section %d/%d: %v", i+1, len(sections), err)
			continue
		}
		applied++
	}

	if applied == 0 {
		cleanup()
		if len(sections) == 1 {
			return "", nil, lastErr
		}
		return "", nil, fmt.Errorf("git apply: no sections applied; %s", strings.Join(failures, "; "))
	}

	return promptsCopy, cleanup, nil
}

// splitDiffByFileHeader breaks a unified diff into one slice element per
// "--- a/<file>" header. Lines preceding the first header (commentary,
// stray markdown fences) are dropped — they are not part of any hunk.
// Each returned section starts with its own "--- a/..." line.
func splitDiffByFileHeader(diff string) []string {
	lines := strings.Split(diff, "\n")
	var sections []string
	var current []string
	for _, line := range lines {
		if strings.HasPrefix(line, "--- ") {
			if len(current) > 0 {
				sections = append(sections, strings.Join(current, "\n")+"\n")
			}
			current = []string{line}
			continue
		}
		if len(current) > 0 {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		sections = append(sections, strings.Join(current, "\n")+"\n")
	}
	return sections
}

// runGitApply runs "git apply -p1" against tmpRoot with the supplied
// patch text on stdin and returns a wrapped error containing git's
// stderr on failure.
func runGitApply(tmpRoot, patch string) error {
	cmd := exec.Command("git", "apply", "-p1", "--whitespace=nowarn", "-")
	cmd.Dir = tmpRoot
	cmd.Stdin = strings.NewReader(patch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("git apply: %w", err)
		}
		return fmt.Errorf("git apply: %w; stderr: %s", err, msg)
	}
	return nil
}

// copyDirContents recursively copies all regular files (and directory
// structure) under src into dst. Symlinks and other non-regular entries
// are skipped — the prompts directory is just text files.
func copyDirContents(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
