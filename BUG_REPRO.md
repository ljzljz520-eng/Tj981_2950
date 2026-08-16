# BUG_REPRO

The following failures were observed while validating the initial project state.
Each section records what failed, how to reproduce it, and the complete command output.
They are preserved intentionally; only failing build gates are omitted from the generated Dockerfile.

## Failure 1: Go test (.)

- Observed problem: `Go test (.)` failed in the initial project state.
- Working directory: `.`
- Command: `cd /app && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -count=1 ./...`
- Exit status: `1`

```text
?   	drivemigrate/cmd/drivemigrate	[no test files]
?   	drivemigrate/fixture	[no test files]
--- FAIL: TestMigrationCancellationIsAtomicAndRetryable (0.00s)
    service_test.go:144: Migrate() error = <nil>, want context canceled
    service_test.go:147: task.Status = "completed", want "canceled"
    service_test.go:150: target.Count() = 3, want 0
FAIL
FAIL	drivemigrate/migration	0.003s
FAIL
```

## Architecture reproduction

### linux/amd64
- Go toolchain version: exit `0`
- Go build (.): exit `0`
- Go test (.): exit `1`
- Go run smoke (cmd/drivemigrate): exit `0`
### linux/arm64
- Go toolchain version: exit `0`
- Go build (.): exit `0`
- Go test (.): exit `1`
- Go run smoke (cmd/drivemigrate): exit `0`
