# GC-091 - Developer onboarding

- **Epic:** J - Docs
- **Type:** Docs
- **Priority:** Low
- **Status:** TODO
- **Estimate:** 0.5 day
- **Depends on:** GC-003
- **Blocks:** -

## Context

- Architecture + interfaces: [docs/01-architecture.md](../01-architecture.md),
  [docs/06-driver-interface.md](../06-driver-interface.md).

## Description

Write a `CONTRIBUTING.md` / developer guide so new contributors can build, test,
and extend the project (e.g. add a new transport).

## Tasks

- Document: prerequisites, `make build/test/lint`, repo layout (link
  [01](../01-architecture.md) §2), CI expectations.
- "How to add a transport" walkthrough: implement `Transport`, register in the
  factory (GC-030), add golden vectors + a mock (GC-082), wire config.
- Coding conventions, commit/PR guidance, and the compatibility rule
  ("C# source is authoritative").

## Acceptance criteria

- A developer can build and test from a clean checkout following the guide.
- The "add a transport" steps are concrete and reference the relevant tickets.

## Test notes

- Doc-only; sanity-check the build/test commands actually work.
