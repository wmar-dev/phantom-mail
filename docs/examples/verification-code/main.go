// Command verification-code waits for a verification email in a Phantom Mail
// mailbox and prints the code from it. It is the Go version of the pattern in
// docs/testing-verification-flows.md and uses only the standard library.
//
//	go run ./docs/examples/verification-code -mailbox signup-123
//
// Exit status 0 means a code was printed; 1 means no email arrived in time or
// it contained no code; 2 means a usage or connection error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

func main() {
	api := flag.String("api", "http://localhost:8080", "base URL of the Phantom Mail instance")
	box := flag.String("mailbox", "", "mailbox name to watch (required)")
	timeout := flag.Duration("timeout", 30*time.Second, "how long to wait for the email")
	pattern := flag.String("pattern", `\b\d{6}\b`, "regular expression matching the code")
	flag.Parse()
	if *box == "" {
		fmt.Fprintln(os.Stderr, "usage: verification-code -mailbox NAME [-api URL] [-timeout 30s] [-pattern REGEXP]")
		os.Exit(2)
	}
	re, err := regexp.Compile(*pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad -pattern:", err)
		os.Exit(2)
	}
	code, err := waitForCode(*api, *box, *timeout, re, os.Getenv("PM_API_TOKEN"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if strings.HasPrefix(err.Error(), "no email") || strings.HasPrefix(err.Error(), "no code") {
			os.Exit(1)
		}
		os.Exit(2)
	}
	fmt.Println(code)
}

func waitForCode(api, box string, timeout time.Duration, re *regexp.Regexp, token string) (string, error) {
	secs := int(timeout.Seconds())
	if secs < 1 {
		secs = 1
	}
	if secs > 60 {
		secs = 60
	}
	base := strings.TrimRight(api, "/") + "/api/v1/mailboxes/" + url.PathEscape(box) + "/messages"
	client := &http.Client{Timeout: time.Duration(secs+10) * time.Second}

	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	status, err := get(client, fmt.Sprintf("%s/wait?timeout=%d", base, secs), token, &list)
	if err != nil {
		return "", err
	}
	if status == http.StatusNoContent || len(list.Messages) == 0 {
		return "", fmt.Errorf("no email arrived in mailbox %q within %v", box, timeout)
	}

	var msg struct {
		Subject string `json:"subject"`
		Text    string `json:"text"`
		HTML    string `json:"html"`
	}
	if _, err := get(client, base+"/"+list.Messages[0].ID, token, &msg); err != nil {
		return "", err
	}
	for _, s := range []string{msg.Subject, msg.Text, msg.HTML} {
		if code := re.FindString(s); code != "" {
			return code, nil
		}
	}
	return "", fmt.Errorf("no code matching %q in the email %q", re, msg.Subject)
}

func get(c *http.Client, u, token string, out any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("%s returned %s: %s", u, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp.StatusCode, json.Unmarshal(body, out)
}
