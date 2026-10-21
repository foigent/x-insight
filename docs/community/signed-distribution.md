# Signed distribution and managed installation

The current source candidate is `0.2.0-dev.fpv20`, schema 46, manual
`0.2.0-dev.fpv20/manual-r1`, with release channel `release_candidate`.
This page describes the existing supported path for an approved distribution.
The owner-signed FPV20 candidate has been produced and locally verified.
Its exact public assets and installation erratum are recorded in
[the FPV20 candidate record](releases/v0.2.0-dev.fpv20.md). Public availability and
native-host qualification are separate accepted steps; this record does not
claim a stable or Production release.

## Release operator: select authority and qualify the exact source

Use the general [builder](../../scripts/build-signed-release.sh) with
`--profile=development-d`, the current [release config](../../release/d-validation-config.json)
and [compatibility matrix](../../release/d-validation-compatibility-matrix.json).
Keep the exact clean release commit and successful applicable CI/native
acceptance records. Source preparation alone does not qualify an installer.
The configured 17 upgrade sources are declarations; record actual accepted
predecessor scenarios separately. The rc13 profile retains its historical contract.

Before signing, the release owner must select the approved signing authority,
existing-key reuse or new-key creation, custodian, protected signing host,
validity/revocation policy and release scope. The existing signer uses an
owner-only Ed25519 JSON key file and an `xinsight.trusted-release-keys.v1`
public trust store. It does not supply a hosted key service or keyless signing.
A dedicated owner-controlled signing process using that existing format is
the minimum supported option; the owner must choose its actual custody.
The builder sets manifest `created_at` to the source commit timestamp. Existing
trust validation checks that timestamp against `not_before`/`not_after`, not
current wall-clock expiry. Bind the source epoch and approved key-validity
policy before signing; a newly generated default trust may start after that
commit and fail verification. Do not alter validity to bypass an unapproved
trust policy. Publish revocation/status changes through the authority record.

Publish the public key's exact `ed25519:sha256:...` identity, trust-store hash,
validity and bootstrap verifier's hash through an independently authenticated
authority record. A trust store or checksum delivered only beside an otherwise
untrusted package cannot establish that authority. Consumers may instead build
the verifier from the independently authenticated exact source and pinned
toolchain. Choosing a checksum is a trust decision, not signature verification.

The owner-approved public key and XI issuance window are recorded in
[release authority](release-authority.json) and [public trust](trusted-release-keys.json).
The XI window is 2026-10-18T10:42:42.832Z through 2026-11-17T10:42:42.832Z;
it asserts no earlier UR activation or original key-generation date. The real
source timestamp of the formal candidate must fall in this window. These
source records are preparation, not evidence of an issued or qualified artifact.
A new owner-only private representation records its actual creation time and
reuses the same material; never backfill the unknown original material birthday.

The minimum authority channel is the existing protected canonical GitHub
source plus the owner's direct fingerprint attestation. Consumers pin the real
source and authority-record commits, and check the public trust/bootstrap hashes
before execution, or build a verifier from that authenticated exact source.
The final authority record gains real package/bootstrap hashes only after
production and independent review. A record cannot embed its own unknown commit.
Source and Release still share an owner account: this model does not claim an
independent administrative domain or protection from complete account compromise.
It does not require a new domain or service by default.

The existing toolchain is Go 1.25.12, Node 22.23.1 and npm 10.9.8. From the approved
clean source, use a new absolute output directory. These examples describe
separately authorized operations; they are not an instruction to create a key:

```sh
bash scripts/build-signed-release.sh /approved/new-preflight --profile=development-d --dry-run
# Only in the approved signing process with XINSIGHT_RELEASE_SIGNING_KEY already configured:
bash scripts/build-signed-release.sh /approved/new-build --profile=development-d
```

`--dry-run` checks preconditions without inspecting a signing key. The signed
build creates `xinsight-v0.2.0-dev.fpv20-linux-amd64-COMMIT.tar.gz`, the separate
`bootstrap/xinsight-release-linux-amd64-COMMIT`, and `build-result.json`.
`COMMIT` means the actual 40-character release commit, never a branch alias.
`--prepare-only` stops at an unsigned, non-installable payload. CI and native
qualification artifacts signed with temporary test trust remain qualification
artifacts and must not be relabelled as formal public distributions.

