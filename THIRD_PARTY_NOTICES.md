# Third-party notices

X-Insight's own code is licensed under Apache-2.0. The components below retain their original licenses. Their unmodified license, notice, and patent texts are included in `licenses/` and in built release payloads.

This inventory is generated from the Linux command dependency graph, the pinned Go runtime, and the production frontend lockfile. Development-only tools are not redistributed in these binaries; their packages retain their own license files. Historical reference documents require their own provenance review and are not covered by this inventory.

| Component | Version | Distribution | License texts |
| --- | --- | --- | --- |
| Go standard library and runtime | go1.25.12 | Go binary | [LICENSE](licenses/Go_standard_library_and_runtime/LICENSE), [PATENTS](licenses/Go_standard_library_and_runtime/PATENTS) |
| github.com/jackc/pgpassfile | v1.0.0 | Go binary | [LICENSE](licenses/github.com_jackc_pgpassfile/LICENSE) |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | Go binary | [LICENSE](licenses/github.com_jackc_pgservicefile/LICENSE) |
| github.com/jackc/pgx/v5 | v5.10.0 | Go binary | [LICENSE](licenses/github.com_jackc_pgx_v5/LICENSE) |
| github.com/jackc/puddle/v2 | v2.2.2 | Go binary | [LICENSE](licenses/github.com_jackc_puddle_v2/LICENSE) |
| github.com/santhosh-tekuri/jsonschema/v6 | v6.0.2 | Go binary | [LICENSE](licenses/github.com_santhosh-tekuri_jsonschema_v6/LICENSE) |
| golang.org/x/crypto | v0.53.0 | Go binary | [LICENSE](licenses/golang.org_x_crypto/LICENSE), [PATENTS](licenses/golang.org_x_crypto/PATENTS) |
| golang.org/x/sync | v0.21.0 | Go binary | [LICENSE](licenses/golang.org_x_sync/LICENSE), [PATENTS](licenses/golang.org_x_sync/PATENTS) |
| golang.org/x/sys | v0.46.0 | Go binary | [LICENSE](licenses/golang.org_x_sys/LICENSE), [PATENTS](licenses/golang.org_x_sys/PATENTS) |
| golang.org/x/term | v0.44.0 | Go binary | [LICENSE](licenses/golang.org_x_term/LICENSE), [PATENTS](licenses/golang.org_x_term/PATENTS) |
| golang.org/x/text | v0.38.0 | Go binary | [LICENSE](licenses/golang.org_x_text/LICENSE), [PATENTS](licenses/golang.org_x_text/PATENTS) |
| react | 19.2.7 | Embedded browser UI | [LICENSE](licenses/react/LICENSE) |
| react-dom | 19.2.7 | Embedded browser UI | [LICENSE](licenses/react-dom/LICENSE) |
| scheduler | 0.27.0 | Embedded browser UI | [LICENSE](licenses/scheduler/LICENSE) |

Regenerate with `python3 scripts/collect-third-party-notices.py`; verify with `--check`. See `licenses/manifest.json` for exact text digests.
