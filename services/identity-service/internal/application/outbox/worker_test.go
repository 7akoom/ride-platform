package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type workerTestProcessor struct {
	mu      sync.Mutex
	calls   int
	results []ProcessResult
	errors  []error
	onCall  func(int)
}

func (p *workerTestProcessor) ProcessOnce(
	ctx context.Context,
) (ProcessResult, error) {
	p.mu.Lock()

	p.calls++
	call := p.calls

	var result ProcessResult
	if call <= len(p.results) {
		result = p.results[call-1]
	}

	var err error
	if call <= len(p.errors) {
		err = p.errors[call-1]
	}

	onCall := p.onCall

	p.mu.Unlock()

	if onCall != nil {
		onCall(call)
	}

	return result, err
}

func (p *workerTestProcessor) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.calls
}

func newWorkerTestLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)
}

func TestWorkerRunProcessesImmediately(
	t *testing.T,
) {
	ctx, cancel :=
		context.WithCancel(
			context.Background(),
		)

	called := make(
		chan struct{},
		1,
	)

	processor := &workerTestProcessor{
		onCall: func(call int) {
			if call == 1 {
				called <- struct{}{}
				cancel()
			}
		},
	}

	worker := NewWorker(
		processor,
		newWorkerTestLogger(),
		WorkerConfig{
			PollInterval: time.Hour,
		},
	)

	done := make(
		chan error,
		1,
	)

	go func() {
		done <- worker.Run(ctx)
	}()

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal(
			"worker did not process immediately",
		)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf(
				"Run() returned an error: %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker did not stop after cancellation",
		)
	}

	if processor.callCount() != 1 {
		t.Fatalf(
			"ProcessOnce() calls = %d, expected 1",
			processor.callCount(),
		)
	}
}

func TestWorkerRunContinuesAfterProcessingError(
	t *testing.T,
) {
	ctx, cancel :=
		context.WithCancel(
			context.Background(),
		)

	secondCall := make(
		chan struct{},
		1,
	)

	processor := &workerTestProcessor{
		results: []ProcessResult{
			{
				Claimed:        1,
				RetryScheduled: 1,
			},
			{
				Claimed:   1,
				Published: 1,
			},
		},
		errors: []error{
			errors.New(
				"publish failed",
			),
			nil,
		},
		onCall: func(call int) {
			if call == 2 {
				secondCall <- struct{}{}
				cancel()
			}
		},
	}

	worker := NewWorker(
		processor,
		newWorkerTestLogger(),
		WorkerConfig{
			PollInterval: time.Millisecond,
		},
	)

	done := make(
		chan error,
		1,
	)

	go func() {
		done <- worker.Run(ctx)
	}()

	select {
	case <-secondCall:
	case <-time.After(time.Second):
		t.Fatal(
			"worker did not continue after processing error",
		)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf(
				"Run() returned an error: %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker did not stop after cancellation",
		)
	}

	if processor.callCount() != 2 {
		t.Fatalf(
			"ProcessOnce() calls = %d, expected 2",
			processor.callCount(),
		)
	}
}

func TestWorkerRunRepeatsProcessingAfterPollInterval(
	t *testing.T,
) {
	ctx, cancel :=
		context.WithCancel(
			context.Background(),
		)

	thirdCall := make(
		chan struct{},
		1,
	)

	processor := &workerTestProcessor{
		onCall: func(call int) {
			if call == 3 {
				thirdCall <- struct{}{}
				cancel()
			}
		},
	}

	worker := NewWorker(
		processor,
		newWorkerTestLogger(),
		WorkerConfig{
			PollInterval: time.Millisecond,
		},
	)

	done := make(
		chan error,
		1,
	)

	go func() {
		done <- worker.Run(ctx)
	}()

	select {
	case <-thirdCall:
	case <-time.After(time.Second):
		t.Fatal(
			"worker did not repeat processing",
		)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf(
				"Run() returned an error: %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker did not stop after cancellation",
		)
	}

	if processor.callCount() != 3 {
		t.Fatalf(
			"ProcessOnce() calls = %d, expected 3",
			processor.callCount(),
		)
	}
}

func TestWorkerRunStopsWhileWaitingForNextPoll(
	t *testing.T,
) {
	ctx, cancel :=
		context.WithCancel(
			context.Background(),
		)

	firstCall := make(
		chan struct{},
		1,
	)

	processor := &workerTestProcessor{
		onCall: func(call int) {
			if call == 1 {
				firstCall <- struct{}{}
			}
		},
	}

	worker := NewWorker(
		processor,
		newWorkerTestLogger(),
		WorkerConfig{
			PollInterval: time.Hour,
		},
	)

	done := make(
		chan error,
		1,
	)

	go func() {
		done <- worker.Run(ctx)
	}()

	select {
	case <-firstCall:
	case <-time.After(time.Second):
		t.Fatal(
			"worker did not perform first processing cycle",
		)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf(
				"Run() returned an error: %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker did not stop while waiting for next poll",
		)
	}

	if processor.callCount() != 1 {
		t.Fatalf(
			"ProcessOnce() calls = %d, expected 1",
			processor.callCount(),
		)
	}
}

func TestWorkerRunReturnsImmediatelyForCancelledContext(
	t *testing.T,
) {
	ctx, cancel :=
		context.WithCancel(
			context.Background(),
		)

	cancel()

	processor :=
		&workerTestProcessor{}

	worker := NewWorker(
		processor,
		newWorkerTestLogger(),
		WorkerConfig{
			PollInterval: time.Second,
		},
	)

	err := worker.Run(ctx)
	if err != nil {
		t.Fatalf(
			"Run() returned an error: %v",
			err,
		)
	}

	if processor.callCount() != 0 {
		t.Fatalf(
			"ProcessOnce() calls = %d, expected 0",
			processor.callCount(),
		)
	}
}

func TestNewWorkerPanicsForInvalidConfiguration(
	t *testing.T,
) {
	validProcessor :=
		&workerTestProcessor{}

	validLogger :=
		newWorkerTestLogger()

	tests := []struct {
		name      string
		processor ProcessorRunner
		logger    *slog.Logger
		config    WorkerConfig
	}{
		{
			name:      "nil processor",
			processor: nil,
			logger:    validLogger,
			config: WorkerConfig{
				PollInterval: time.Second,
			},
		},
		{
			name:      "nil logger",
			processor: validProcessor,
			logger:    nil,
			config: WorkerConfig{
				PollInterval: time.Second,
			},
		},
		{
			name:      "zero poll interval",
			processor: validProcessor,
			logger:    validLogger,
			config: WorkerConfig{
				PollInterval: 0,
			},
		},
		{
			name:      "negative poll interval",
			processor: validProcessor,
			logger:    validLogger,
			config: WorkerConfig{
				PollInterval: -time.Second,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				defer func() {
					if recover() == nil {
						t.Fatal(
							"NewWorker() did not panic",
						)
					}
				}()

				NewWorker(
					testCase.processor,
					testCase.logger,
					testCase.config,
				)
			},
		)
	}
}
