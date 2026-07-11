package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, func(string) string { return "" }, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"toolkitVersion":"v0.2.0"`) || !strings.Contains(stdout.String(), `"referenceCliVersion":"2.0.8"`) || !strings.Contains(stdout.String(), `"targetPlatform":"linux/amd64"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"build"}, func(string) string { return "" }, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d", code)
	}
}
