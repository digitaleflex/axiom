# Axiom Architecture Overview

```text
Business Domain
      |
      v
Business Expert
      |
      v
Axiom Orchestrator
      |
      +--> Architect
      +--> Backend
      +--> Frontend
      +--> Database
      +--> Security
      +--> QA
      +--> DevOps
      +--> Infrastructure
      +--> Observability
      +--> Documentation
      |
      v
Axiom Platform
      |
      v
Runtime Agent
      |
      v
Infrastructure / Runtime
```

The orchestrator coordinates work; it does not replace expert responsibility. The runtime agent executes bounded operational actions; it is not the orchestration brain.
