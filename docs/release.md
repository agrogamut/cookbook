# Verification and explicit source activation

The `Verify source and native output` workflow runs on branch pushes and pull requests. It does not deploy, import into an operator database, activate a source revision, or publish an image. A successful workflow is engineering evidence, not clinical approval.

The Go job builds the pinned renderer image, adds Fish, Python and Git only to a test derivative, and provisions three disposable PostgreSQL 17 databases: enriched provider data, a case-source fixture driver, and portal fixtures. The test derivative copies its Go toolchain from the pinned builder; host build/vet and focused race use the same patch version. The lock is `docker/pdf-runtime.lock`. The unfiltered suite enables both native opt-ins and requires the 13 named source/native tests plus the database release-inspection control in `scripts/ci/events.py` to pass. Only the named GAP-017 coverage skip and optional browser-fixture-server skip are permitted, with their recorded reasons. A missing browser, converter, font tool, database or terminal event fails verification. Raw logs and whole-command results are retained even on failure.

Native admission requires 10 GiB available RAM and the phase-specific storage budget below. PostgreSQL and native verification each have a 3 GiB memory limit with no additional swap, sharing two CPUs. Each image build also has a 3 GiB memory limit and two-CPU quota. Image builds, fixture preparation and tests have separate deadlines. Runtime and system-font assertions are those in the pinned image, not claims that the host runner has equivalent fonts.

The native full command is `go test -p=1 -json -count=1 -timeout=45m ./...`. Its explicit package limit overrides the shared `GOFLAGS=-p=2`, preventing workbook and browser test packages from overlapping inside the 3 GiB native container. `GOMAXPROCS=2` and any parallelism within a package remain unchanged. Every package, assertion, native opt-in and required terminal-event check still runs.

Both image builds explicitly use the normal daemon with `DOCKER_BUILDKIT=0`, matching the method used for capacity measurements. Preflight retains the installed CLI/daemon versions, requires a reachable Linux amd64 daemon and checks classic-builder CLI flags. Those checks establish prerequisites; the actual image-build exits establish execution. An unavailable capability or failed build stops verification without switching builders or installing Buildx. Docker deprecates the classic builder, so a future removal requires a reviewed builder and capacity-contract update. Prepared-image mode skips this build preflight and continues to perform no builds or pulls.

Frontend checks follow the Go job: frozen pnpm lock, component tests, lint, TypeScript and production build. Canonical Next fonts must be downloadable; the workflow does not substitute fonts. No provider, payment or identity-service secrets are supplied to verification. Known accepted translation responses are tested locally without provider calls. Unknown production text retains its existing provider behavior.

## Capacity contract

