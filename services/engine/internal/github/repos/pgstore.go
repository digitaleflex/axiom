package repos

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// PGStore persists repository identity in the repositories table.
type PGStore struct{ DB *sql.DB }

func (p PGStore) Upsert(ctx context.Context, connectionID string, items []Repository) ([]Repository, error) {
	out := make([]Repository, 0, len(items))
	for _, r := range items {
		id := "repo_" + randomHex(12)
		err := p.DB.QueryRowContext(ctx, `INSERT INTO repositories
				(id, connection_id, external_id, full_name, clone_url, default_branch, private, html_url, language, pushed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (connection_id, external_id) DO UPDATE SET
				full_name = EXCLUDED.full_name, clone_url = EXCLUDED.clone_url, default_branch = EXCLUDED.default_branch,
				private = EXCLUDED.private, html_url = EXCLUDED.html_url, language = EXCLUDED.language,
				pushed_at = EXCLUDED.pushed_at, updated_at = now()
			RETURNING id`, id, connectionID, r.ExternalID, r.FullName, r.CloneURL, r.DefaultBranch, r.Private, r.HTMLURL, r.Language, r.PushedAt).Scan(&r.ID)
		if err != nil {
			return nil, fmt.Errorf("upsert repository %s: %w", r.FullName, err)
		}
		r.ConnectionID = connectionID
		out = append(out, r)
	}
	return out, nil
}

func (p PGStore) Get(ctx context.Context, userID, repoID string) (Repository, error) {
	var r Repository
	var pushed sql.NullTime
	err := p.DB.QueryRowContext(ctx, `SELECT r.id, r.connection_id, r.external_id, r.full_name, r.clone_url, r.html_url,
			r.default_branch, r.private, r.language, r.pushed_at
		FROM repositories r JOIN github_connections c ON c.id = r.connection_id
		WHERE r.id = $1 AND c.user_id = $2`, repoID, userID).
		Scan(&r.ID, &r.ConnectionID, &r.ExternalID, &r.FullName, &r.CloneURL, &r.HTMLURL, &r.DefaultBranch, &r.Private, &r.Language, &pushed)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	if err != nil {
		return Repository{}, err
	}
	if pushed.Valid {
		t := pushed.Time.UTC()
		r.PushedAt = &t
	}
	return r, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
