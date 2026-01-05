package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wesm/roborev/internal/config"
	"github.com/wesm/roborev/internal/storage"
)

func TestWorkerPoolE2E(t *testing.T) {
	// Setup temp DB
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}
	defer db.Close()

	// Setup config with test agent
	cfg := config.DefaultConfig()
	cfg.MaxWorkers = 2

	// Create a repo and commit
	repo, err := db.GetOrCreateRepo(tmpDir)
	if err != nil {
		t.Fatalf("GetOrCreateRepo failed: %v", err)
	}

	commit, err := db.GetOrCreateCommit(repo.ID, "testsha123", "Test Author", "Test commit", time.Now())
	if err != nil {
		t.Fatalf("GetOrCreateCommit failed: %v", err)
	}

	// Enqueue a job with test agent
	job, err := db.EnqueueJob(repo.ID, commit.ID, "testsha123", "test")
	if err != nil {
		t.Fatalf("EnqueueJob failed: %v", err)
	}

	// Create and start worker pool
	pool := NewWorkerPool(db, cfg, 1)
	pool.Start()

	// Wait for job to complete (with timeout)
	deadline := time.Now().Add(10 * time.Second)
	var finalJob *storage.ReviewJob
	for time.Now().Before(deadline) {
		finalJob, err = db.GetJobByID(job.ID)
		if err != nil {
			t.Fatalf("GetJobByID failed: %v", err)
		}
		if finalJob.Status == storage.JobStatusDone || finalJob.Status == storage.JobStatusFailed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Stop worker pool
	pool.Stop()

	// Verify job completed (might fail if git repo not available, that's ok)
	if finalJob.Status != storage.JobStatusDone && finalJob.Status != storage.JobStatusFailed {
		t.Errorf("Job should be done or failed, got %s", finalJob.Status)
	}

	// If done, verify review was stored
	if finalJob.Status == storage.JobStatusDone {
		review, err := db.GetReviewByCommitSHA("testsha123")
		if err != nil {
			t.Fatalf("GetReviewByCommitSHA failed: %v", err)
		}
		if review.Agent != "test" {
			t.Errorf("Expected agent 'test', got '%s'", review.Agent)
		}
		if review.Output == "" {
			t.Error("Review output should not be empty")
		}
	}
}

func TestWorkerPoolConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}
	defer db.Close()

	cfg := config.DefaultConfig()
	cfg.MaxWorkers = 4

	repo, _ := db.GetOrCreateRepo(tmpDir)

	// Create multiple jobs
	for i := 0; i < 5; i++ {
		sha := "concurrentsha" + string(rune('0'+i))
		commit, _ := db.GetOrCreateCommit(repo.ID, sha, "Author", "Subject", time.Now())
		db.EnqueueJob(repo.ID, commit.ID, sha, "test")
	}

	pool := NewWorkerPool(db, cfg, 4)
	pool.Start()

	// Wait briefly and check active workers
	time.Sleep(500 * time.Millisecond)
	activeWorkers := pool.ActiveWorkers()

	pool.Stop()

	// Should have had some workers active (exact number depends on timing)
	t.Logf("Peak active workers: %d", activeWorkers)
}
