out := 'out'
cert_dir := x'${CERT_DIR:-certs}'
cert_expiry := x'${CERT_EXPIRY:-1 day}'
cert_ip := x'${CERT_IP:-127.0.0.1}'
cert_domain := x'${CERT_DOMAIN:-localhost}'

set quiet

# Run all checks.
all: build test lint

# Run all checks, require up-to-date repository.
ci: check-uptodate all
	echo "{{GREEN}}CI passed{{NORMAL}}"

# Build server and client binaries.
build: out
	go build -o {{out}} ./cmd/...

# Test Go code. `just test Simple` runs TestControllerSimple only.
test name="":
       sudo bin/go test -v -race -count=1  -run={{name}} ./...

# Stress test. `just stress 10 20` runs with 10 jobs, 20 log reads.
stress jobs="1000" readers="1000" address="":
       sudo bin/go test -v -race -count=1 -run="Many" ./pkg/job -jobs={{jobs}}
       sudo bin/go test -v -race -count=1 -run="Many" ./cmd/telejob -readers={{readers}} -address={{address}}

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
	rm -rf {{out}}
	rm -rf pkg/pb

[private]
check-uptodate: proto fmt
	test -z $(git status --porcelain) || { git status; false; }

[private]
out:
	mkdir -p {{out}}

# ------------------ Execution ------------------
test_cert_dir := "pkg/telejob/testdata"
client := "1"

server_env := replace("
	TELEJOB_ADDRESS=localhost:8443
	TELEJOB_SERVER_CERT="+test_cert_dir/"server.crt
	TELEJOB_SERVER_KEY="+test_cert_dir/"server.key
	TELEJOB_CLIENT_CA_CERT="+test_cert_dir/"client-ca.crt", "\n", "")

client_env := replace("
	TELEJOB_ADDRESS=localhost:8443
	TELEJOB_CLIENT_CERT="+test_cert_dir/"client"+client+".crt
	TELEJOB_CLIENT_KEY="+test_cert_dir/"client"+client+".key
	TELEJOB_SERVER_CA_CERT="+test_cert_dir/"server-ca.crt
	TELEJOB_TIME_FORMAT=15:04:05", "\n", "")

# Start server.
[positional-arguments]
[group('execution')]
serve *args='':	build
	sudo {{server_env}} ./{{out}}/telejob-server $@

# Run client. `just run start sleep 10`
[positional-arguments]
[group('execution')]
run *args='': build
	{{client_env}} ./{{out}}/telejob "$@"

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
