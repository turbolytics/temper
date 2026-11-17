package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/turbolytics/temper/findings"
)

// RefreshProjects rewrites findings/projects.csv with the GitHub stars and
// language of every project findings.csv names, as of today. Projects owned
// by turbolytics are marked ours. Set GITHUB_TOKEN to raise the API's rate
// limit.
func RefreshProjects(root, api, today string, client *http.Client) error {
	rows, err := findings.Load(filepath.Join(root, "findings", "findings.csv"))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var repos []string
	for _, f := range rows {
		if !seen[f.Repo] {
			seen[f.Repo] = true
			repos = append(repos, f.Repo)
		}
	}
	sort.Slice(repos, func(i, j int) bool { return strings.ToLower(repos[i]) < strings.ToLower(repos[j]) })

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(findings.ProjectsHeader)
	for _, repo := range repos {
		info, err := fetchRepo(client, api, repo)
		if err != nil {
			return fmt.Errorf("%s: %w", repo, err)
		}
		lang := info.Language
		if lang == "" {
			lang = "unknown"
		}
		ours := "no"
		if strings.EqualFold(strings.SplitN(repo, "/", 2)[0], "turbolytics") {
			ours = "yes"
		}
		_ = w.Write([]string{repo, strconv.Itoa(info.Stars), lang, today, ours})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if _, err := findings.ParseProjects(bytes.NewReader(buf.Bytes())); err != nil {
		return fmt.Errorf("refreshed projects.csv is invalid: %w", err)
	}
	return os.WriteFile(filepath.Join(root, "findings", "projects.csv"), buf.Bytes(), 0o644)
}

type repoInfo struct {
	Stars    int    `json:"stargazers_count"`
	Language string `json:"language"`
}

func fetchRepo(client *http.Client, api, repo string) (repoInfo, error) {
	var info repoInfo
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(api, "/")+"/repos/"+repo, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, err
	}
	return info, nil
}
