# Axiom Glossary

- **Platform** — reusable technical foundation maintained by Axiom.
- **Domain Project** — concrete product containing business-specific logic.
- **Expert** — specialized role responsible for a defined decision or artifact.
- **Agent** — executable implementation of an expert role.
- **Orchestrator** — coordination layer that routes tasks and artifacts between experts.
- **Artifact** — versioned output exchanged between stages.
- **Contract** — machine-readable agreement defining inputs, outputs and constraints.
- **Job** — executable unit of work requested from the system.
- **Task** — bounded piece of a job assigned to an expert.
- **Capability** — operation an agent is authorized and able to perform.
- **Runtime** — environment where the application and operational components execute.
- **Adapter** — integration translating Axiom contracts to an external provider or tool.
- **Provider** — external infrastructure, model or service consumed through an adapter.
- **Environment** — isolated project execution context such as development, staging or production.
- **Project Manifest** — `axiom.yaml` declaration of project requirements and enabled capabilities.
- **Quality Gate** — validation required before an artifact or task can advance.
- **Human Approval** — explicit human authorization required for designated decisions or operations.
