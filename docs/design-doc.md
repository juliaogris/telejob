---
authors: Julia Ogris (julia.ogris@gmail.com)
state: draft
---

# Telejob Design Doc

Required Approvers: Two of `nklaassen`, `r0mant`, `russjones`, `smallinsky`

This design doc conforms to the Teleport [RFD Guidelines].

[RFD Guidelines]: https://github.com/gravitational/teleport/blob/aaf8cd94001552ad53679a87d5038ce5d2a5e03f/rfd/0000-rfds.md

## What

This document outlines the design of Telejob, a remote job execution service.
Telejob allows users to run and manage commands with resource limits through a
gRPC API and client CLI. Mutual TLS secures communication, using client
certificates for user authorization.

## Why

This codebase demonstrates [@juliaogris]'s Go and systems software engineering
abilities according to the [Teleport Systems Engineering Challenge].

[Teleport Systems Engineering Challenge]: https://github.com/gravitational/careers/blob/main/challenges/systems/challenge-1.md
[@juliaogris]: https://github.com/juliaogris

## Details

Telejob uses a gRPC API secured with mutual TLS (mTLS) to facilitate
communication between the server and client. The server manages job execution
through the endpoints `start`, `stop`, `status`, and `logs`, while the client
provides a CLI for user interaction. A "job" is defined as a process with
assigned resource limits, initiated by any command, and can be in any execution
state. User authorization is based on the common name (CN) within the client
certificate – a user can start any command, but can only manage(stop, query the
status of, and retrieve logs for) their own jobs.

### UX

Telejob consists of two separate binaries:

- Server: `telejob-server`
- Client: `telejob`

#### Server: `telejob-server`

The `telejob-server` binary starts a gRPC server and can be configured using the
following flags or environment variables:

    --address=STRING           Address to listen on ($TELEJOB_ADDRESS).
    --server-cert=STRING       Server certificate file ($TELEJOB_SERVER_CERT).
    --server-key=STRING        Server private key file ($TELEJOB_SERVER_KEY).
    --client-ca-cert=STRING    Client CA certificate file ($TELEJOB_CLIENT_CA_CERT).
    --cpu-limit=FLOAT-64       Number of CPUs to allocate per job.
    --memory-limit=UINT-64     Memory limit in KiB per job.
    --io-limit=IO-LIMIT,...    I/O Limit per job. ex.: "252:1 rbps=1000000".

Starting the server requires root privileges so that the server can set up
cgroups, for example:

```sh
sudo telejob-server \
    --address=localhost:8443 \
    --server-cert=certs/server.crt \
    --server-key=certs/server.key \
    --client-ca-cert=certs/client-ca.crt
```

#### Client: `telejob`

The `telejob` client binary provides a command-line interface (CLI) for
interacting with the Telejob server. It uses a gRPC client to communicate with
the server and formats the output for user readability.

To establish a connection to the server, the client requires the following
global flags:

    --address=STRING           Server address ($TELEJOB_ADDRESS).
    --client-cert=STRING       Client Certificate file ($TELEJOB_CLIENT_CERT).
    --client-key=STRING        Client Private Key file ($TELEJOB_CLIENT_KEY).
    --server-ca-cert=STRING    Server CA certificate file ($TELEJOB_SERVER_CA_CERT).

The `telejob` client CLI provides the following commands:

    start <COMMAND> ...        Start a new job executing the specified command with arguments.
                               Print the job's ID.

    stop <JOB_ID>              Stop the job with the given <JOB_ID>.

    status <JOB_ID>            Print the status of the job with the given <JOB_ID>.

    logs <JOB_ID>              Print the logs of the job with the given <JOB_ID>.
                               Continuously stream additional output.

**Example**

Set up connection values via environment first:

```sh
export TELEJOB_ADDRESS=localhost:8443
export TELEJOB_CLIENT_CERT=certs/client1.crt
export TELEJOB_CLIENT_KEY=certs/client1.key
export TELEJOB_SERVER_CA_CERT=certs/server-ca.crt
```

Test `start`, `stop`, and `status` commands.

```
$ telejob start sleep 10
1
$ telejob status 1
ID  COMMAND   STATE    STARTED   STOPPED  EXIT
1   sleep 10  running  08:00:00
$ telejob stop 1
$ telejob status 1
ID  COMMAND   STATE    STARTED   STOPPED   EXIT
1   sleep 10  stopped  08:00:00  08:05:00  signal
$ telejob start sleep 1
2
$ # after a few seconds
$ telejob status 2
ID  COMMAND   STATE    STARTED   STOPPED   EXIT
2   sleep 1   stopped  08:10:00  08:11:02  0
```

Test `logs` command:

```
$ touch /tmp/log
$ telejob start -- tail -f /tmp/log
1
$ telejob logs 1
```

In another start another log reading client:

```
$ telejob logs 1
```

In a third terminal append to `/tmp/log`.

```
$ echo "Hello" >> /tmp/log
```

Output in first and second terminal:

```
Hello
```

## API

The gRPC API for Telejob reflects the functionality of the `telejob` CLI
described above.

