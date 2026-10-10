package org

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// PGStore is the PostgreSQL implementation of Store (issue #146).
type PGStore struct{ DB *sql.DB }

var _ Store = PGStore{}

// pgErrCode extracts the SQLSTATE so a unique violation is distinguishable from
// a check violation without string-matching driver messages.
func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func (s PGStore) Create(ctx context.Context, r Record) error {
	limits, err := json.Marshal(r.Limits)
	if err != nil {
		return fmt.Errorf("org: marshal limits: %w", err)
	}
	usage, err := json.Marshal(r.Usage)
	if err != nil {
		return fmt.Errorf("org: marshal usage: %w", err)
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO organizations (id, name, slug, plan, limits, usage, billing_customer_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,nullif($7,''),coalesce($8, now()), now())`,
		r.ID, r.Name, r.Slug, string(r.Plan), limits, usage, r.BillingCustomerID, nullTime(r.CreatedAt))
	if pgErrCode(err) == "23505" { // unique_violation
		return ErrAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("org: create organization: %w", err)
	}
	return nil
}

func (s PGStore) Get(ctx context.Context, id string) (Record, error) {
	return s.scanOne(ctx,
		`SELECT id, name, slug, plan, limits, usage, coalesce(billing_customer_id,''), created_at, updated_at
		 FROM organizations WHERE id = $1`, id)
}

func (s PGStore) GetBySlug(ctx context.Context, slug string) (Record, error) {
	return s.scanOne(ctx,
		`SELECT id, name, slug, plan, limits, usage, coalesce(billing_customer_id,''), created_at, updated_at
		 FROM organizations WHERE slug = $1`, slug)
}

func (s PGStore) scanOne(ctx context.Context, query string, arg any) (Record, error) {
	var (
		r         Record
		plan      string
		limitsRaw []byte
		usageRaw  []byte
	)
	err := s.DB.QueryRowContext(ctx, query, arg).Scan(
		&r.ID, &r.Name, &r.Slug, &plan, &limitsRaw, &usageRaw, &r.BillingCustomerID, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("org: scan organization: %w", err)
	}
	r.Plan = Plan(plan)
	if err := json.Unmarshal(limitsRaw, &r.Limits); err != nil {
		return Record{}, fmt.Errorf("org: decode limits: %w", err)
	}
	if err := json.Unmarshal(usageRaw, &r.Usage); err != nil {
		return Record{}, fmt.Errorf("org: decode usage: %w", err)
	}
	return r, nil
}

func (s PGStore) Update(ctx context.Context, r Record) error {
	limits, err := json.Marshal(r.Limits)
	if err != nil {
		return fmt.Errorf("org: marshal limits: %w", err)
	}
	usage, err := json.Marshal(r.Usage)
	if err != nil {
		return fmt.Errorf("org: marshal usage: %w", err)
	}
	tag, err := s.DB.ExecContext(ctx, `
		UPDATE organizations
		   SET name = $2, plan = $3, limits = $4, usage = $5,
		       billing_customer_id = nullif($6,'')
		 WHERE id = $1`,
		r.ID, r.Name, string(r.Plan), limits, usage, r.BillingCustomerID)
	if err != nil {
		return fmt.Errorf("org: update organization: %w", err)
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s PGStore) Delete(ctx context.Context, id string) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM organizations WHERE id = $1`, id); err != nil {
		return fmt.Errorf("org: delete organization: %w", err)
	}
	return nil
}

func (s PGStore) ListForUser(ctx context.Context, userID string) ([]Record, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT o.id, o.name, o.slug, o.plan, o.limits, o.usage,
		       coalesce(o.billing_customer_id,''), o.created_at, o.updated_at
		  FROM organizations o
		  JOIN organization_members m ON m.org_id = o.id
		 WHERE m.user_id = $1
		 ORDER BY o.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("org: list organizations: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var (
			r         Record
			plan      string
			limitsRaw []byte
			usageRaw  []byte
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &plan, &limitsRaw, &usageRaw,
			&r.BillingCustomerID, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("org: scan organization row: %w", err)
		}
		r.Plan = Plan(plan)
		if err := json.Unmarshal(limitsRaw, &r.Limits); err != nil {
			return nil, fmt.Errorf("org: decode limits: %w", err)
		}
		if err := json.Unmarshal(usageRaw, &r.Usage); err != nil {
			return nil, fmt.Errorf("org: decode usage: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("org: iterate organizations: %w", err)
	}
	return out, nil
}
func (s PGStore) AddMember(ctx context.Context, m Member) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO organization_members (org_id, user_id, role, invited_by, created_at)
		VALUES ($1,$2,$3,nullif($4,''),coalesce($5, now()))`,
		m.OrgID, m.UserID, string(m.Role), m.InvitedBy, nullTime(m.CreatedAt))
	if pgErrCode(err) == "23505" {
		return ErrAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("org: add member: %w", err)
	}
	return nil
}

func (s PGStore) GetMember(ctx context.Context, orgID, userID string) (Member, error) {
	var (
		m    Member
		role string
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT org_id, user_id, role, coalesce(invited_by,''), created_at
		  FROM organization_members WHERE org_id = $1 AND user_id = $2`,
		orgID, userID).Scan(&m.OrgID, &m.UserID, &role, &m.InvitedBy, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("org: get member: %w", err)
	}
	m.Role = Role(role)
	return m, nil
}

