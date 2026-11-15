# Changelog

All notable changes of `multi-tour-worker-cash-close` (Automatic cash close worker) are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [3.0.0] - 2026-11-14

Cut 3 release. Every entry below is a pull request merged into `qa` and re-applied to `release/3.0.0` with `git cherry-pick -x`; the heading of each group is the user story named in the pull request, and the entry is the title of the pull request without its type. The frozen head of `qa` is `3e74fdcbd2cdf92815381223bdac8494b5c6615d`.

### Changed

#### HU-WRK-008 - Close the automatic cash sessions

- The module and configuration of job 8 (part 1) ([PR 5](https://github.com/code-corhuila/multi-tour-worker-cash-close/pull/5))
- The job and its adapters of job 8 (part 2) ([PR 6](https://github.com/code-corhuila/multi-tour-worker-cash-close/pull/6))
- The AC-WK-08.5 quiet run test ([PR 8](https://github.com/code-corhuila/multi-tour-worker-cash-close/pull/8))
- The checks on pull requests to main ([PR 11](https://github.com/code-corhuila/multi-tour-worker-cash-close/pull/11))
- The HU-WRK-008 tests ([PR 12](https://github.com/code-corhuila/multi-tour-worker-cash-close/pull/12))
- Word the 422 refusal test after the PDR, not after movements ([PR 16](https://github.com/code-corhuila/multi-tour-worker-cash-close/pull/16))
- Run the test workflow only on pull requests to develop and qa ([PR 18](https://github.com/code-corhuila/multi-tour-worker-cash-close/pull/18))

### Fixed

#### HU-WRK-008 - Close the automatic cash sessions

- Rotate the pages of the open list so alerted boxes do not hide the automatic ones ([PR 15](https://github.com/code-corhuila/multi-tour-worker-cash-close/pull/15))

[Unreleased]: https://github.com/code-corhuila/multi-tour-worker-cash-close/compare/v3.0.0...HEAD
[3.0.0]: https://github.com/code-corhuila/multi-tour-worker-cash-close/releases/tag/v3.0.0
