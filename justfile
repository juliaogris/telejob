set quiet

# Run all checks.
all: test lint

# Run all checks, require up-to-date repository.
ci: check-uptodate all
	echo "{{GREEN}}CI passed{{NORMAL}}"

# Test Go code. `just test Simple` runs TestControllerSimple only.
test name="":
       sudo bin/go test -v -race -count=1  -run={{name}} ./...

# Stress test. `just stress 1000` runs with 1000 jobs.
stress jobs:
       sudo bin/go test -v -race -count=1 -run="Many" ./pkg/job -jobs={{jobs}}

# Lint Go and Proto files.
lint:
	golangci-lint run
	buf lint --config proto/buf.yaml

# Format Go and Proto file.
fmt:
	gofumpt -w .
	go mod tidy
	buf format -w

[private]
check-uptodate: fmt
	test -z $(git status --porcelain) || { git status; false; }
