// Package github implements the adapter.IssueTracker port via `gh` shell-out.
// No GitHub Go SDK is used.
package github

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/unsafe9/clutch/internal/adapter"
	"github.com/unsafe9/clutch/internal/model"
)

// trackerName is the backend identifier.
const trackerName = "github"

// Tracker is the github IssueTracker backend (shells out to `gh`).
type Tracker struct{}

// Compile-time proof that *Tracker implements the port.
var _ adapter.IssueTracker = (*Tracker)(nil)

// New returns a github issue tracker.
func New() *Tracker { return &Tracker{} }

// Name implements adapter.IssueTracker.
func (t *Tracker) Name() string { return trackerName }

// Fetch implements adapter.IssueTracker. It shells out to
// `gh issue view <number> --json number,title,url,state`, passing `--repo` when
// the key carries an owner/repo prefix. On any gh error it returns a zero Issue
// plus the error.
func (t *Tracker) Fetch(key string) (model.Issue, error) {
	repo, number := parseKey(key)

	args := []string{"issue", "view", number, "--json", "number,title,url,state"}
	if repo != "" {
		args = append(args, "--repo", repo)
	}

	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		return model.Issue{}, fmt.Errorf("gh issue view %s: %w", key, err)
	}

	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"url"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return model.Issue{}, fmt.Errorf("gh issue view %s: parse: %w", key, err)
	}

	return model.Issue{
		Ref:     model.RepRef("issue:" + trackerName + "/" + key),
		Tracker: trackerName,
		Key:     key,
		URL:     raw.URL,
		State:   raw.State,
	}, nil
}

// parseKey splits an issue key into an optional owner/repo locator and the issue
// number. Forms accepted:
//
//	"123"                  -> repo="",            number="123"
//	"#123"                 -> repo="",            number="123"
//	"owner/repo#123"       -> repo="owner/repo",  number="123"
//	"owner/repo/123"       -> repo="owner/repo",  number="123"
//
// The number is passed through verbatim; gh validates it.
func parseKey(key string) (repo, number string) {
	if i := strings.LastIndex(key, "#"); i >= 0 {
		return strings.TrimSuffix(key[:i], "/"), key[i+1:]
	}
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}
