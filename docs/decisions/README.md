# Decision records

Short records of the choices that shape SimplyCubed Code. Each states the
context, the decision, its status, and the consequences.

Some records are marked **pending S3**. S3 is the replay validation spike:
reverting known-good commits in a real repository with a real gate and checking
whether the model, driven through the loop, reproduces them. Anything whose
correctness depends on how a specific model behaves (prompt wording, the reliable
shape of structured output, budget and stall numbers) is written here as a
decision but is deliberately **not yet coded**, because a spike can revise a
document cheaply and cannot cheaply revise code written against a wrong guess.

| ADR | Title | Status |
| --- | --- | --- |
| 0001 | Engine-independent core | Accepted, implemented |
| 0002 | No merge path in the binary | Accepted, implemented |
| 0003 | Codex on Azure as the first engine adapter | Accepted; adapter pending S3 |
| 0004 | Role prompts and the reviewer verdict | Pending S3 |
| 0005 | Budgets and stall thresholds | Pending S3 |
| 0006 | Runtime on GitHub Actions, and identity | Accepted; one-vs-two-App open |
