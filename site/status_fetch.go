package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/turbolytics/temper/findings"
)

// ourLogin is the account that posts our findings. Its comments are never
// a change.
const ourLogin = "turbolytics"

// CheckFindings fetches the GitHub state of every URL findings.csv names,
// writes findings/snapshot.json, and prints to out what changed since the
// last snapshot, with a proposed status where a rule decides it. It never
// edits findings.csv, and it writes nothing if any fetch fails. Set
// GITHUB_TOKEN: the GraphQL API that serves Discussions requires one.
func CheckFindings(root, api string, client *http.Client, out io.Writer) error {
	rows, err := findings.Load(filepath.Join(root, "findings", "findings.csv"))
	if err != nil {
		return err
	}
	byURL := map[string][]findings.Finding{}
	var urls []string
	for _, f := range rows {
		if _, ok := byURL[f.URL]; !ok {
			urls = append(urls, f.URL)
		}
		byURL[f.URL] = append(byURL[f.URL], f)
	}

	gh := github{api: strings.TrimRight(api, "/"), client: client}
	cur := Snapshot{}
	for _, url := range urls {
		e, err := gh.fetch(byURL[url][0])
		if err != nil {
			return fmt.Errorf("%s: %w", url, err)
		}
		cur[url] = e
	}

	path := filepath.Join(root, "findings", "snapshot.json")
	var old Snapshot
	switch b, err := os.ReadFile(path); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if old, err = DecodeSnapshot(b); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	b, err := EncodeSnapshot(cur)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}

	changes := Diff(old, cur)
	switch {
	case old == nil:
		fmt.Fprintf(out, "baseline: %d URLs recorded\n", len(cur))
	case len(changes) == 0:
		fmt.Fprintf(out, "no changes in %d URLs\n", len(cur))
	default:
		report(out, changes, byURL, old, cur)
	}
	return nil
}

// report prints one block per changed URL, in Diff's order.
func report(out io.Writer, changes []Change, byURL map[string][]findings.Finding, old, cur Snapshot) {
	for i := 0; i < len(changes); {
		url := changes[i].URL
		rows := byURL[url]
		first := rows[0]
		var items, statuses []string
		for _, f := range rows {
			statuses = append(statuses, f.Status)
			if f.Item > 0 {
				items = append(items, strconv.Itoa(f.Item))
			}
		}
		head := fmt.Sprintf("%s#%d (%s)", first.Repo, first.Number, first.Kind)
		if len(items) > 0 {
			head += ", items " + strings.Join(items, ", ")
		}
		fmt.Fprintf(out, "%s (ledger: %s)\n", head, strings.Join(statuses, ", "))
		newest := false
		for ; i < len(changes) && changes[i].URL == url; i++ {
			c := changes[i]
			fmt.Fprintf(out, "  %s: %s → %s\n", c.Field, c.Old, c.New)
			newest = newest || c.Field == "newest" || c.Field == "new"
		}
		e := cur[url]
		if status, ok := Propose(first.Kind, e); ok {
			fmt.Fprintf(out, "  proposed: %s\n", status)
		} else {
			fmt.Fprintln(out, "  proposed: needs review")
		}
		if newest && e.Newest != nil {
			body := strings.Join(strings.Fields(e.NewestBody), " ")
			if r := []rune(body); len(r) > 300 {
				body = string(r[:300]) + "…"
			}
			fmt.Fprintf(out, "  newest comment: %s (%s) %s: %s\n", e.Newest.Author, e.Newest.Association, e.Newest.Date, body)
		}
		fmt.Fprintln(out)
	}
}

type github struct {
	api    string
	client *http.Client
}

func (g github) fetch(f findings.Finding) (Entry, error) {
	if f.IsReport() {
		return g.discussion(f.Repo, f.Number)
	}
	return g.issue(f.Repo, f.Number)
}

type ghUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type ghIssue struct {
	State       string                  `json:"state"`
	StateReason string                  `json:"state_reason"`
	ClosedAt    string                  `json:"closed_at"`
	ClosedBy    *ghUser                 `json:"closed_by"`
	Draft       bool                    `json:"draft"`
	Labels      []struct{ Name string } `json:"labels"`
	PullRequest *struct {
		MergedAt string `json:"merged_at"`
	} `json:"pull_request"`
}

type ghEvent struct {
	Event       string  `json:"event"`
	ID          int64   `json:"id"`
	User        *ghUser `json:"user"`
	Association string  `json:"author_association"`
	CreatedAt   string  `json:"created_at"`
	Body        string  `json:"body"`
	Source      *struct {
		Issue *struct {
			Number     int    `json:"number"`
			State      string `json:"state"`
			Draft      bool   `json:"draft"`
			Repository *struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			PullRequest *struct {
				MergedAt string `json:"merged_at"`
			} `json:"pull_request"`
		} `json:"issue"`
	} `json:"source"`
}

