# Integration Tests

Integration tests will validate interactions between Axiom components once executable components exist.

Initial scenarios:

1. project manifest → platform initialization;
2. business artifact → orchestrator task creation;
3. task → expert assignment;
4. expert output → artifact validation;
5. quality gate → accepted/revision flow;
6. approved task → runtime operation request;
7. runtime result → orchestrator state update;
8. audit trail correlation across the workflow.

No test in this directory should assume that the current documentation-only foundation is already executable.
