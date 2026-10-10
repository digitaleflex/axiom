package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/org"
)

// Organizations manages tenant organizations, memberships and invitations
// (#146, M12.1). It is satisfied by *org.Service.
type Organizations interface {
	Create(ctx context.Context, in org.CreateInput) (org.Record, error)
	Get(ctx context.Context, orgID, requesterID string) (org.Record, error)
	ListForUser(ctx context.Context, userID string) ([]org.Record, error)
	Update(ctx context.Context, orgID, requesterID string, in org.UpdateInput) (org.Record, error)
	Delete(ctx context.Context, orgID, requesterID string) error
	ListMembers(ctx context.Context, orgID, requesterID string) ([]org.Member, error)
	ChangeRole(ctx context.Context, orgID, requesterID, targetID string, role org.Role) error
	RemoveMember(ctx context.Context, orgID, requesterID, targetID string) error
	Invite(ctx context.Context, in org.InviteInput) (org.PendingInvite, error)
	AcceptInvitation(ctx context.Context, token, userID string) (org.Record, error)
	ListInvitations(ctx context.Context, orgID, requesterID string) ([]org.Invitation, error)
	RevokeInvitation(ctx context.Context, orgID, requesterID, invitationID string) error
}

// memberDTO is the membership projection returned to clients. It carries no
// secret: a member row is exactly a role.
type memberDTO struct {
	UserID    string `json:"userId"`
	Role      string `json:"role"`
	InvitedBy string `json:"invitedBy,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func toMemberDTO(m org.Member) memberDTO {
	return memberDTO{
		UserID:    m.UserID,
		Role:      string(m.Role),
		InvitedBy: m.InvitedBy,
		CreatedAt: m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// invitationDTO is the invitation projection. The token hash is never returned;
// the plaintext token is available exactly once, from the invite response.
type invitationDTO struct {
	ID         string `json:"id"`
	OrgID      string `json:"orgId"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expiresAt"`
	InvitedBy  string `json:"invitedBy,omitempty"`
	AcceptedAt string `json:"acceptedAt,omitempty"`
}

