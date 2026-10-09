// Package webhook implements the minimal GitHub webhook endpoint (#57 M3.1).
// It validates X-Hub-Signature-256 when AXIOM_WEBHOOK_SECRET is set,
// parses a push event, and logs the trigger point. Full pipeline
// integration (deployment lookup by repo + runner start) requires
// ApplicationID binding (#145) and agent-credential direction (#77)
// and is documented in docs/webhook-m3.1.md rather than forced here.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// Handler is the minimal webhook receiver.
type Handler struct {
	Log *slog.Logger
}

func New(log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{Log: log}
}

type pushPayload struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	After string `json:"after"`
	Ref   string `json:"ref"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	secret := os.Getenv("AXIOM_WEBHOOK_SECRET")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.Log.Error("webhook read body", "err", err)
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig == "" || !strings.HasPrefix(sig, "sha256=") {
			h.Log.Warn("webhook rejected: missing signature", "remote", r.RemoteAddr)
			http.Error(w, `{"error":"missing signature"}`, http.StatusForbidden)
			return
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(sig[len("sha256="):]), []byte(expected)) {
			h.Log.Warn("webhook rejected: bad signature", "remote", r.RemoteAddr)
			http.Error(w, `{"error":"invalid signature"}`, http.StatusForbidden)
			return
		}
	} else {
		h.Log.Warn("webhook received without AXIOM_WEBHOOK_SECRET; no HMAC enforced", "remote", r.RemoteAddr)
	}

	var p pushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		h.Log.Warn("webhook parse fail", "err", err)
	} else {
		h.Log.Info("webhook push received",
			"repo", p.Repository.FullName,
			"ref", p.Ref,
			"after", p.After,
			"remote", r.RemoteAddr,
		)
	}

	// Pipeline trigger point: the underlying repos.Service (repos.Service)
	// and ghauth.Service are wired in bootstrap (250-240). Triggering a
	// build requires resolving repo→connection→user and a deployment plan,
	// blocked by #145 (no ApplicationID in protocol) + agent auth #77.
	// This endpoint returns 202 so GitHub does not retry; operational
	// trigger is deferred until the mapping lands.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "accepted",
		"note":   "pipeline trigger deferred: repo→deployment mapping requires #145 + #77",
	})
}
