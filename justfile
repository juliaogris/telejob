cert_dir := x'${CERT_DIR:-certs}'
cert_expiry := x'${CERT_EXPIRY:-1 day}'
cert_ip := x'${CERT_IP:-127.0.0.1}'
cert_domain := x'${CERT_DOMAIN:-localhost}'

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

# Generate go code from proto files.
proto:
	buf generate

# Lint Go and Proto files.
lint:
	golangci-lint run
	buf lint --config proto/buf.yaml

# Format Go and Proto file.
fmt:
	gofumpt -w .
	go mod tidy
	buf format -w

# Remove all generated files.
clean:
	rm -rf pkg/pb

[private]
check-uptodate: proto fmt
	test -z $(git status --porcelain) || { git status; false; }

# ------------------ Certificates ------------------

# Create all CAs and certs. `CERT_DIR=out just certs`
[group('certificates')]
certs: clean-certs server-ca client-ca server-cert client-certs

[private]
server-ca: (ca "server-ca" x'${CERT_EXPIRY:-10 years}')
[private]
client-ca: (ca "client-ca" x'${CERT_EXPIRY:-10 years}')
[private]
server-cert: (cert "server" "server-ca" x'${CERT_EXPIRY:-90 days}')
[private]
client-certs: (client-cert "client1") (client-cert "client2")
[private]
client-cert cn: (cert cn "client-ca" x'${CERT_EXPIRY:-1 day}')
[private]
clean-certs:
	rm -rf {{cert_dir}}

# Create individual cert. `just ca test-ca "1 year"`
[group('certificates')]
ca cn expiry:
	mkdir -p {{cert_dir}}
	certstrap --depot-path {{cert_dir}} init --cn {{cn}} --expires "{{expiry}}" --curve P-256 --passphrase "" > /dev/null
	rm -f {{cert_dir}}/{{cn}}.crl
	echo "Created {{cert_dir}}/{{cn}}.crt (expiry: {{expiry}}, CA)"
	echo "Created {{cert_dir}}/{{cn}}.key"

ip_flag := if cert_ip != "" { "--ip "+cert_ip } else {""}
domain_flag := if cert_domain != "" { "--domain "+cert_domain } else {""}

# Create individual CA. `just cert test-cn test-ca "1 day"`
[group('certificates')]
cert cn ca expiry:
	mkdir -p {{cert_dir}}
	certstrap --depot-path {{cert_dir}} request-cert --cn {{cn}} {{ip_flag}} {{domain_flag}} --passphrase "" > /dev/null
	certstrap --depot-path {{cert_dir}} sign {{cn}} --CA {{ca}} --expires "{{expiry}}" > /dev/null
	rm -f {{cert_dir}}/{{cn}}.csr
	echo "Created {{cert_dir}}/{{cn}}.crt (expiry: {{expiry}}, ip: {{cert_ip}}, domain: {{cert_domain}})"
	echo "Created {{cert_dir}}/{{cn}}.key"
