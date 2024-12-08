set quiet

# Run all checks.
all: lint

# Run all checks, require up-to-date repository.
ci: check-uptodate all
	echo "{{GREEN}}CI passed{{NORMAL}}"

# Lint Protobuf files.
lint:
	buf lint --config proto/buf.yaml

# Format Protobuf files.
fmt:
	buf format -w

[private]
check-uptodate: fmt
	test -z $(git status --porcelain) || { git status; false; }