func (s PGStore) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT org_id, user_id, role, coalesce(invited_by,''), created_at
		  FROM organization_members WHERE org_id = $1
		 ORDER BY created_at`, orgID)
	if err != nil {
		return nil, fmt.Errorf("org: list members: %w", err)
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var (
			m    Member
			role string
		)
		if err := rows.Scan(&m.OrgID, &m.UserID, &role, &m.InvitedBy, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("org: scan member row: %w", err)
		}
		m.Role = Role(role)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("org: iterate members: %w", err)
	}
	return out, nil
}

func (s PGStore) UpdateMemberRole(ctx context.Context, orgID, userID string, role Role) error {
	tag, err := s.DB.ExecContext(ctx,
		`UPDATE organization_members SET role = $3 WHERE org_id = $1 AND user_id = $2`,
		orgID, userID, string(role))
	if err != nil {
		return fmt.Errorf("org: update member role: %w", err)
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s PGStore) RemoveMember(ctx context.Context, orgID, userID string) error {
	tag, err := s.DB.ExecContext(ctx,
		`DELETE FROM organization_members WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	if err != nil {
		return fmt.Errorf("org: remove member: %w", err)
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s PGStore) CountOwners(ctx context.Context, orgID string) (int, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM organization_members WHERE org_id = $1 AND role = 'owner'`,
		orgID).Scan(&n); err != nil {
		return 0, fmt.Errorf("org: count owners: %w", err)
	}
	return n, nil
}

func (s PGStore) CreateInvitation(ctx context.Context, inv Invitation) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO organization_invitations
			(id, org_id, email, role, token_hash, invited_by, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,nullif($6,''),$7,coalesce($8, now()))`,
		inv.ID, inv.OrgID, inv.Email, string(inv.Role), inv.TokenHash, inv.InvitedBy, inv.ExpiresAt, nullTime(inv.CreatedAt))
	if pgErrCode(err) == "23505" {
		return ErrAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("org: create invitation: %w", err)
	}
	return nil
}

func (s PGStore) GetInvitationByTokenHash(ctx context.Context, tokenHash string) (Invitation, error) {
	var (
		inv  Invitation
		role string
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, org_id, email, role, token_hash, coalesce(invited_by,''), expires_at, accepted_at, created_at
		  FROM organization_invitations WHERE token_hash = $1`, tokenHash).
		Scan(&inv.ID, &inv.OrgID, &inv.Email, &role, &inv.TokenHash, &inv.InvitedBy,
			&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("org: get invitation: %w", err)
	}
	inv.Role = Role(role)
	return inv, nil
}

func (s PGStore) ListInvitations(ctx context.Context, orgID string) ([]Invitation, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, org_id, email, role, token_hash, coalesce(invited_by,''), expires_at, accepted_at, created_at
		  FROM organization_invitations WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("org: list invitations: %w", err)
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		var (
			inv  Invitation
			role string
		)
		if err := rows.Scan(&inv.ID, &inv.OrgID, &inv.Email, &role, &inv.TokenHash, &inv.InvitedBy,
			&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt); err != nil {
			return nil, fmt.Errorf("org: scan invitation row: %w", err)
		}
		inv.Role = Role(role)
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("org: iterate invitations: %w", err)
	}
	return out, nil
}

func (s PGStore) AcceptInvitation(ctx context.Context, id, userID string) error {
	// Adding the membership and marking the invitation accepted happen in one
	// statement: a member row without a consumed invitation would let the same
	// link be replayed to grant the role twice.
	tag, err := s.DB.ExecContext(ctx, `
		WITH claimed AS (
			UPDATE organization_invitations
			   SET accepted_at = now()
			 WHERE id = $1 AND accepted_at IS NULL AND expires_at > now()
			RETURNING org_id, role, invited_by
		)
		INSERT INTO organization_members (org_id, user_id, role, invited_by)
		SELECT org_id, $2, role, invited_by FROM claimed
		ON CONFLICT (org_id, user_id) DO NOTHING`, id, userID)
	if err != nil {
		return fmt.Errorf("org: accept invitation: %w", err)
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s PGStore) DeleteInvitation(ctx context.Context, id string) error {
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM organization_invitations WHERE id = $1 AND accepted_at IS NULL`, id); err != nil {
		return fmt.Errorf("org: delete invitation: %w", err)
	}
	return nil
}

// nullTime converts the zero time into SQL NULL so a caller that does not set a
// timestamp gets the column default rather than year 1.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
