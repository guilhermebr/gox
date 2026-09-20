//go:build integration

package temporal_test

import (
	"context"
	"os"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/guilhermebr/gox/providers/temporal"
)

func greetActivity(_ context.Context, name string) (string, error) { return "hello " + name, nil }

func greetWorkflow(ctx workflow.Context, name string) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 10 * time.Second})
	var out string
	err := workflow.ExecuteActivity(ctx, greetActivity, name).Get(ctx, &out)
	return out, err
}

// Needs a Temporal server: TEMPORAL_ADDRESS=127.0.0.1:7233 (temporal server start-dev).
func TestAWorkflowRunsOnTheAppsWorkerAndDrainsOnShutdown(t *testing.T) {
	addr := os.Getenv("TEMPORAL_ADDRESS")
	if addr == "" {
		t.Skip("TEMPORAL_ADDRESS not set")
	}
	t.Setenv("BILLING_TEMPORAL_ADDRESS", addr)
	a := newApp(t)
	w := temporal.Worker(a, "gox-test", worker.Options{})
	w.RegisterWorkflow(greetWorkflow)
	w.RegisterActivity(greetActivity)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	for !a.Health().IsReady() {
		time.Sleep(10 * time.Millisecond)
	}

	run, err := temporal.From(a).ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: "gox-test"}, greetWorkflow, "ana")
	if err != nil {
		t.Fatal(err)
	}
	var out string
	if err := run.Get(ctx, &out); err != nil || out != "hello ana" {
		t.Fatalf("workflow = %q %v", out, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("RunContext = %v", err)
	}
}
