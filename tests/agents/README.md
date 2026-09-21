# Agent Tests

Agent tests validate that each expert specification respects the common Axiom agent contract.

## Required coverage

- role identity is explicit;
- capabilities are declared;
- inputs and outputs are defined;
- forbidden responsibilities are represented;
- permissions are not implicitly granted by capabilities;
- quality gates are present;
- sensitive operations require authorization;
- context access follows least privilege.

Future executable tests should use the normative agent schema once M2.2 is implemented.