The [public Ubuntu runner specification](https://docs.github.com/en/actions/reference/runners/github-hosted-runners) lists 14 GB storage. That is not a guarantee of free space after checkout and host tool installation. Admission samples actual available blocks after the host build/vet/race steps; existing host toolchains, caches and checkout are already charged to that free-space measurement. No historical local database or image charge is added as a fresh hosted requirement.

The normal-daemon production and test-image build at source `b18e946e8d7f95ae5d55d41b6d53301a58aec5d1` supplies the observed components. Its capacity receipt SHA256 is `c1372f5a75dc20040b7718f062a6e1d6adfe8d0ab436f616af0405652b768cfe`. The measurement used the lock and CI Dockerfile hashes bound in `scripts/ci/capacity.py`; changing either requires a reviewed capacity update. It was a local normal-cache build, not a cold or hosted run.

| Component | Bytes | Evidence or bound |
| --- | ---: | --- |
| Selected base union plus observed build increment | 6,845,239,296 | Conservative sampled inode high-water; includes builder, runtime and derivative work |
| Compressed Go and Debian parents | 325,060,003 | Unique official registry descriptor sizes; additional cold transfer allowance |
| Unobserved cold/transient margin | 1,073,741,824 | Configured allowance, not a measured cold peak |
| Context and build-side reports | 134,217,728 | Configured allowance |
| Resident image/base/cache union after build | 3,437,273,088 | Observed physical union, shared parents counted once |
| PostgreSQL image and pull overhead | 805,306,368 | Configured allowance; separate from database data |
| Fresh fixture databases, clones and WAL | 2,147,483,648 | Configured allowance, to be validated by the final native run |
| Native source/archive/external inputs, modules, compiler cache, temporary files and reports | 2,684,354,560 | Configured allowance, to be validated by the final native run |
| Native container writable layers | 268,435,456 | Configured allowance |
| Free reserve per distinct filesystem | 2,147,483,648 | Required throughout guarded commands |

With all paths on one filesystem, the cold build phase requires 10,525,742,499 free bytes. The later resident-image plus native phase requires 11,490,336,768 bytes from the original baseline. Initial admission uses the larger phase, not their sum. This replaces the unsupported fixed 20 GiB requirement. After image preparation, fresh admission requires the remaining 7.5 GiB (native work, PostgreSQL pull/data, writable layers and reserve); existing images are already occupied blocks. A colder build exceeding its declared allowance fails visibly. Neither these bounds nor local build success assert that a hosted run has passed.

Paths are grouped by their actual filesystem device. Work and PostgreSQL data share their allowances when co-located; Docker storage on a different device needs its own reserve. The default fixture uses an explicit run-owned `pgdata` bind so database growth is included. No global image prune, cache deletion or automatic guard relaxation is used to obtain a pass.

`check.py` samples VFS availability during host/frontend checks. The native host additionally samples Docker, work and PostgreSQL filesystems throughout build/provision/test commands, with a 0.5-second sampling interval. Capacity failure terminates the active command, retains its observed process exit and returns verification failure (125); a missing measurement has a distinct reason. Timeouts remain 124. Reports contain admission, samples, minimum free space and maximum sampled net occupancy growth. VFS growth includes unrelated allocations and can be affected by concurrent frees; it is not an owner-specific inode audit or a filesystem quota. Coordinated local runs also retain their independent ownership/allocation guards. The final native run must validate the declared working-set allowances; hosted execution remains unobserved until an actual Actions result exists.

## Release preflight

No deployment target is supplied by this repository. The operator must name the environment, exact database identity, source commit, image digest and source archive. Obtain the deployed revision and image digest from the deployment platform and retain its evidence before using the preflight tool. The tool labels that evidence as operator-supplied; it does not pretend to probe a running application.

Build `go build -o /private/path/release-preflight ./scripts/release` from the candidate. Use a clean checkout and an operator-owned JSON contract outside the repository. The contract has these required fields:

| Field | Required value |
| --- | --- |
| `environment`, `kind` | Explicit environment name; `staging` or `production` |
| `repository`, `source_sha` | `git@github.com:agrogamut/cookbook.git`, exact 40-character candidate commit |
| `archive_sha256`, `source_contract` | `4efae52ed55f1b2fa6bf91df5ef00ac0c7801680c54f75d093366665d7689ef3`, `final-bn-v1` |
| `deployment` | `source_sha`, exact `image_digest` prefixed `sha256:`, and retained `evidence` reference |
| `target` | `host`, `port`, `database`, `role`, PostgreSQL `cluster_identifier` |
| `expected` | Explicit `migration`, `dirty`, `archives`, `current_recipes`, `final_recipes`, `final_locales`, `final_occurrences`, `pointer_sha256` |

Count expectations come from an approved environment inventory, including explicit zeros before import. Do not copy another environment's counts. The current-pointer digest is SHA256 over rows ordered by `(case_number,recipe_id)`, each encoded as decimal case number, tab, recipe ID, tab, candidate ID, newline. Database inspection uses a repeatable-read, read-only transaction, a 30-second statement limit and a 2-minute overall bound. The inspection role must have read access and permission to read `pg_control_system()`; the tool does not grant permissions or migrate schemas.

Offline validation checks the clean source revision, archive bytes and explicit contract. It performs no database connection:

```fish
/private/path/release-preflight --contract /private/path/release.json --environment staging-name --archive data/case-source/4efae52ed55f1b2fa6bf91df5ef00ac0c7801680c54f75d093366665d7689ef3.zip
```

For actual read-only database verification, supply `RELEASE_DATABASE_URL` through the environment or a private secret injector and add `--inspect-database`. Never echo the connection string. Host, port, role and database must match the contract; production inspection requires verified TLS. This command neither imports nor activates. It refuses dirty migration state, changed source bytes, unexpected counts or a different current-pointer digest.

## Prepared native verification

The default Actions path builds the production and test images and creates its own disposable PostgreSQL fixture. A coordinated local verification can instead consume already-built images and an explicitly prepared isolated fixture. This mode never builds, pulls or creates PostgreSQL.

Run the following from the exact clean committed checkout:

    python3 -B scripts/ci/native_host.py --existing-images /private/images.json --work-dir /private/fresh-native-run --fixture-env /private/fixture.env --pg-data-dir /private/guarded-pgdata --external-data /private/verified-external-data --run-id unique-run-token

The image handoff is a version 1 JSON object with exactly these fields: version, source_sha, runtime_image, ci_image, lock_sha256, ci_dockerfile_sha256 and go_version. Both image values must be immutable SHA256 IDs. The runner verifies their platform, source and lock labels, test-image toolchain and Dockerfile labels, and inherited runtime layers. The test image also checks the actual Go executable version and installed lock before fixtures or tests.

The fixture file must be owned by the operator and readable only by that user. It contains exactly TEST_DATABASE_URL, CASE_SOURCE_TEST_DATABASE_URL and PORTAL_TEST_DATABASE_URL, targeting ci_legacy, ci_cases and ci_portal through the same loopback port and ci_fixture role. The fixture factory writes only to these disposable test databases. Keep them separate from application and retained source databases. PostgreSQL creation and cluster identity belong to the local coordinator; this mode does not manage the supplied cluster. Supply its actual retained bind directory with `--pg-data-dir` for filesystem accounting. The path alone does not prove database identity; keep the coordinator's verified container/bind/cluster receipt with the fixture file.

The work directory must be new. Source comes from a verified Git archive, with every file, mode and blob checked against the commit; ignored worktree files are not mounted. The two external enrichment CSVs are separate inputs, verified against the archived SHA256SUMS and mounted read-only. Actions downloads them from the existing dataset sources; prepared-image mode requires retained files and never downloads them. Keep source-archive.json, external-fixtures.json, prepared image metadata, tool/package versions, raw command results and native-result.json together. The same fixture factory, unfiltered test command and required native-event checks run in either mode. A prepared image is not a passing test result.

Prepared mode requires 10 GiB available RAM and the remaining native budget, without cold build or pull costs. With work and PostgreSQL on one filesystem, require 6.5 GiB free (2.5 GiB work, 2 GiB database growth, 2 GiB reserve); a separate Docker filesystem requires 2.25 GiB free for writable layers and reserve. When all three share a device, require 6.75 GiB once. Retained database and image bytes remain occupied baseline space; new growth is not omitted. These checks supplement the coordinator's aggregate storage and memory guards. No allowance is silently increased after a failed run.

## Deliberate staging and activation

First run the check-only source command, including the triage/correction validation described in [the source README](../data/case-source/README.md). Its findings remain source claims and explicit gaps, not approval to invent missing nutrition or reformulation values.

```fish
go run ./cmd/import-cases -check -details -archive data/case-source/4efae52ed55f1b2fa6bf91df5ef00ac0c7801680c54f75d093366665d7689ef3.zip
```

An operator may then run the existing importer against a specifically authorized staging target. `-database-url` performs a real staging import and can apply outstanding schema migrations. Adding `-activate` additionally changes current case-source pointers. These are two distinct write actions; neither is a dry run. Obtain the required operational approval for that exact target, take the environment's normal backup, record the preflight, and invoke the importer explicitly. This CLI requires the target argument and has no connection-string environment fallback. Use a password-free explicit connection string with a private PostgreSQL password file supplied through PGPASSFILE, so the password does not enter command arguments or logs. Do not put credentials in a checked-in script.

Repeat the read-only preflight with the approved expected post-import or post-activation inventory. The accepted final source has 6000 current recipes, 78106 paired Bengali fields and 60106 ingredient occurrences; archive/history totals depend on the target's retained revisions. Existing raw history stays immutable. Ordinary activation cannot downgrade an already active final source. No disaster-rollback command is introduced here.

Provider reference import and case-source activation remain separate. `IMPORT_ON_START` applies only to the existing provider importer; it must not be repurposed to activate final case data. Publication, deployment and production activation each require their own explicit action. A passing preflight grants none of them.
