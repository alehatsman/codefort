package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
	"github.com/robfig/cron/v3"
)

const cronTickInterval = 30 * time.Second

// runCronScheduler fires pipeline schedule entries on their cron expressions.
// It ticks every 30 seconds (sub-minute to avoid skipping a window on jitter),
// reconciles each CI-enabled repo's cron_schedules rows against the pipeline
// declared at HEAD, then fires any entry whose next occurrence is due.
func runCronScheduler(ctx context.Context, db *sql.DB, cfg *config.Config, logger *slog.Logger) {
	ticker := time.NewTicker(cronTickInterval)
	defer ticker.Stop()

	log := logger.With("component", "cron-scheduler")
	log.Info("started")

	for {
		select {
		case <-ctx.Done():
			log.Info("stopped")
			return
		case now := <-ticker.C:
			tickCronScheduler(ctx, db, cfg, log, now)
		}
	}
}

func tickCronScheduler(ctx context.Context, db *sql.DB, cfg *config.Config, log *slog.Logger, now time.Time) {
	repos, err := storage.ListRepos(db)
	if err != nil {
		log.Error("list repos", "err", err)
		return
	}

	for _, repo := range repos {
		if !repo.CIEnabled {
			continue
		}
		if err := processRepoCron(ctx, db, cfg, log, repo, now); err != nil {
			log.Error("process repo cron", "repo", repo.Owner+"/"+repo.Name, "err", err)
		}
	}
}

func processRepoCron(ctx context.Context, db *sql.DB, cfg *config.Config, log *slog.Logger, repo storage.RepoSummary, now time.Time) error {
	bare := filepath.Join(cfg.ReposDir, repo.Owner, repo.Name+".git")

	headSHA, err := gitHeadSHA(ctx, bare)
	if err != nil {
		// Repo exists in DB but has no commits yet — skip silently.
		return nil
	}

	raw, ok, err := gitReadPipeline(ctx, bare, headSHA)
	if err != nil {
		return err
	}
	if !ok {
		// No mgitci.yml at HEAD — clear any stale schedules and move on.
		return storage.UpsertCronSchedules(db, repo.ID, nil)
	}

	pipeline, err := ci.Parse(raw)
	if err != nil {
		// Broken pipeline — don't touch schedules, don't fire.
		log.Warn("parse pipeline", "repo", repo.Owner+"/"+repo.Name, "err", err)
		return nil
	}

	exprs := make([]string, 0, len(pipeline.On.Schedule))
	for _, entry := range pipeline.On.Schedule {
		exprs = append(exprs, entry.Cron)
	}

	if err := storage.UpsertCronSchedules(db, repo.ID, exprs); err != nil {
		return err
	}

	if len(exprs) == 0 {
		return nil
	}

	schedules, err := storage.ListCronSchedules(db, repo.ID)
	if err != nil {
		return err
	}

	for _, s := range schedules {
		fired, err := maybeFire(ctx, db, cfg, log, repo, headSHA, s, now)
		if err != nil {
			log.Error("fire cron", "repo", repo.Owner+"/"+repo.Name, "expr", s.CronExpr, "err", err)
		} else if fired {
			log.Info("fired", "repo", repo.Owner+"/"+repo.Name, "expr", s.CronExpr)
		}
	}
	return nil
}

// maybeFire checks whether the schedule entry is due and, if so, enqueues a run.
func maybeFire(ctx context.Context, db *sql.DB, cfg *config.Config, log *slog.Logger, repo storage.RepoSummary, headSHA string, s storage.CronSchedule, now time.Time) (bool, error) {
	sched, err := cron.ParseStandard(s.CronExpr)
	if err != nil {
		log.Warn("invalid cron expr", "expr", s.CronExpr, "err", err)
		return false, nil
	}

	var from time.Time
	if s.LastFiredAt != nil {
		from = *s.LastFiredAt
	} else {
		// Never fired: treat as if it last fired one tick ago so it fires on
		// the first occurrence at or before now, not on the very first tick.
		from = now.Add(-cronTickInterval)
	}

	next := sched.Next(from)
	if next.After(now) {
		return false, nil // not yet due
	}

	commitMsg, commitAuthor, err := gitHeadCommitInfo(ctx, filepath.Join(cfg.ReposDir, repo.Owner, repo.Name+".git"), headSHA)
	if err != nil {
		commitMsg = ""
		commitAuthor = ""
	}

	if _, err := storage.EnqueueRun(db, repo.ID, storage.NewRun{
		CommitSHA:    headSHA,
		CommitMsg:    commitMsg,
		CommitAuthor: commitAuthor,
		Ref:          "refs/heads/HEAD", // symbolic; HEAD resolves the branch
		Event:        "schedule",
		Trigger:      s.CronExpr,
	}); err != nil {
		return false, err
	}

	return true, storage.UpdateCronLastFired(db, s.ID, now)
}

// gitHeadSHA resolves HEAD to a commit SHA in a bare repo.
func gitHeadSHA(ctx context.Context, bare string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "--git-dir", bare, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitHeadCommitInfo returns the subject and author of a commit.
func gitHeadCommitInfo(ctx context.Context, bare, sha string) (msg, author string, err error) {
	out, err := exec.CommandContext(ctx, "git", "--git-dir", bare, "log", "-1", "--format=%s\x00%an", sha).Output()
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\x00", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	return string(out), "", nil
}