On the approved native Linux x86_64 qualification host, use the exact source
and independently admitted verifier. Choose a new absolute staging destination:

```sh
bash scripts/verify-signed-release.sh /approved/candidate.tar.gz /approved/public-trust.json /approved/new-qualified-payload --profile=development-d
```

This existing producer check verifies the signature, manifest, payload,
embedded binaries/UI, schema contract, both manuals and notices. A successful
artifact check remains separate from first install, predecessor/recovery,
ordinary-user access and the installed two-product acceptance.

## Publish a portable record after approval

The owner must select the canonical release/download channel and the
independent authority-record location. Neither is supplied by this guide.
Before any immutable release, bind its exact repository, source commit/tree,
profile, version/channel, artifact name/size/SHA-256, manifest/payload hashes,
signing key ID, bootstrap name/size/hash and public trust-store hash. Include
the applicable acceptance records and retained project/dependency attribution.
Do not invent author identities or sign off historical commits on their behalf.
The signed payload already includes LICENSE, NOTICE, THIRD_PARTY_NOTICES and
the accompanying license texts; keep them intact.

Reuse the CLI's `xinsight.release-verify-result.v1` JSON as portable public
verification metadata: it includes all three package hashes, key ID, version,
commit and target. The signed manifest and exact source config bind schema 46,
manual-r1, compatibility and the existing schema/migration digests. Publish the
independently authenticated bootstrap/trust hashes in the authority record.
No new release schema or signing framework is needed for this path.

Keep the original `build-result.json` private: its `output` is an absolute
build-host path. Publish the existing path-free verification result, rather
than exposing that path or modifying the original build evidence. Do not
publish extraction output containing a private destination, private keys,
account/DSN files, or unreviewed logs. A formal distribution approval must bind
the final exact bytes and channel after qualification, not this source proposal.

## Consumer: verify, extract, check, then install

Obtain the approved artifact and independently authenticated verifier/trust
store. Check their actual hashes and key identity against the authority record
before executing the verifier. The consumer uses the standalone CLI and does
not need the source checkout, Go, Node.js or npm to perform these steps.

Use the actual approved release commit in place of `APPROVED_40_HEX_COMMIT`:

```text
xinsight-release verify --artifact /approved/candidate.tar.gz --trust-store /etc/x-insight/trusted-release-keys.json --expected-commit APPROVED_40_HEX_COMMIT --expected-version 0.2.0-dev.fpv20
xinsight-release extract --artifact /approved/candidate.tar.gz --trust-store /etc/x-insight/trusted-release-keys.json --destination /approved/new-staging --expected-commit APPROVED_40_HEX_COMMIT --expected-version 0.2.0-dev.fpv20
sudo /approved/new-staging/bin/xinsightctl version --json
```

Verification rejects invalid/untrusted or revoked keys, a manifest timestamp
outside the key's declared validity, and wrong commit/version/target. Extraction verifies again and creates a new
absolute destination. Preserve the returned version/commit/manual identities.

