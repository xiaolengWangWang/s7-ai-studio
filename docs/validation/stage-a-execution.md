# SDD ledger — plan: docs/superpowers/plans/2026-10-11-s7-core.md
Baseline: 632ea73; working branch feat/s7-core; approved in-place new project initialization.
Baseline tests: go test ./internal/ai ./internal/update ./build/release → PASS.
Pre-flight: Tasks 1→2/3/4/6/7 share device addresses, values and events; interfaces aligned.
Pre-flight: Tasks 3→4/5 share TPKT frames only; simulator request semantics remain independent.
Pre-flight: Tasks 4/5→7 share Driver and simulator lifecycle; tests run local loopback, no production device.
Ruling: Work in the approved project directory on feat/s7-core — no previous repository or user changes to isolate — cost if wrong: a linked worktree can be added later without losing branch history.
Task 1: in progress. Root-wide tests cannot compile until planned recorder model and icon fixes in Tasks 6/7.
Ruling: Native PowerShell extracts briefs and writes task records — Git Bash reads protected Markdown ciphertext (E-SafeNet) while .NET reads plaintext — cost if wrong: helper scripts are not reusable in this environment.
Task 1: Ruling: add Value.Elements and STRING length descriptor — typed count reads must preserve every element and STRING requires a declared bound — cost if wrong: future API consumers must accept these optional JSON fields.

Task 1: complete (632ea73..d25c58d6364bd2d73dd6cc4083b0495bea667971; go test ./internal/device -count=1 → PASS; baseline AI/update/release tests PASS; full suite still blocked by planned recorder import and icon fixes).
Task 2: in progress.

Task 2: complete (d25c58d..917c2258af27745c5c889528d3e186a026ae36fd; go test ./internal/device ./internal/profile -count=1 → PASS).
Task 3: in progress.

Task 3: complete (917c225..5ba1a8b8e5659e4b789c45a89476ae68b56d0151; fixed golden frames and go test ./internal/s7 -run '^$' -fuzz FuzzDecodePDU -fuzztime 10s → PASS).
Task 4: in progress.

Task 4: Ruling: no automatic retry after a transport timeout — a safe reference-matched session is required and no supported retryable service code is identified yet — cost if wrong: caller must explicitly reconnect and repeat reads.
Task 4: Ruling: add WriteResult.Segments — split writes must expose each confirmed or uncertain segment — cost if wrong: callers must accept the optional field.
Task 4: complete (5ba1a8b..e807fa1ba62b98550c4f552a4bfcc611485bc029; go test ./internal/device ./internal/profile ./internal/s7 -count=1 → PASS; uncertain writes, cancellation, close, native bit writes and split reports covered).
Task 5: in progress.

Task 5: complete (e807fa1..4f07b4792c34211078a24eb993f15cd203866589; go test ./internal/device ./internal/profile ./internal/s7 ./internal/simulator ./tests -count=1 → PASS).
Task 6: in progress.

Task 6: complete (4f07b47..b9e3b24c0df23c94415760aae36c46ed0553532b; go test ./internal/recorder -count=1 -timeout 3m → PASS; new metadata, unknown protocols, read-only byte hash and existing migration/rotation tests covered).
Task 7: in progress.

Task 7: CLI tests watched RED (Run undefined) then GREEN; simulator entry compiled after correcting PDULimit int. Regression TestReadRetainsCompletedItemsAfterLaterBatchDisconnect watched RED then GREEN.
Task 7: go mod tidy, go vet ./..., go test -tags ci -count=1 -timeout 15m ./... → PASS; CGO_ENABLED=0 CLI/simulator builds PASS. Actual executable demonstrations: initial read 42 (0), write 123 CONFIRMED (0), read back 123 (0), mixed/rejected items BAD (1), dropped write ACK UNKNOWN (1), new-connection read back 99 (0). SQLite latest session ended, six S7_TCP events, DINT 42 metadata preserved.
Task 7: complete (b9e3b24..8ea67c0; implemented and verified; final whole-branch review pending).
Final review: fresh reviewer stage_a_review (gpt-6-astra xhigh), read-only 632ea73..8ea67c0. No Critical or Minor findings; three Important findings verified and accepted.
Final: Ruling: desktop, Web, AI workflows and automatic write readback remain later-stage work — the approved Stage A plan delivers only core and headless tools — cost if wrong: these product features are not delivered yet.
Final: Ruling: PPI, OPC UA, S5 and all-series hardware certification remain later-stage work — Stage A explicitly implements classic S7 TCP with synthetic verification only — cost if wrong: route profiles do not prove hardware support.
Final: Ruling: release assets, installers and remote cross-platform CI remain unverified — Stage A excludes publishing and no remote run occurred — cost if wrong: no formal release or cross-platform assurance is available.
Final: Ruling: promote the oldest events-table read-only compatibility issue to Important and fix it — users reasonably expect the promised old-history reader to include log rows — cost if wrong: missing optional fields can only return defaults, not recover absent data.
Final fix pass: all four issues reproduced RED before implementation. Zero-value JSON uses active typed pointers; received frames retain exact wire bytes; failed handshakes update RX events; old log queries default absent optional columns without migrating the file.
Final: fixed active zero JSON values — TestValueJSONKeepsActiveZeroFields and TestRecorderKeepsZeroValueJSON RED→GREEN, suite 12/12 packages PASS.
Final: fixed fabricated truncated RX bytes — TestTPKTTruncationReturnsOnlyReceivedPayload and TestObserverRecordsExactTruncatedOrInvalidWire RED→GREEN, suite 12/12 packages PASS.
Final: fixed successful status on failed handshakes — TestFailedHandshakeObserverRecordsParseError RED→GREEN, suite 12/12 packages PASS.
Final: fixed oldest log archive compatibility — TestLegacyEventsFileMissingColumnsReadOnly RED→GREEN, suite 12/12 packages PASS.
Final: no deferred minors. Final CGO=0 builds, nine actual EXE demos and SQLite query PASS; program SHA256 persisted.
