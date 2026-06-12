package localcommit

import (
	"os/exec"
	"regexp"
	"strings"
)

func Slugify(title string) string {
	if title == "" {
		return "item"
	}

	// Convert to lowercase
	s := strings.ToLower(title)

	// Replace spaces and underscores with dashes
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '_' {
			return '-'
		}
		return r
	}, s)

	// Keep only ASCII lowercase letters, digits, and dashes
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	s = result.String()

	// Collapse repeated dashes
	re := regexp.MustCompile(`-+`)
	s = re.ReplaceAllString(s, "-")

	// Trim leading and trailing dashes
	s = strings.Trim(s, "-")

	if s == "" {
		return "item"
	}

	return s
}

func BaseRef() string {
	return "origin/main"
}

func MRBranch(slug string) string {
	return "wi/" + slug
}

func WIPBranch(iid string) string {
	return "wip/" + iid
}

func ResolveTip(repoDir, slug string) (string, error) {
	ref := "refs/heads/wi/" + slug
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", ref)
	err := cmd.Run()
	if err == nil {
		return "wi/" + slug, nil
	}
	return BaseRef(), nil
}