func toInvitationDTO(i org.Invitation) invitationDTO {
	out := invitationDTO{
		ID:        i.ID,
		OrgID:     i.OrgID,
		Email:     i.Email,
		Role:      string(i.Role),
		ExpiresAt: i.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		InvitedBy: i.InvitedBy,
	}
	switch {
	case i.Accepted():
		out.Status = "accepted"
	case i.Expired(time.Now().UTC()):
		out.Status = "expired"
	default:
		out.Status = "pending"
	}
	if i.AcceptedAt != nil {
		out.AcceptedAt = i.AcceptedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return out
}

// listOrganizations returns every organization the caller belongs to. This is
// the entry point after sign-in: it answers "what can I work in" without the
// client needing to know an organization id.
func (a *API) listOrganizations(w http.ResponseWriter, r *http.Request) error {
	if a.orgs == nil {
		return errUnavailable
	}
	items, err := a.orgs.ListForUser(r.Context(), principal(r.Context()).UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// createOrganization registers a new tenant owned by the caller.
func (a *API) createOrganization(w http.ResponseWriter, r *http.Request) (err error) {
	defer func() { a.audit(r, "organization.create", "self", principal(r.Context()).UserID, err) }()
	if a.orgs == nil {
		return errUnavailable
	}
	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		Plan string `json:"plan"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	rec, err := a.orgs.Create(r.Context(), org.CreateInput{
		OwnerID: principal(r.Context()).UserID,
		Name:    in.Name,
		Slug:    in.Slug,
		Plan:    org.Plan(in.Plan),
	})
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/api/v1/orgs/"+rec.ID)
	writeJSON(w, http.StatusCreated, rec)
	return nil
}

func (a *API) getOrganization(w http.ResponseWriter, r *http.Request) error {
	if a.orgs == nil {
		return errUnavailable
	}
	rec, err := a.orgs.Get(r.Context(), r.PathValue("orgID"), principal(r.Context()).UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

func (a *API) updateOrganization(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("orgID")
	defer func() { a.audit(r, "organization.update", target, principal(r.Context()).UserID, err) }()
	if a.orgs == nil {
		return errUnavailable
	}
	var in struct {
		Name string `json:"name"`
		Plan string `json:"plan"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	rec, err := a.orgs.Update(r.Context(), target, principal(r.Context()).UserID,
		org.UpdateInput{Name: in.Name, Plan: org.Plan(in.Plan)})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

func (a *API) deleteOrganization(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("orgID")
	defer func() { a.audit(r, "organization.delete", target, principal(r.Context()).UserID, err) }()
	if a.orgs == nil {
		return errUnavailable
	}
	if err := a.orgs.Delete(r.Context(), target, principal(r.Context()).UserID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) listOrganizationMembers(w http.ResponseWriter, r *http.Request) error {
	if a.orgs == nil {
		return errUnavailable
	}
	members, err := a.orgs.ListMembers(r.Context(), r.PathValue("orgID"), principal(r.Context()).UserID)
	if err != nil {
		return err
	}
	out := make([]memberDTO, 0, len(members))
	for _, m := range members {
		out = append(out, toMemberDTO(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
	return nil
}

func (a *API) changeMemberRole(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("orgID") + "/" + r.PathValue("userID")
	defer func() { a.audit(r, "organization.role_change", target, principal(r.Context()).UserID, err) }()
	if a.orgs == nil {
		return errUnavailable
	}
	var in struct {
		Role string `json:"role"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if err := a.orgs.ChangeRole(r.Context(), r.PathValue("orgID"), principal(r.Context()).UserID,
		r.PathValue("userID"), org.Role(in.Role)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) removeOrganizationMember(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("orgID") + "/" + r.PathValue("userID")
	defer func() { a.audit(r, "organization.member_remove", target, principal(r.Context()).UserID, err) }()
	if a.orgs == nil {
		return errUnavailable
	}
	if err := a.orgs.RemoveMember(r.Context(), r.PathValue("orgID"), principal(r.Context()).UserID,
		r.PathValue("userID")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// inviteOrganizationMember creates an invitation.
//
// The plaintext token is returned in the response body and nowhere else: it is
// not persisted, so this response is the only chance to build the link. The
// client is expected to deliver it by email and discard it.
func (a *API) inviteOrganizationMember(w http.ResponseWriter, r *http.Request) (err error) {
	orgID := r.PathValue("orgID")
	defer func() { a.audit(r, "organization.invite", orgID, principal(r.Context()).UserID, err) }()
	if a.orgs == nil {
		return errUnavailable
	}
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	inv, err := a.orgs.Invite(r.Context(), org.InviteInput{
		OrgID:     orgID,
		InviterID: principal(r.Context()).UserID,
		Email:     in.Email,
		Role:      org.Role(in.Role),
	})
	if err != nil {
		return err
	}
	dto := toInvitationDTO(inv.Invitation)
	writeJSON(w, http.StatusCreated, map[string]any{
		"invitation": dto,
		// token is returned exactly once; the store keeps only its hash.
		"token": inv.Token,
	})
	return nil
}

func (a *API) listOrganizationInvitations(w http.ResponseWriter, r *http.Request) error {
	if a.orgs == nil {
		return errUnavailable
	}
	invs, err := a.orgs.ListInvitations(r.Context(), r.PathValue("orgID"), principal(r.Context()).UserID)
	if err != nil {
		return err
	}
	out := make([]invitationDTO, 0, len(invs))
	for _, i := range invs {
		out = append(out, toInvitationDTO(i))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
	return nil
}

func (a *API) revokeOrganizationInvitation(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("orgID") + "/" + r.PathValue("invitationID")
	defer func() { a.audit(r, "organization.invite_revoke", target, principal(r.Context()).UserID, err) }()
	if a.orgs == nil {
		return errUnavailable
	}
	if err := a.orgs.RevokeInvitation(r.Context(), r.PathValue("orgID"), principal(r.Context()).UserID,
		r.PathValue("invitationID")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// acceptInvitation redeems an invitation token for the signed-in user.
//
// The token arrives in the request body rather than the path: a token in a URL
// ends up in browser history, proxy logs and Referer headers. A replayed or
// expired token is answered 404, not 403, so the endpoint cannot be used to test
// whether a link is still valid.
func (a *API) acceptInvitation(w http.ResponseWriter, r *http.Request) (err error) {
	defer func() { a.audit(r, "organization.invite_accept", "self", principal(r.Context()).UserID, err) }()
	if a.orgs == nil {
		return errUnavailable
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	rec, err := a.orgs.AcceptInvitation(r.Context(), in.Token, principal(r.Context()).UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

// fromOrgErr maps organization errors onto the API error envelope. A caller who
// is not a member is told the resource does not exist: answering 403 would
// confirm the organization is real to somebody who has no access to it.
func fromOrgErr(err error) *Error {
	switch {
	case errors.Is(err, org.ErrNotFound):
		return errNotFound("organization", "")
	case errors.Is(err, org.ErrNotAMember):
		return errNotFound("organization", "")
	case errors.Is(err, org.ErrAlreadyExists):
		return newError(http.StatusConflict, CodeConflict, "an organization with this slug already exists", nil)
	case errors.Is(err, org.ErrInvalidSlug):
		return errValidation("invalid organization", map[string]any{"fields": map[string]any{
			"slug": "must be 1-63 lowercase letters, digits or hyphens, starting and ending alphanumeric"}})
	case errors.Is(err, org.ErrInvalidName):
		return errValidation("invalid organization", map[string]any{"fields": map[string]any{
			"name": "must be 1-120 characters"}})
	case errors.Is(err, org.ErrInvalidEmail):
		return errValidation("invalid invitation", map[string]any{"fields": map[string]any{
			"email": "must be a valid email address"}})
	case errors.Is(err, org.ErrRoleNotInvitable):
		return errValidation("invalid role", map[string]any{"fields": map[string]any{
			"role": "must be admin, developer or viewer; ownership is granted separately"}})
	case errors.Is(err, org.ErrQuotaExceeded):
		return newError(http.StatusConflict, CodeConflict,
			"the plan's allowance for this organization is spent", map[string]any{"reason": "quota_exceeded"})
	case errors.Is(err, org.ErrLastOwner):
		return newError(http.StatusConflict, CodeConflict,
			"an organization must keep at least one owner", map[string]any{"reason": "last_owner"})
	case errors.Is(err, org.ErrCannotRemoveOwner):
		return newError(http.StatusForbidden, CodeForbidden,
			"only an owner may change an owner's role", map[string]any{"reason": "owner_required"})
	case errors.Is(err, org.ErrUnknownPlan):
		return errValidation("invalid plan", map[string]any{"fields": map[string]any{
			"plan": "must be free, starter, pro or enterprise"}})
	default:
		return fromDomain(err)
	}
}
