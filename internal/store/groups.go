package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/emdzej/acc/internal/model"
)

// tree is the whole group hierarchy, loaded at once: it is small and every
// caller needs paths or inherited colours.
type tree struct {
	byID     map[int64]*model.Group
	children map[int64][]int64
	ordered  []*model.Group // depth-first, siblings by name
}

func (s *Store) loadTree(ctx context.Context) (*tree, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, parent_id, name, color FROM groups`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	t := &tree{byID: map[int64]*model.Group{}, children: map[int64][]int64{}}
	for rows.Next() {
		g := &model.Group{}
		if err := rows.Scan(&g.ID, &g.ParentID, &g.Name, &g.Color); err != nil {
			return nil, err
		}
		t.byID[g.ID] = g
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, g := range t.byID {
		parent := g.ParentID
		if _, ok := t.byID[parent]; !ok {
			parent = 0 // orphan: treat as root rather than hide it
		}
		t.children[parent] = append(t.children[parent], id)
	}
	for _, ids := range t.children {
		sort.Slice(ids, func(i, j int) bool { return t.byID[ids[i]].Name < t.byID[ids[j]].Name })
	}
	var walk func(parent int64, path, color string, depth int)
	walk = func(parent int64, path, color string, depth int) {
		for _, id := range t.children[parent] {
			g := t.byID[id]
			g.Path, g.Depth, g.Display = g.Name, depth, g.Color
			if path != "" {
				g.Path = path + "/" + g.Name
			}
			if g.Display == "" {
				g.Display = color
			}
			t.ordered = append(t.ordered, g)
			walk(id, g.Path, g.Display, depth+1)
		}
	}
	walk(0, "", "", 0)
	return t, nil
}

func (t *tree) find(path string) (*model.Group, error) {
	path = cleanPath(path)
	for _, g := range t.ordered {
		if g.Path == path {
			return g, nil
		}
	}
	return nil, fmt.Errorf("group %q: %w", path, ErrNotFound)
}

// subtree returns id and all its descendants.
func (t *tree) subtree(id int64) []int64 {
	out := []int64{id}
	for i := 0; i < len(out); i++ {
		out = append(out, t.children[out[i]]...)
	}
	return out
}

func cleanPath(p string) string {
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "/")
}

// ListGroups returns the tree depth-first with paths and inherited colours.
func (s *Store) ListGroups(ctx context.Context) ([]model.Group, error) {
	t, err := s.loadTree(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Group, len(t.ordered))
	for i, g := range t.ordered {
		out[i] = *g
	}
	return out, nil
}

// UpsertGroup creates the group at path (and any missing ancestors, with
// inherited colour) or sets its colour. color "" means inherit from the parent.
func (s *Store) UpsertGroup(ctx context.Context, path, color string) (model.Group, error) {
	path = cleanPath(path)
	if path == "" {
		return model.Group{}, errors.New("group name required")
	}
	if color != "" && !colorRe.MatchString(color) {
		return model.Group{}, fmt.Errorf("invalid colour %q, want #rrggbb", color)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Group{}, err
	}
	defer tx.Rollback()
	parts := strings.Split(path, "/")
	var parent int64
	for i, name := range parts {
		leaf := i == len(parts)-1
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM groups WHERE parent_id = ? AND name = ?`, parent, name).Scan(&id)
		switch {
		case err == nil && leaf:
			_, err = tx.ExecContext(ctx, `UPDATE groups SET color = ? WHERE id = ?`, strings.ToLower(color), id)
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			c := ""
			if leaf {
				c = strings.ToLower(color)
			}
			err = tx.QueryRowContext(ctx, `INSERT INTO groups (parent_id, name, color) VALUES (?, ?, ?) RETURNING id`, parent, name, c).Scan(&id)
		}
		if err != nil {
			return model.Group{}, err
		}
		parent = id
	}
	if err := tx.Commit(); err != nil {
		return model.Group{}, err
	}
	t, err := s.loadTree(ctx)
	if err != nil {
		return model.Group{}, err
	}
	return *t.byID[parent], nil
}

// DeleteGroup removes the group and its whole subtree; memberships cascade.
func (s *Store) DeleteGroup(ctx context.Context, path string) error {
	t, err := s.loadTree(ctx)
	if err != nil {
		return err
	}
	g, err := t.find(path)
	if err != nil {
		return err
	}
	ids := t.subtree(g.ID)
	_, err = s.db.ExecContext(ctx, `DELETE FROM groups WHERE id IN (`+placeholders(len(ids))+`)`, anySlice(ids)...)
	return err
}

func (s *Store) Assign(ctx context.Context, sessionID, path string, remove bool) error {
	t, err := s.loadTree(ctx)
	if err != nil {
		return err
	}
	g, err := t.find(path)
	if err != nil {
		return err
	}
	if remove {
		_, err := s.db.ExecContext(ctx, `DELETE FROM session_groups WHERE session_id = ? AND group_id = ?`, sessionID, g.ID)
		return err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO session_groups (session_id, group_id) VALUES (?, ?)`, sessionID, g.ID)
	return err
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func anySlice[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}
