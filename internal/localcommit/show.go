package localcommit

import (
	"bytes"
	"os/exec"
)

func ShowCommit(repoDir, sha string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "show", sha)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func DiffBase(repoDir, sha string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "diff", "origin/main..."+sha)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return out.String(), nil
}