Most API calls follow a simple request-response pattern. The exception is the
`Logs` method, which streams log data from the server to the client.

```proto
service Telejob {
  rpc Start(StartRequest) returns (StartResponse) {}
  rpc Stop(StopRequest) returns (StopResponse) {}
  rpc Status(StatusRequest) returns (StatusResponse) {}
  rpc Logs(LogsRequest) returns (stream LogsResponse) {}
}
```

For a complete definition of the API, including message structures, see the
[Telejob Protobuf](../proto/telejob.proto) definitions.

## Security

### TLS setup

Telejob is a new service that complies with the [Modern TLS compatibility] as
defined by Mozilla and supports TLS 1.3 only with its predefined, default
cipher suites.

- Protocols: TLS 1.3
- The default cipher suites provided by TLS 1.3 are used:
  - `TLS_AES_128_GCM_SHA256`
  - `TLS_AES_256_GCM_SHA384`
  - `TLS_CHACHA20_POLY1305_SHA256`
- Certificate type: `ECDSA (P-256)`
- Certificate lifespan:
  - Server certificates: Maximum 90 days
  - Client certificates: Maximum 1 day (for enhanced security)

In a build step Telejob provides a facility to generate new client and server
certificates and optionally client and server CA certificates according to the
above requirements.

For testing purposes, sample client and server certificates, private keys, and
CA certificates (with extended expiry dates) are provided in the repository's
`testdata/` directory.

[Modern TLS compatibility]: https://wiki.mozilla.org/Security/Server_Side_TLS

#### Client Certificates

- Each client possesses a unique client certificate and private key pair.
- These certificates are issued and signed by the Client Certification
  Authority (ClientCA).
- Clients present their client certificate when connecting to the server.
- The server verifies the client certificates using the ClientCA certificate.
- The server trusts the ClientCA and its certificate.

#### Server Certificate

- The server has its own server certificate and private key pair.
- This certificate is issued and signed by the Server Certification Authority
  (ServerCA).
- The server presents its server certificate when connecting to clients.
- Clients verify the server's certificate using the ServerCA certificate,
  thereby authenticating the server.
- Clients trust the ServerCA and its certificate.

If the server certificate is issued by a CA already trusted by the client's
operating system, it is not necessary to provide the ServerCA to the client
explicitly.

### Authentication

Telejob uses client certificates to verify user identities:

1. The client presents its certificate when connecting.
2. The server checks if the certificate is valid and trusted
   - Verifies the digital signature.
   - Ensures the certificate is within its valid time range.
   - Confirms the presence of a `commonName`
   - Verifies the certificate is signed by the Client CA
3. The server uses the certificate's `commonName` (CN) to identify the user.

This ensures only users with valid certificates can access Telejob.

### Authorization

Telejob uses a simple authorization model based on job ownership:

- Any user can start a job.
- Users can only manage their own jobs.

This means users can only stop, get the status of, or stream logs for jobs they
started. There are no special user roles or permissions.

### Trust Model

Telejob currently operates under a high-trust model for authenticated users. This is because:

- **Root privileges are required:** To manage resource limits, jobs run with root privileges.
- **No command restrictions exist:** Users can execute any command.
- **The focus is on resource limits:** Process isolation is not enforced.

This model assumes users will not intentionally misuse the system or interfere with other jobs.

## Process Execution Lifecycle

When a user initiates a new job through the Telejob service, the server performs
the following steps:

1. **Command Validation:** The server validates the provided command to ensure
   it's not empty.
2. **Job ID Generation:** A unique job ID is generated for the new job.
3. **Resource Allocation:** The server sets up cgroups v2 to enforce resource
   limits.
4. **Log Dispatcher Setup:** A log dispatcher is initialized to capture the
   job's output.
5. **Process Configuration:** A [`SysProcAttr`] struct is created with the
   `UseCgroupFD` and `CgroupFD` fields set. This configuration ensures that the
   new process is associated with the previously created cgroup.
6. **Command Execution:** An [`exec.Cmd`] is constructed to execute
   the user-provided command. The `SysProcAttr` from the previous step is assigned
   to the `Cmd` to ensure proper cgroup association. Additionally, the `Stdout`
   and `Stderr` of the `Cmd` are connected to the log dispatcher.
7. **Process Start:** The command is started using `cmd.Start()`.
8. **Asynchronous Wait:** A separate goroutine is launched to wait for the
   command to terminate using `cmd.Wait()`. This allows the server to handle
   other requests concurrently.
9. **Job Termination (Optional):** If a user requests to stop the job, the
   server writes `1` to the job's `cgroup.kill` file. This means that the job's
   process and all its forked child processes will be killed via `SIGKILL`. For
   simplicity, a more graceful termination sequence (e.g., `SIGTERM`, `SIGKILL`
   with grace periods) is not implemented.
10. **Cleanup:** After the process terminates (either naturally or due to a
    `SIGKILL`), the server removes the associated cgroup and log dispatcher,
    releasing resources.