Follow the release-matched [installation and access manual](../manual/en-US/index.md#install-and-establish-access)
([中文](../manual/zh-CN/current.md#安装与账户)). The managed controller supports
Ubuntu 22.04/24.04 amd64 with systemd and a dedicated loopback PostgreSQL 16
database. Its checks require four distinct database roles, protected DSN files,
an approved service account, access accounts, local TLS and the exact trust file.
The current Ubuntu 24.04 native PostgreSQL profile still requires Docker Compose
and inspection of the pinned PostgreSQL image; native database provisioning
does not remove those checks. The disk check requires the manifest's compressed payload artifact size
plus twice the database size plus 512 MiB; also reserve space for actual build,
image and retained-data needs on the selected host.

```text
sudo /approved/new-staging/bin/xinsightctl install --check --artifact /approved/candidate.tar.gz
sudo /approved/new-staging/bin/xinsightctl install --artifact /approved/candidate.tar.gz
sudo /opt/x-insight/current/bin/xinsightctl status
```

On Ubuntu 22.04 or 24.04, provision the documented system dependencies and a
dedicated loopback PostgreSQL 16 database. The distributed Compose profile uses
a pinned PostgreSQL container; it does not replace native systemd application
installation. The matrix separates runtime prerequisites from the previously
documented Ubuntu 24.04/native-PG validation environment. Code acceptance is
not a passing artifact or Target result; exact FPV20 qualification on Ubuntu
22.04/container PostgreSQL remains pending.

Use an empty database and unused XI application paths/unit names for a fresh
install. Where XI already exists, use a supported upgrade or first back up and
retire its exact application objects under your own maintenance/recovery
procedure. Preserve shared services and databases. A data reset, test-root
mapping or deletion of only install-state metadata cannot establish fresh
installation. Record existing OS dependencies in the acceptance scope.

Check is read-only. Install requires separate authorization for the exact host,
database, service account and credentials. A first install does not connect a
source or authorize an ordinary reader. Use the manual's subject/password,
HTTPS login and scoped-grant instructions; never use sample credentials.

## Install UniRouter and connect the same approved pair

The [UniRouter official installation guide](https://github.com/foigent/unirouter/blob/3818ba9627d1bc44eb836036b8ae8904a7175e65/docs/community/install-linux.md)
and [getting-started guide](https://github.com/foigent/unirouter/blob/3818ba9627d1bc44eb836036b8ae8904a7175e65/docs/community/getting-started.md)
were read at source `3818ba9627d1bc44eb836036b8ae8904a7175e65` on 2026-10-12.
Its VERSION is 1.12.0. Those documents still say official signed downloads are
pending; their earlier native bootstrap acceptance binds a 1.11.0 candidate and
temporary test trust. Neither source version is an official artifact identity.

When both approved distributions exist, install UR through its release-matched
download/verify/prepare path and original installer `check`, `install`, `resume`.
UR's path uses a separate rootful Docker Compose project, private installation
directory, six signed image digests, PostgreSQL/Redis and a loopback console.
It has its own independent verifier/public ring and administrator bootstrap.
It does not authorize a provider credential, qualification probe or paid call.
Do not substitute XI's compiled CI fixture or P3 test producer for this install.

Then follow XI's [first-source setup](../manual/en-US/index.md#connect-the-first-source)
and [ordinary-reader grants](../manual/en-US/index.md#grant-ordinary-users-the-right-scope),
with UR's [existing setup API](https://github.com/foigent/unirouter/blob/3818ba9627d1bc44eb836036b8ae8904a7175e65/docs/tech-ref/05-openapi-spec.yaml).
Confirm exact installations and projects, invitation/preview/consent, capture
and retention choices, active original connection credentials and received
source records. Connection administration does not grant every reader access.

Record both installed artifacts' version/commit/digests and supported protocol
bindings. For one authorized synthetic execution, preserve the original
source/request IDs, usage, currency/unit, calculation basis and charged amount.
Have an ordinary authorized XI user locate that same execution and evidence;
verify foreign-scope denial and revocation of an existing reading. Missing cost
or provider facts remain explicit. A zero charge and a calculated amount are
different facts. Do not sum child attempts into an already authoritative total.
This full installed pair remains unperformed for the current candidate. The
existing P3 and installed-CI pins are separate qualification inputs and are not
a formal UR/XI release pair or a substitute for this consumer walkthrough.

## FPV20 preparation boundary

FPV20 changes source and schema identity. The historical FPV19 preparation and signing inputs keep their original scope. The actual owner-signed FPV20 main084 candidate has since been produced and locally verified; its exact public identity is recorded below. This does not itself authorize public publication or establish native-host qualification. See the connection synchronization manual for manual defaults and independent local restore controls.

## Exact FPV20 candidate and installation erratum

The FPV20 owner-signed identity and corrected verification/extraction examples
are in [the FPV20 candidate record](releases/v0.2.0-dev.fpv20.md). The historical
FPV19 preparation scope remains historical; its approval is not rewritten as a
new FPV20 publication decision. Pin the real later authority-record commit
separately from the immutable source08435e09 candidate.
