package updater

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPullImageRetriesAfterStalledAttempt(t *testing.T) {
	image := "ghcr.io/guyu233/cli2api:v0.3.0"
	runner := &scriptedRunner{t: t, steps: []runnerStep{
		{name: "docker", args: []string{"pull", image}, err: errors.New("unexpected EOF")},
		{name: "docker", args: []string{"pull", image}},
	}}
	executor := NewExecutor(ExecutorConfig{})
	executor.runner = runner
	executor.pullBackoff = time.Millisecond
	if err := executor.pullImage(context.Background(), image); err != nil {
		t.Fatal(err)
	}
	runner.assertDone()
}

func TestPullImageGivesUpAfterAllAttempts(t *testing.T) {
	image := "ghcr.io/guyu233/cli2api:v0.3.0"
	stalled := errors.New("stalled")
	runner := &scriptedRunner{t: t, steps: []runnerStep{
		{name: "docker", args: []string{"pull", image}, err: stalled},
		{name: "docker", args: []string{"pull", image}, err: stalled},
		{name: "docker", args: []string{"pull", image}, err: stalled},
	}}
	executor := NewExecutor(ExecutorConfig{})
	executor.runner = runner
	executor.pullBackoff = time.Millisecond
	if err := executor.pullImage(context.Background(), image); err == nil {
		t.Fatal("expected the pull to fail after all attempts")
	}
	runner.assertDone()
}