[`SysProcAttr`]: https://pkg.go.dev/syscall#SysProcAttr
[`exec.Cmd`]: https://pkg.go.dev/os/exec#Cmd
[cgroup v2 docs]: https://docs.kernel.org/admin-guide/cgroup-v2.html#core-interface-files

### Termination Status

The `Status.ExitCode` field in the status response provides information about
the job's termination status:

- **Successful termination:** If the command runs to completion, the
  `ExitCode` reflects the process's exit code (0-255).
- **Termination by signal:** If the command is terminated by a signal, the
  `ExitCode` is set to -1. That's the case when the user stops it. In the
  client CLI, this is presented as `EXIT: signal` (see godocs of
  [`ProcessState.ExitCode`] for details).
- **Startup failure:** If the command cannot be started (e.g., due to a
  non-existent command), the gRPC method immediately returns an error with
  [`InvalidArgument` code]. In this case, the job is not created, and no
  `ExitCode` is provided.
- **Running job:** A running job has an `ExitCode` of -2, equivalent to the
  `job.NotTerminated` constant.

[`ProcessState.ExitCode`]: https://pkg.go.dev/os#ProcessState.ExitCode
[`InvalidArgument` code]: https://pkg.go.dev/google.golang.org/grpc/codes#Code

## Resource Management with cgroups

Telejob uses [cgroups v2] to enforce resource limits on user jobs, preventing
any single job from monopolizing system resources.

The server applies identical resource limits to all jobs. These limits are
configured through server-side flags and are not customizable on a per-job
basis.

The job worker package establishes the following cgroup hierarchy:

- **Parent cgroup:** `/sys/fs/cgroup/telejob/`
  - This cgroup is created if any resource limits are specified via server flags.
  - The following controllers are enabled for this parent cgroup: `+cpu +io +memory`.
- **Job-specific cgroups:** `/sys/fs/cgroup/telejob/<JOB_ID>/`
  - Each job is assigned its own cgroup under the parent cgroup.
  - If resource limits are defined, they are applied to the job's cgroup by
    writing the corresponding values to `cpu.max`, `memory.max`, and `io.max`
    files within the job's cgroup directory.

[cgroups v2]: https://docs.kernel.org/admin-guide/cgroup-v2.html

## Log Streaming

Telejob has a log streaming endpoint that allows clients to access the output of
their jobs. This includes both standard output (`Stdout`) and standard error
(`Stderr`) streams.

- **Complete Log Capture:** The log streaming mechanism captures the entire
  output of a job, starting from the beginning of its execution.
- **In-Memory Storage:** All log data is stored in memory for retrieval and
  streaming.
- **"Follow" Mode:** Clients "follow" the log, which means they will
  continuously receive new output as it is generated by the running job.
- **Concurrent Client Support:** Multiple clients can simultaneously stream logs
  from the same job without interference.
- **Combined Output:** The log dispatcher combines standard output and standard
  error streams into a single log stream.

## Log Streaming Implementation

The core of the log streaming functionality is the `logDispatcher`. It manages
incoming log data from a job's standard output and standard error streams. This
data is received through a dedicated input channel.

Readers request logs from the `logDispatcher` via a dedicated request channel.
To prevent blocking, readers send a single log request and immediately await a
single response. This ensures the `logDispatcher` remains responsive even with
slow readers:

1.  **Incoming log data:** Raw log data is received on the input channel.
2.  **Data aggregation:** The `logDispatcher` appends this data to an internal
    buffer to provide access to historical logs.
3.  **Handling new requests:** When a new reader requests log data, the
    `logDispatcher` first sends all historical logs. A log request contains a
    response channel and a start index holding the current position in the full
    log. The reader **must** immediately read from the response channel and then
    process the log data, which may be time-consuming. Once processing is complete,
    the reader sends a follow-up request with an updated start index. If no new log
    data is available, the reader's response channel is registered in the
    `logDispatcher`'s followers list.
4.  **Broadcast to followers:** When new data is received through the input
    channel, it is distributed to all registered response channels in the followers
    list, representing connected readers actively streaming logs. After sending the
    current data to all followers, the followers list is reset. Readers are
    responsible for sending the next follow-up request to the `logDispatcher`.

## Design Approach

Telejob is built with the following principles

- **Correctness:** The design favors correctness with regards to synchronization
  over latency concerns.
- **Modularity:** The core job handling code is separate from the gRPC
  communication parts.
- **Security:** Strong encryption (mTLS over TLS 1.3) is used to protect
  communication.
- **Automated Quality:** GitHub Actions automatically runs tests and checks code
  quality.

## Scope and Limitations

The Telejob design has the following limitations:

- **No Persistent Storage:** Job information and logs are stored in memory only.
- **Limited Fault Tolerance:** Jobs and the server are not automatically restarted upon failure.
- **No Monitoring:** Metrics, observability, and advanced logging are not supported.
- **No Job Isolation:** Jobs are not isolated from each other or the host system.
- **Limited security:** Only client certificates and job ownership are used for
  access control. No fine-grained permissions or JWT support.
- **Uniform resource limits:** All jobs have the same resource limits.
- **Simple UX:** Lacks features such as fine-grained I/O limits, user-defined
  resource limits, and units for memory limits.
