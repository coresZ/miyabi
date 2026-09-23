package scrape

import (
	"testing"

	"github.com/ppxb/miyabi/internal/pan"
)

func TestSubtitleQueue_DeduplicationAndCapacity(t *testing.T) {
	// Create a queue with concurrency 0 (workers don't consume, so tasks stay buffered)
	// We instantiate SubtitleQueue directly for exact queue channel testing.
	q := &SubtitleQueue{
		tasks:   make(chan SubtitleTask, 2),
		pending: make(map[int]bool),
		service: &Service{},
		ctx:     t.Context(),
	}

	task1 := SubtitleTask{MovieID: 101, Code: "TEST-001"}
	task2 := SubtitleTask{MovieID: 102, Code: "TEST-002"}
	task3 := SubtitleTask{MovieID: 103, Code: "TEST-003"}

	// 1. Initial enqueue succeeds
	if !q.Enqueue(task1) {
		t.Fatal("expected task1 to be enqueued")
	}

	// 2. Duplicate enqueue for the same movie ID is rejected
	if q.Enqueue(task1) {
		t.Fatal("expected duplicate task1 to be rejected")
	}

	// 3. Second unique task fills the buffer (capacity 2)
	if !q.Enqueue(task2) {
		t.Fatal("expected task2 to be enqueued")
	}

	// 4. Third task exceeds capacity and is dropped cleanly
	if q.Enqueue(task3) {
		t.Fatal("expected task3 to be dropped due to full queue")
	}

	// Verify that dropped task was removed from pending map
	q.mu.Lock()
	if q.pending[103] {
		t.Fatal("dropped task 103 should not remain in pending map")
	}
	if !q.pending[101] || !q.pending[102] {
		t.Fatal("tasks 101 and 102 should be in pending map")
	}
	q.mu.Unlock()
}

func TestSubtitleQueue_GracefulShutdown(t *testing.T) {
	service := &Service{}
	q := newSubtitleQueue(service, 2, 8, nil)

	task := SubtitleTask{MovieID: 201, Code: "TEST-201"}
	if !q.Enqueue(task) {
		t.Fatal("expected task to be enqueued")
	}

	// Close waits for workers to drain and exit
	q.Close()

	// After close, enqueue must return false
	taskAfterClose := SubtitleTask{MovieID: 202, Code: "TEST-202"}
	if q.Enqueue(taskAfterClose) {
		t.Fatal("enqueue after close should return false")
	}
}

func TestIsUncensoredVideo(t *testing.T) {
	tests := []struct {
		name     string
		files    []pan.File
		expected bool
	}{
		{
			name:     "standard censored",
			files:    []pan.File{{Name: "ABP-001.mp4"}, {Name: "ABP-001.nfo"}},
			expected: false,
		},
		{
			name:     "contains uncensored keyword",
			files:    []pan.File{{Name: "ABP-001.Uncensored.mp4"}},
			expected: true,
		},
		{
			name:     "contains chinese uncensored keyword",
			files:    []pan.File{{Name: "ABP-001-无码破解.mkv"}},
			expected: true,
		},
		{
			name:     "empty list",
			files:    []pan.File{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUncensoredVideo(tt.files); got != tt.expected {
				t.Fatalf("isUncensoredVideo() = %v, expected %v", got, tt.expected)
			}
		})
	}
}
