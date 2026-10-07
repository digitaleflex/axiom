# Engine End-to-End Test (#68)

`e2e_test.go` proves the V0.1 path with a real reference application
(a static page served from a Dockerfile build):

snapshot → analysis → profile → server → plan → real image build →
real container run → real health probe → LIVE, with the event timeline
replayed through SSE.

Needs `AXIOM_TEST_DATABASE_URL`, Docker (`AXIOM_TEST_DOCKER=1`) and
network for the fixture base image (`AXIOM_TEST_E2E=1`); otherwise skipped.
Runs in CI on the engine job. No SSH, GitHub, or Traefik involved:
source is a fixture, agent transport is in-process, routing is recorded
(Traefik arrives in #84; the real agent protocol in #75/#80).
