package server

import (
	"context"
	"time"

	"github.com/emdzej/acc/internal/transcript"
)

const (
	titlePollEvery = 5 * time.Second
	titleLookback  = 24 * time.Hour // ended sessions are re-checked this long after last activity
)

// pollTitles picks up session renames. Claude Code's /rename writes to the
// transcript without necessarily firing a hook, so the daemon looks itself.
func (s *Server) pollTitles(ctx context.Context) {
	scanned := map[string]bool{} // sessions that already had one full-file scan
	t := time.NewTicker(titlePollEvery)
	defer t.Stop()
	for {
		changed := false
		since := time.Now().Add(-titleLookback).UnixMilli()
		targets, err := s.store.TitleTargets(ctx, since)
		if err != nil && ctx.Err() == nil {
			s.log.Debug("title poll failed", "err", err)
		}
		for _, tg := range targets {
			custom, ai, err := transcript.Titles(tg.TranscriptPath, !scanned[tg.ID])
			if err != nil {
				continue
			}
			scanned[tg.ID] = true
			if ok, err := s.store.SetTitles(ctx, tg.ID, custom, ai); err == nil && ok {
				changed = true
			}
		}
		if changed {
			s.hub.publish("sessions")
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Start runs the server's background work until ctx is cancelled.
func (s *Server) Start(ctx context.Context) {
	go s.pollTitles(ctx)
}
