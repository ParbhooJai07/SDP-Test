package gitservice

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const EmptyTreeHash = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

type Runner struct {
	Binary string
}

type Result struct {
	Stdout string
	Stderr string
}

type RawCommit struct {
	Hash           string
	ParentHash     string
	AuthorEmail    string
	AuthorName     string
	CommitterTS    int64
	Subject        string
	MailmapEmail   string
	MailmapName    string
}

type FileDelta struct {
	Path     string
	OldPath  string
	Added    int64
	Removed  int64
	IsRename bool
	IsBinary bool
}

type MailmapEntry struct {
	RawEmail      string
	RawName       string
	ResolvedEmail string
	ResolvedName  string
}

func NewRunner() Runner {
	return Runner{Binary: "git"}
}

func (r Runner) Run(ctx context.Context, workDir string, args ...string) (Result, error) {
	bin := r.Binary
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String()},
			fmt.Errorf("git %v failed: %w (stderr: %s)", args, err, stderr.String())
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func (r Runner) CloneRepo(ctx context.Context, url, destPath string) error {
	_, err := r.Run(ctx, "", "clone", url, destPath)
	return err
}

func (r Runner) ValidateRepo(ctx context.Context, path string) error {
	_, err := r.Run(ctx, path, "rev-parse", "--git-dir")
	return err
}

func (r Runner) ResolveRef(ctx context.Context, repoPath, ref string) (string, error) {
	res, err := r.Run(ctx, repoPath, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (r Runner) ListCommits(ctx context.Context, repoPath, ref string) ([]RawCommit, error) {
	// Get raw commit data
	res, err := r.Run(ctx, repoPath, "log", "--no-merges", "--format=%H %P%n%ae%n%an%n%ct%n%s", ref)
	if err != nil {
		return nil, err
	}

	// Get mailmap-resolved identities
	mailmapRes, err := r.Run(ctx, repoPath, "log", "--no-merges", "--mailmap", "--format=%H %aE %aN", ref)
	if err != nil {
		return nil, err
	}

	mailmap := make(map[string][2]string) // hash -> [email, name]
	for _, line := range strings.Split(strings.TrimSpace(mailmapRes.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) >= 3 {
			mailmap[parts[0]] = [2]string{parts[1], parts[2]}
		}
	}

	var commits []RawCommit
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	for i := 0; i+4 < len(lines); i += 5 {
		hashLine := lines[i]
		parts := strings.SplitN(hashLine, " ", 2)
		hash := parts[0]
		parentHash := ""
		if len(parts) > 1 {
			parentHash = parts[1]
		}

		email := lines[i+1]
		name := lines[i+2]
		tsStr := lines[i+3]
		subject := lines[i+4]

		ts, _ := strconv.ParseInt(tsStr, 10, 64)

		c := RawCommit{
			Hash:        hash,
			ParentHash:  parentHash,
			AuthorEmail: email,
			AuthorName:  name,
			CommitterTS: ts,
			Subject:     subject,
		}

		if mm, ok := mailmap[hash]; ok {
			c.MailmapEmail = mm[0]
			c.MailmapName = mm[1]
		} else {
			c.MailmapEmail = email
			c.MailmapName = name
		}

		commits = append(commits, c)
	}

	return commits, nil
}

func (r Runner) DiffCommit(ctx context.Context, repoPath, hash, parentHash string) ([]FileDelta, error) {
	if parentHash == "" {
		parentHash = EmptyTreeHash
	}

	res, err := r.Run(ctx, repoPath, "diff", "--numstat", "-z", "--find-renames=50%", parentHash, hash)
	if err != nil {
		return nil, err
	}

	return parseNumstatNul(res.Stdout), nil
}

func parseNumstatNul(raw string) []FileDelta {
	var deltas []FileDelta

	// Split on NUL to handle filenames with special chars
	parts := strings.Split(raw, "\000")

	i := 0
	for i < len(parts) {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			i++
			continue
		}

		// Each record starts with "added\tremoved\tpath" or "added\tremoved\t" for renames
		scanner := bufio.NewScanner(strings.NewReader(part))
		scanner.Split(bufio.ScanWords)

		fields := strings.Split(part, "\t")
		if len(fields) < 2 {
			i++
			continue
		}

		addedStr := fields[0]
		removedStr := fields[1]

		// Binary file
		if addedStr == "-" && removedStr == "-" {
			i++
			// Skip the filename(s)
			if i < len(parts) && len(fields) < 3 {
				i++
			}
			continue
		}

		added, _ := strconv.ParseInt(addedStr, 10, 64)
		removed, _ := strconv.ParseInt(removedStr, 10, 64)

		var path string
		if len(fields) >= 3 && fields[2] != "" {
			path = fields[2]
			// Check for rename pattern in the path itself
			if strings.Contains(path, " => ") || strings.Contains(path, "=>") {
				// Rename where both paths are in same field
				delta := FileDelta{
					Path:     extractNewPath(path),
					OldPath:  extractOldPath(path),
					Added:    added,
					Removed:  removed,
					IsRename: true,
				}
				deltas = append(deltas, delta)
				i++
				continue
			}
			deltas = append(deltas, FileDelta{Path: path, Added: added, Removed: removed})
			i++
		} else {
			// Rename: next two NUL-separated entries are old and new paths
			i++
			oldPath := ""
			newPath := ""
			if i < len(parts) {
				oldPath = strings.TrimSpace(parts[i])
				i++
			}
			if i < len(parts) {
				newPath = strings.TrimSpace(parts[i])
				i++
			}
			deltas = append(deltas, FileDelta{
				Path:     newPath,
				OldPath:  oldPath,
				Added:    added,
				Removed:  removed,
				IsRename: true,
			})
		}
	}

	return deltas
}

func extractNewPath(renamePath string) string {
	// Handle patterns like "{old => new}/file" or "dir/{old => new}"
	if idx := strings.Index(renamePath, "{"); idx >= 0 {
		end := strings.Index(renamePath, "}")
		if end > idx {
			prefix := renamePath[:idx]
			suffix := renamePath[end+1:]
			inner := renamePath[idx+1 : end]
			parts := strings.SplitN(inner, " => ", 2)
			if len(parts) == 2 {
				newPart := strings.TrimSpace(parts[1])
				return cleanPath(prefix + newPart + suffix)
			}
		}
	}
	// Simple "old => new"
	parts := strings.SplitN(renamePath, " => ", 2)
	if len(parts) == 2 {
		return cleanPath(strings.TrimSpace(parts[1]))
	}
	return renamePath
}

func extractOldPath(renamePath string) string {
	if idx := strings.Index(renamePath, "{"); idx >= 0 {
		end := strings.Index(renamePath, "}")
		if end > idx {
			prefix := renamePath[:idx]
			suffix := renamePath[end+1:]
			inner := renamePath[idx+1 : end]
			parts := strings.SplitN(inner, " => ", 2)
			if len(parts) == 2 {
				oldPart := strings.TrimSpace(parts[0])
				return cleanPath(prefix + oldPart + suffix)
			}
		}
	}
	parts := strings.SplitN(renamePath, " => ", 2)
	if len(parts) == 2 {
		return cleanPath(strings.TrimSpace(parts[0]))
	}
	return renamePath
}

func cleanPath(p string) string {
	p = strings.ReplaceAll(p, "//", "/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	return p
}
