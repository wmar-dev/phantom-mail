package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// healthcheckCommand probes the local /healthz endpoint. It exists because
// the runtime container image has no shell or curl; the Dockerfile's
// HEALTHCHECK runs "phantom-mail healthcheck". Exit status 0 means healthy.
func healthcheckCommand(getenv func(string) string) int {
	addr := strings.TrimSpace(getenv("PM_HTTP_ADDR"))
	if addr == "" {
		addr = ":8080"
	}
	if err := probe(healthURL(addr), 3*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "unhealthy:", err)
		return 1
	}
	return 0
}

// healthURL turns a listen address into a URL reachable from the same host.
func healthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8080/healthz"
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

func probe(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	if !strings.Contains(string(body), `"status":"ok"`) {
		return fmt.Errorf("%s did not report ok: %s", url, strings.TrimSpace(string(body)))
	}
	return nil
}
