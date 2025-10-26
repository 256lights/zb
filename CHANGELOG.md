# zb Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

[Unreleased]: https://github.com/256lights/zb/compare/v0.1.0...main

## [Unreleased][]

### Added

- New `readFile` function
  ([#148](https://github.com/256lights/zb/issues/148)).
  Thank you to [@winterqt](https://github.com/winterqt)!

### Fixed

- `zb store object delete` is no longer flaky
  ([#135](https://github.com/256lights/zb/issues/135)).
- Lua operator metamethods now receive their arguments in the correct order
  when one of the operands is a constant
  ([#152](https://github.com/256lights/zb/issues/152)).
- Updated to Go 1.25.2.

## [0.1.0][] - 2025-06-15

Initial public release.
Special thanks to [@ocurr](https://github.com/ocurr) for early tester feedback
and to [@ejrichards](https://github.com/ejrichards) for NixOS support!

[0.1.0]: https://github.com/256lights/zb/releases/tag/v0.1.0
