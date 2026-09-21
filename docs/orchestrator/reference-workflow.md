# Canonical Reference Workflow

The first deterministic workflow validates the separation between business expertise and Axiom technical expertise.

```text
Business Specification
        |
        v
Business Expert
        |
        v
Architecture
        |
   +----+----+
   |    |    |
   v    v    v
Backend Frontend Database
   \    |    /
    \   |   /
      Security
         |
         v
         QA
         |
         v
   DevOps / Infrastructure
         |
         v
    Runtime Agent
         |
         v
    Deployed System
```

## Required artifacts

| Stage | Output |
|---|---|
| Business Expert | Business Specification |
| Architect | Architecture Specification + ADRs |
| Backend | Backend Implementation |
| Frontend | Frontend Implementation |
| Database | Database Schema/Migrations |
| Security | Security Review |
| QA | Test Report |
| DevOps/Infrastructure | Deployment Plan |
| Runtime Agent | Runtime Execution Result |

Every transition is represented by validated artifacts and governed by task dependencies and quality gates.