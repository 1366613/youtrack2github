package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gocarina/gocsv"
	"golang.org/x/net/http2"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args
	if len(args) != 7 {
		fmt.Println("Wrong number of arguments\n" +
			"Input in this form: ./github2youtrack token user repo inputFile label milestone")
		os.Exit(0)
	}
	token := args[1]
	user := args[2]
	repo := args[3]
	inputFile := args[4]
	label := args[5]
	milestone, err := strconv.ParseInt(args[6], 10, 64)
	if err != nil {
		log.Panic(err)
	}
	err, youtrackIssues := parseYouTrackIssues(inputFile)
	if err != nil {
		log.Panic(err)
	}

	if isLegacyToken(token) {
		fmt.Println("Classic GitHub personal access token are disallowed\n" +
			"Please create a fine-grained token at https://github.com/settings/personal-access-tokens/new")
		os.Exit(0)
	}

	fmt.Println("Due to the API rate limiting, don't create any issues on any projects on GitHub until I'm done!")
	for _, issue := range *youtrackIssues {
		err := addIssue(user, repo, token, issue, label, int(milestone))
		if err == nil {
			log.Printf("Issue %s created", issue.IssueId)
		} else {
			log.Printf("Unable to add issue %s: %s", issue.IssueId, err)
		}
		time.Sleep(2 * time.Second)
	}
}

type GitHubIssue struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Assignees []string `json:"assignees"`
	Milestone int      `json:"milestone"`
	Labels    []string `json:"labels"`
}

func addIssue(owner string, repoName string, token string, ytIssue YouTrackIssue, label string, milestone int) error {
	client := &http.Client{Transport: &http2.Transport{}}
	uri := "https://api.github.com/repos/" + owner + "/" + repoName + "/issues"
	issue := &GitHubIssue{
		Title:     ytIssue.Summary,
		Body:      ytIssue.Description,
		Assignees: []string{owner},
		Labels:    []string{label},
		Milestone: milestone}
	data, err := json.Marshal(issue)
	if err != nil {
		return err
	}
	buf := bytes.NewReader(data)
	req, err := http.NewRequest("POST", uri, buf)
	if err != nil {
		return err
	}
	req.Header.Add("Accept", "application/vnd.github+json")
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("X-GitHub-Api-Version", "2022-11-28")
doRequest:
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	rateLimit, err := strconv.Atoi(resp.Header.Get("X-Ratelimit-Remaining"))
	if err != nil {
		return err
	}
	rateLimitReset, err := strconv.ParseInt(resp.Header.Get("X-Ratelimit-Reset"), 10, 64)
	if err != nil {
		return err
	}
	rateLimitResetTime := time.Unix(rateLimitReset, 0)
	if rateLimit < 5 || resp.StatusCode == 403 {
		for {
			if time.Now().Before(rateLimitResetTime) {
				eta, err := getETA(rateLimitResetTime)
				if err != nil {
					return err
				}
				log.Printf("Waiting %s to pass rate limit threshold", eta.String())
				time.Sleep(eta)
			} else {
				goto doRequest
			}
		}
	}

	if resp.StatusCode != 201 {
		return errors.New(fmt.Sprintf("unable to create issue: status code: %d", resp.StatusCode))
	}
	return nil
}

type YouTrackIssues []YouTrackIssue

type YouTrackIssue struct {
	IssueId     string `csv:"Issue Id"`
	Project     string `csv:"Project"`
	Tags        string `csv:"Tags"`
	Summary     string `csv:"Summary"`
	Reporter    string `csv:"Reporter"`
	Created     string `csv:"Created"`
	Updated     string `csv:"Updated"`
	Resolved    string `csv:"Resolved"`
	Priority    string `csv:"Priority"`
	Motivation  string `csv:"Motivation"`
	State       string `csv:"State"`
	Area        string `csv:"Area"`
	Description string `csv:"Description"`
	Votes       string `csv:"Votes"`
}

func parseYouTrackIssues(path string) (error, *YouTrackIssues) {
	data, err := os.ReadFile(path)
	if err != nil {
		return err, nil
	}
	var youtrackIssues YouTrackIssues
	lines := strings.Split(string(data), "\n")

	var newData string

	for n, line := range lines {
		if n == 0 {
			header := strings.ReplaceAll(line, "\"", "")
			newData += header + "\n"
		} else {
			newData += line + "\n"
		}
	}

	err = gocsv.UnmarshalBytes([]byte(newData), &youtrackIssues)
	return err, &youtrackIssues
}

func getETA(t time.Time) (time.Duration, error) {
	remaining := t.Unix() - time.Now().Unix()
	var duration time.Duration
	if remaining <= 0 {
		return duration, errors.New("ETA is less than or equal to 0")
	}
	return time.ParseDuration(fmt.Sprintf("%ds", remaining))
}

func isLegacyToken(token string) bool {
	return strings.HasPrefix(token, "ghp_")
}