// issue reads an issue or a pull request: the issues endpoint serves both,
// and its timeline holds the comments and the PRs that mention it.
func (g github) issue(repo string, n int) (Entry, error) {
	var is ghIssue
	base := fmt.Sprintf("%s/repos/%s/issues/%d", g.api, repo, n)
	if _, err := g.getJSON(base, &is); err != nil {
		return Entry{}, err
	}
	e := Entry{State: is.State, StateReason: is.StateReason, Closed: day(is.ClosedAt), Draft: is.Draft}
	if is.ClosedBy != nil {
		e.ClosedBy = is.ClosedBy.Login
	}
	for _, l := range is.Labels {
		e.Labels = append(e.Labels, l.Name)
	}
	if is.PullRequest != nil && is.PullRequest.MergedAt != "" {
		e.State, e.Merged = "merged", day(is.PullRequest.MergedAt)
	}

	linked := map[string]bool{}
	next := base + "/timeline?per_page=100"
	for next != "" {
		var page []ghEvent
		link, err := g.getJSON(next, &page)
		if err != nil {
			return Entry{}, err
		}
		for _, ev := range page {
			switch ev.Event {
			case "commented":
				login, kind := "ghost", "User"
				if ev.User != nil {
					login, kind = ev.User.Login, ev.User.Type
				}
				e.comment(login, kind, strconv.FormatInt(ev.ID, 10), ev.Association, ev.CreatedAt, ev.Body)
			case "cross-referenced":
				if ev.Source == nil || ev.Source.Issue == nil || ev.Source.Issue.PullRequest == nil || ev.Source.Issue.Repository == nil {
					continue
				}
				src := ev.Source.Issue
				ref := fmt.Sprintf("%s#%d", src.Repository.FullName, src.Number)
				if linked[ref] {
					continue
				}
				linked[ref] = true
				state := src.State
				switch {
				case src.PullRequest.MergedAt != "":
					state = "merged"
				case src.Draft:
					state = "draft"
				}
				e.LinkedPRs = append(e.LinkedPRs, LinkedPR{ref, state})
			}
		}
		next = nextPage(link)
	}
	return e, nil
}

const discussionQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    discussion(number: $number) {
      closed
      isAnswered
      comments(last: 100) {
        nodes {
          id author { login } authorAssociation createdAt body
          replies(last: 100) { nodes { id author { login } authorAssociation createdAt body } }
        }
      }
    }
  }
}`

type gqlComment struct {
	ID     string `json:"id"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	Association string `json:"authorAssociation"`
	CreatedAt   string `json:"createdAt"`
	Body        string `json:"body"`
	Replies     *struct {
		Nodes []gqlComment `json:"nodes"`
	} `json:"replies"`
}

// discussion reads a Discussion through GraphQL. Replies under a comment
// count as comments.
func (g github) discussion(repo string, n int) (Entry, error) {
	owner, name, _ := strings.Cut(repo, "/")
	payload, _ := json.Marshal(map[string]any{
		"query":     discussionQuery,
		"variables": map[string]any{"owner": owner, "name": name, "number": n},
	})
	var resp struct {
		Data struct {
			Repository struct {
				Discussion *struct {
					Closed     bool `json:"closed"`
					IsAnswered bool `json:"isAnswered"`
					Comments   struct {
						Nodes []gqlComment `json:"nodes"`
					} `json:"comments"`
				} `json:"discussion"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := g.do(http.MethodPost, g.api+"/graphql", payload, &resp, nil); err != nil {
		return Entry{}, err
	}
	if len(resp.Errors) > 0 {
		return Entry{}, fmt.Errorf("GraphQL: %s", resp.Errors[0].Message)
	}
	d := resp.Data.Repository.Discussion
	if d == nil {
		return Entry{}, errors.New("GraphQL: no such Discussion")
	}
	e := Entry{State: "open", Answered: d.IsAnswered}
	if d.Closed {
		e.State = "closed"
	}
	var walk func([]gqlComment)
	walk = func(cs []gqlComment) {
		for _, c := range cs {
			login := "ghost"
			if c.Author != nil {
				login = c.Author.Login
			}
			e.comment(login, "User", c.ID, c.Association, c.CreatedAt, c.Body)
			if c.Replies != nil {
				walk(c.Replies.Nodes)
			}
		}
	}
	walk(d.Comments.Nodes)
	return e, nil
}

// comment counts a comment unless it is ours or a bot's, and keeps the
// newest one. A later comment with the same time wins.
func (e *Entry) comment(login, userType, id, association, createdAt, body string) {
	if strings.EqualFold(login, ourLogin) || userType == "Bot" || strings.HasSuffix(login, "[bot]") {
		return
	}
	e.OthersComments++
	if e.Newest == nil || createdAt >= e.newestTime {
		e.Newest = &Comment{ID: id, Author: login, Association: association, Date: day(createdAt)}
		e.NewestBody = body
		e.newestTime = createdAt
	}
}

func (g github) getJSON(url string, v any) (string, error) {
	h := http.Header{}
	if err := g.do(http.MethodGet, url, nil, v, h); err != nil {
		return "", err
	}
	return h.Get("Link"), nil
}

// do sends one request and decodes a 200 response into v, copying the
// response headers into h when h is not nil.
func (g github) do(method, url string, body []byte, v any, h http.Header) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned %s for %s", resp.Status, url)
	}
	for k, vs := range resp.Header {
		if h != nil {
			h[k] = vs
		}
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func nextPage(link string) string {
	if m := nextLink.FindStringSubmatch(link); m != nil {
		return m[1]
	}
	return ""
}

// day turns an RFC 3339 time into its UTC day, or "" for no time.
func day(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format(time.DateOnly)
}
