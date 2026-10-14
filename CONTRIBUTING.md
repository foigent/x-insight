# Contributing to X-Insight

[简体中文](CONTRIBUTING.zh-CN.md)

Start with a concrete problem, a reproducible example, or a proposed user outcome. Small fixes and documentation corrections can go directly to a pull request. Discuss changes to source contracts, authorization, database migrations, release trust, or supported installation behavior before implementing a broad redesign.

## Working on a change

1. Read the [getting-started guide](docs/community/getting-started.md) and relevant paths in the [contribution map](docs/community/contribution-map.md).
2. Work on a focused branch. Preserve unrelated changes and use synthetic, minimized fixtures. Never include tokens, account exports, raw customer content, private target addresses, or local operations records.
3. Run checks appropriate to the affected paths. Backend, schema, authorization, and installer changes require real database or installed-host evidence where applicable; a skipped test is not a pass. Frontend changes require type checking, coverage, a build, and the relevant browser scenarios.
4. Describe the problem, resulting behavior, validation, compatibility impact, and remaining limits. Do not present local validation as CI, Target, external integration, or production acceptance.

Canonical documentation uses English filenames without a language suffix. Simplified Chinese translations use `.zh-CN.md`, link to the canonical page, and identify the source version. Language-specific embedded manuals have `en-US` and `zh-CN` directories. If a translation falls behind, mark it as outdated and point to the canonical document.

Chinese issues, reviews, and discussion are welcome. Record substantial decisions in a brief English summary so others can follow them. English fluency is not a contribution gate.

## Commit sign-off

Contributions require the [Developer Certificate of Origin 1.1](https://developercertificate.org/). Read it before signing. Use your real name and a reachable email address that you choose to publish in the commit:

```sh
git commit -s
```

This adds `Signed-off-by: Name <email>` to the commit message. It certifies your right to submit under the project's license; it does not transfer copyright. There is no CLA. Maintainers review sign-off manually; this change does not enable an automatic DCO merge gate or change branch protection.

## Community and review

Use bug reports for reproducible defects and proposals for changes to behavior or scope. Useful labels are `bug`, `enhancement`, `documentation`, `good first issue`, and `help wanted`; maintainers assign them when appropriate. Their existence in hosted repository settings is not assumed by these files.

Follow the [Code of Conduct](CODE_OF_CONDUCT.md). Read [SECURITY.md](SECURITY.md) before reporting a suspected vulnerability.
