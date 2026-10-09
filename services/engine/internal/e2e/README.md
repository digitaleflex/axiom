# Engine End-to-End Test (#68) — V1 (protocol-aware, agent-transport still in-process)

`e2e_test.go` proves the V0.1/V1 deployment path with a real reference
application (static page served from Dockerfile):
snapshot → analysis → profile → server → plan → real image build →
real container run → real health probe → LIVE, timeline replayed via SSE.

The engine side is fully mounted (bootstrap, agentclient, agentauth,
agentkey/signing, protocol ApplicationID + HMAC AD-0008). The agent
listener (loopback + TLS optionnel) and bridge (`WithScope`, distinct
ApplicationID / DeploymentID labels, #145 regression covered in
agent/bootstrap/bridge_test.go) are also mounted. What is NOT exercised
here is the end-to-end protocol handshake through the listener: the test
uses an in-process `dockerAgent` rather than a real `agent/bootstrap`
listener with `protocol` encoding and HMAC verification. That integration
requires starting the listener, registering the agent, and rotating the
operation-signing key inside the test process, which exceeds a single-tour
scope and touches agent/bootstrap beyond the engine-only fixture.

Needs `AXIOM_TEST_DATABASE_URL`, Docker (`AXIOM_TEST_DOCKER=1`) and
network for the fixture base image (`AXIOM_TEST_E2E=1`); otherwise skipped.
Runs in CI on the engine job. No SSH, GitHub, or Traefik involved:
source is a fixture, agent transport is in-process, routing is recorded
(Traefik arrives in #84; real agent protocol in #75/#80; listener + TLS
in agent/bootstrap, not in this fixture).
