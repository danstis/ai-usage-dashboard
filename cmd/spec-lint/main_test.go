package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_DefaultPathValidates(t *testing.T) {
	// Run from the repo root so the default "api/openapi.yaml" path resolves.
	t.Chdir(filepath.Join("..", ".."))

	var stdout, stderr bytes.Buffer
	err := run([]string{"spec-lint"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("expected run() to succeed for the default spec, got: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "spec-lint:") {
		t.Errorf("expected status line on stdout, got: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no output on stderr, got: %q", stderr.String())
	}
}

func TestRun_CustomPathValidates(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))

	var stdout, stderr bytes.Buffer
	err := run([]string{"spec-lint", "api/openapi.yaml"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("expected run() to succeed for the explicit spec path, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("expected OK marker on stdout, got: %q", stdout.String())
	}
}

func TestRun_InvalidPathReturnsError(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))

	var stdout, stderr bytes.Buffer
	err := run([]string{"spec-lint", "testdata/does-not-exist.yaml"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected run() to return error for a missing spec path, got nil")
	}
	if !strings.Contains(stderr.String(), "spec-lint:") {
		t.Errorf("expected error line on stderr, got: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("expected no stdout on failure, got: %q", stdout.String())
	}
}

func TestRun_BrokenFixtureReturnsError(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", "internal", "specvalidate"))

	var stdout, stderr bytes.Buffer
	err := run([]string{"spec-lint", "testdata/broken.yaml"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected run() to return error for the broken fixture, got nil")
	}
	if !strings.Contains(stderr.String(), "spec-lint:") {
		t.Errorf("expected error line on stderr, got: %q", stderr.String())
	}
}
