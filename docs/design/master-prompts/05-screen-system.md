# Master Prompt 05 — Axiom Screen System (Visual Generation)

Using the approved Axiom Product UI Brief, UX & Information Architecture, and Design DNA, generate the complete Axiom application screen family as **actual high-fidelity UI mockups**, not as documentation.

## Critical instruction

The output must be visual interface design. Do **not** return a textual UX specification, architecture document, wireframe description, or essay.

If the tool supports visual canvas generation, create the screens directly on the canvas. If it supports multiple frames, create one frame per screen. If it supports only one frame, generate the requested screen from the dedicated screen prompt files in the screen-prompts directory.

## Product context

Axiom is a developer-first deployment and cloud application hosting platform. The sacred pipeline is:

GitHub → Repository → Repository Analysis → Stack Detection → Application Profile → Deployment Plan → Build → Runtime → Networking → Domain / SSL → Health Check → LIVE.

The interface must communicate infrastructure state, deployment progress, logs, health, domains, and operational control without hiding important technical information.

## Visual requirements

- Use the approved Design DNA as immutable source of truth.
- Use the approved UX architecture for navigation and information hierarchy.
- Use the Product UI Brief for product behavior and terminology.
- Preserve Axiom's high-density developer ergonomics.
- Keep Production / Staging / Preview visibly anchored in global chrome where relevant.
- Use the semantic status system consistently: Emerald = Live, Amber = Building/Probing, Crimson = Failure/Crash, Blue = Preview/Queued, Slate = Superseded/Inactive.
- Use terminal/monospace treatment for technical identifiers, logs, commands, SHAs, container IDs, IPs and timestamps where appropriate.
- Show realistic but clearly synthetic data.
- Design responsive desktop-first SaaS interfaces suitable for a developer/DevOps audience.
- Avoid generic dashboard filler, decorative charts without meaning, stock imagery, and unrelated marketing sections.
- Do not imitate another product's interface.

## Screen family

01 Dashboard
02 GitHub Connection
03 Repository List
04 Repository Detail
05 Repository Analysis
06 Application Profile
07 Server Selection
08 Deployment Configuration
09 Deployment Plan
10 Deployment Progress
11 Deployment Success
12 Deployment Failure
13 Application Overview
14 Application Deployments
15 Application Logs
16 Application Metrics
17 Application Domains
18 Server Overview
19 Server Details
20 Settings

For every screen, design the primary/default state and the most important operational states. Do not create twenty unrelated visual concepts: they must clearly belong to one Axiom product.

## Output

Generate **UI screens**, not prose. Each screen must visibly expose:

- page title and context
- global navigation
- primary action
- relevant data
- meaningful status
- technical detail appropriate to the task
- feedback/state where relevant
- responsive structure

The dedicated prompts in the screen-prompts directory are the canonical instructions for generating each screen individually.