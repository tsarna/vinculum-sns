# Changelog

## Unreleased

## [0.2.0] - 2026-04-21

### Changed

- **Hook context optimization for per-message expression evaluation.** Introduced
  `HookContext`, `MakeHookContextFunc`, and `HookFunc` types. When multiple per-message
  expressions are configured (e.g. dynamic `sns_topic` + `subject` + `message_group_id`
  + `deduplication_id`), the evaluation context is now built once and shared across all
  hook evaluations within a single `OnEvent` call. Previously each expression built its
  own context independently.
  - `WithTopicFunc` renamed to `WithTopicHook`
  - `WithSubjectFunc` renamed to `WithSubjectHook`
  - `FIFOConfig.GroupIDFunc` renamed to `GroupIDHook`
  - `FIFOConfig.DeduplicationFunc` renamed to `DeduplicationHook`
  - New `WithMakeHookContext` builder method (optional; defaults to nil context for
    callers that provide self-contained hook closures)
