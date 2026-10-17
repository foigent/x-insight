# Compatibility sources

These directories retain the selected original inputs used to reproduce fixed
test and installation predecessors. They are separate from the current product
and from its deployed `fixtures/` payload. Each manifest identifies the original
commit, original file bytes and modes, toolchain, and package input selection.
An original commit is provenance metadata; it is not a claim that this repository
contains the original private Git ancestry.

| Source | Original commit | Go | Build profile or purpose |
| --- | --- | --- | --- |
| Phase 1 | `b9e5811cc53a5cb06614956e66a1c54756617f0a` | 1.25.11 | Frozen legacy package |
| RC13 | `8bcea5bebf6f0f0796391dfe8c62fdf7a43519eb` | 1.25.11 | Original `0.3.0-rc.13` signed package |
| FPV9 | `8ee764accbda21f21b489e01a4ab7751571beed4` | 1.25.12 | Exact unsigned Linux binary inputs |
| FPV11 | `9c58e07f011918c4a0aed2f876ce4baa9fb12b5e` | 1.25.12 | Historical managed FPV11 to FPV12 qualification |
| FPV14 | `a10aa9cf517ce187a95f5c7e20be0f8c8fdbe6c6` | 1.25.12 | `development-d` adopt-existing predecessor |
| FPV17 | `164582cab0e7f169c265c2494d04920c61af833d` | 1.25.12 | `development-d` installed upgrade predecessor |

The four signed sources use Node 22.23.1 and npm 10.9.8 from their original
package metadata. Their adapters run the original builder and verifier with
admitted origin metadata. They retain the original product version, migrations,
schema contracts, signing checks, generated-asset equality, and host checks.
FPV11's historical CI lane remains conditional on FPV12; retaining its source
does not activate that lane for the current FPV19 profile.

Run source admission checks without credentials, services, generated outputs,
or Git history:

```sh
python3 scripts/reproduce-phase1-source.py --check-source-directory "$PWD/compatibility/phase1-linux-amd64/source"
scripts/reproduce-direct-fpv9.sh --check-source-directory "$PWD/compatibility/fpv9-linux-amd64/source"
python3 scripts/reproduce-signed-predecessor-source.py --check-source-directory rc13 "$PWD/compatibility/rc13-linux-amd64/source"
python3 scripts/reproduce-signed-predecessor-source.py --check-source-directory fpv11 "$PWD/compatibility/fpv11-linux-amd64/source"
python3 scripts/reproduce-signed-predecessor-source.py --check-source-directory fpv14 "$PWD/compatibility/fpv14-linux-amd64/source"
python3 scripts/reproduce-signed-predecessor-source.py --check-source-directory fpv17 "$PWD/compatibility/fpv17-linux-amd64/source"
```

FPV9 includes a separate `original-asset-source/` companion so the origins of
its embedded frontend and manual can be inspected. Those files are not inputs
to the exact unsigned binary recipe. Every source directory has its own NOTICE
and applicable component mapping; full third-party license and patent texts
remain under the root `licenses/` directory. Original frozen packages are not
rewritten to add notices.

Source admission, cached local rebuilding, native Linux package reproduction,
installation qualification, anonymous cold dependency acquisition, and public
contribution CI are separate checks. These local candidates have not yet passed
the final native public-source and cold-dependency acceptance. They do not grant
redistribution rights or change the current declared upgrade support.

[`source-origins.json`](source-origins.json) resolves the original repository ID
to the preserved private history outside frozen versioned manifests. Historical
object links are optional provenance; the admitted source manifests and relative
source directories above provide the build and test inputs independently. The
new canonical repository has a distinct identity.
