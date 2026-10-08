package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"testing"
)

func decode(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func getJSONMap(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return decode(t, resp.Body)
}

func assertKeys(t *testing.T, what string, got map[string]any, required []string) {
	t.Helper()
	sort.Strings(required)
	for _, k := range required {
		if _, ok := got[k]; !ok {
			t.Errorf("%s response lacks required field %q (has %v)", what, k, keys(got))
		}
	}
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
