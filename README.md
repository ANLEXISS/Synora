SYNORA

  Local-First Distributed Intelligence & Home Automation Platform
  Go · Python · Linux · Distributed Systems · Computer Vision · IoT · Systems Architecture
  
  SYNORA is an experimental, local-first distributed platform designed to connect devices, process information, coordinate independent services, and support contextual decision-making without relying on a mandatory cloud infrastructure.
  The project explores how to build a modular, reliable and extensible system while balancing processing performance, architectural complexity, operational cost and maintainability.
  Unlike traditional home automation applications, SYNORA is designed around a distributed software architecture, with explicit communication contracts, centralized state ownership, isolated processing workloads and controlled execution boundaries.
  Status: Active development — functional software prototype with component-level validation.
  Developer: Alexis Kratz



1. Project Overview
The problem
Modern home automation and intelligent monitoring systems involve multiple technological challenges:
- Heterogeneous devices and communication protocols.
- Different processing requirements for sensors, video and control operations.
- Dependency on external cloud services.
- Complex coordination between independent components.
- Reliability and fault isolation.
- Privacy and sensitive-data management.
- Increasing maintenance costs as systems grow.
A monolithic architecture may simplify initial development, but combining device communication, state management, computer vision, network configuration and intelligent processing in a single application can introduce unnecessary coupling.
SYNORA explores an alternative approach.
The solution
SYNORA separates the system into specialized components with clearly defined responsibilities.
The architecture combines:
- Go-based services for orchestration, communication and state management.
- A local Unix-domain event bus.
- A dedicated Python computer vision worker.
- A persistent central information store.
- HTTP, REST, SSE and WebSocket interfaces.
- Device discovery and authenticated media ingestion.
- A web application.
- Experimental cognitive processing and decision-making.
- Controlled, currently dry-run action execution.
The objective is to create a platform in which components can evolve independently while remaining part of a coherent system.



2. Engineering Philosophy
The main engineering principle behind SYNORA is straightforward:
The quality of a solution depends not only on its capabilities, but also on the cost and complexity required to implement, operate and maintain it.
Development therefore focuses on identifying constraints and evaluating trade-offs before introducing additional complexity.
Objective	Engineering approach
Reliability	Explicit error handling, bounded queues, controlled state mutations
Modularity	Independent services with defined responsibilities
Maintainability	Separation of concerns and reusable interfaces
Efficiency	Local processing and workload-specific components
Privacy	Restricted propagation of sensitive information
Resilience	Reconnection, timeouts, degraded states and fail-closed validation
Extensibility	Versioned message contracts and modular integrations
Cost control	Avoid unnecessary external infrastructure and processing overhead


These are architectural objectives, not claims that every property has already been optimized or production-qualified.
The architecture is continuously evaluated against its implementation and operational costs.



3. System Architecture
SYNORA uses a local service-oriented architecture with multiple independent processes.
The currently implemented system includes the following components.
Synora Bus — Go
An internal communication service based on Unix-domain sockets.
It routes newline-delimited JSON messages between authorized system components.
Implemented mechanisms include:
- Service and executable identity verification.
- Source and destination access-control rules.
- Optional HMAC message authentication.
- Replay tracking.
- Request/response correlation.
- Bounded message handling.
- Client reconnection and timeouts.
The bus is intentionally lightweight and local.
Trade-off: it avoids requiring a separate message broker, but its transport is not durable and bounded queues can drop messages under saturation.
Synora Core — Go
The central processing and state-management component.
It is responsible for:
- Processing admitted events.
- Maintaining cognitive state.
- Producing snapshots and decisions.
- Validating information contracts.
- Applying the Safety Gate.
- Managing persistent state.
- Publishing decisions to other services.
The architecture establishes Core as the authoritative writer of cognitive state.
Universal Store
A persistent local storage layer owned by Core.
It provides:
- Atomic state persistence.
- Append-only journal records.
- State recovery mechanisms.
- Processed-event tracking.
- Idempotent state operations.
- Persistent decisions and snapshots.
The implementation uses local state files including state.json and journal.jsonl.
Trade-off: centralized state ownership simplifies consistency and auditing, but the current architecture does not demonstrate replicated availability or distributed storage scalability.
Synora Discovery — Go
The external integration boundary.
Its responsibilities include:
- Device-facing interfaces.
- Authenticated video ingestion.
- Local network configuration.
- MediaMTX integration.
- Computer vision worker supervision.
- External HTTP interfaces.
- Device-related event processing.
- Dry-run action handling.
Discovery separates external device interactions from Core's authoritative state management.
This boundary is important because the external environment is less predictable than internal application logic.
Vision Worker — Python
An independent Python service dedicated to computer vision workloads.
It communicates with Discovery through a Unix-domain socket and supports processing requests and aggregate evidence generation.
The current implementation contains mechanisms for:
- Clip processing.
- Bounded tracking.
- Vision event aggregation.
- Detector failure handling.
- Evidence contract validation.
- Face-data lifecycle management.
- Worker protocol handling.
The integration deliberately limits the sensitive information transferred toward Core.
Raw frames, embeddings, identity information and local media paths are excluded from the Core-facing Vision Evidence V1 contract.
Important: the software worker has been validated synthetically, but real hardware inference and recognition accuracy have not been qualified.
Synora API — Go
A separate application interface providing:
- REST endpoints.
- Session authentication.
- Intelligence-related data access.
- WebSocket updates.
- Runtime health interfaces.
- Static web application serving.
The API is implemented in source, but its current deployment integration is incomplete.
Synora Web — React
A web application intended to provide system interaction and access to information exposed by the API.
The current source exists, but its build and automated frontend behavior were not reproduced during the latest audit.



4. Data Flow
A representative vision-processing path follows this architecture:
  1. A trusted camera or edge client submits media to Discovery.
  2. Discovery validates authentication, device trust, timestamps and request limits.
  3. Accepted media is submitted to the bounded vision worker pool.
  4. The Python worker performs processing and returns aggregate evidence.
  5. Discovery validates and forwards the evidence through the internal bus.
  6. Core processes the event and commits its resulting state to the Universal Store.
  7. Decisions and snapshots are published to authorized consumers.
  8. A potential action request passes through the Safety Gate.
  9. Discovery returns a dry-run action result.
This design attempts to keep information ownership explicit while limiting direct coupling between external devices and cognitive processing.
Current validation: individual components have passed tests, but the full central journey is not currently reproducible from a fresh checkout because required cognitive model bundles are missing.
For the detailed architecture, see [Architecture Documentation](docs/ARCHITECTURE.md).



5. Technology Stack
Component	Technology
Core services	Go
Specialized processing	Python
Web client	React
Internal communication	Unix-domain sockets, JSON contracts
External interfaces	REST, HTTP, SSE, WebSockets
Media processing	MediaMTX integration, Python vision worker
Persistence	Local transactional state and append-only journal
Operating system	Linux
Service management	systemd
Testing	Go tests, Python unittest, central integration harness
Target hardware integration	ARM64 / RK3588-related tooling


Protocol maturity
Not every protocol represented in the repository is operationally qualified.
- HTTP/REST/WebSockets: implemented and component-tested.
- Unix socket IPC: implemented and component-tested.
- RTSP/MediaMTX: integration code exists; live media operation remains unqualified.
- HLS/WebRTC: configured but not validated in the latest audit.
- MQTT: dependency and configuration foundation exist, but no active application integration was identified.
- ONVIF: no confirmed implementation was found.
Protocol support is therefore documented according to its actual implementation maturity.



6. Architectural Decisions & Trade-offs
SYNORA is also an engineering experiment in selecting appropriate solutions under multiple constraints.
Custom local communication bus
Benefit: lightweight local IPC, inspectable contracts and OS-level identity controls.
Cost: custom routing, authentication, reconnection logic and limited transport durability.
Go and Python process separation
Benefit: specialized processing can evolve independently, while Python vision dependencies remain isolated from Go core services.
Cost: additional IPC protocols, process supervision and packaging complexity.
Centralized state authority
Benefit: consistent ownership, auditable state transitions and easier idempotency guarantees.
Cost: a single local authoritative store without demonstrated multi-node failover.
Local-first infrastructure
Benefit: reduced mandatory cloud dependency and greater control over data processing.
Cost: hardware provisioning, local resource management and additional operational responsibilities.
Fail-closed optional capabilities
Benefit: missing models or invalid evidence do not silently generate fabricated results.
Cost: demonstrations and integrations may become unavailable until the required models and deployment artifacts are provided.
No architectural choice is claimed to be universally optimal.
The objective is to select the most appropriate trade-off for the system's actual constraints.



7. Testing & Validation
SYNORA includes unit tests, integration-oriented tests and a central end-to-end validation harness.
The latest documented audit was performed on 22 November 2026, against commit:
535431259eac0839cf5edf77beadc434df604c05
Verified results
Validation	Result
Go compilation	Passed
Go tests excluding central harness	623 passing test/subtest events, 5 skipped
Python vision tests	112/112 passed
Python syntax compilation	Passed
Configuration template validation	Passed
Full repository Go test suite	Failed — 7 test failures in central harness package
Central E2E harness	Failed — missing cognitive model bundles
Frontend build	Not reproduced
Real camera/NPU testing	Not performed
Production security audit	Not performed


Central harness limitation
The central harness currently depends on external cognitive model bundles that are absent from a fresh checkout.
The default execution reported:
- 539 aggregate cases.
- 31 passing cases.
- 508 failed cases.
The failures are largely cascading consequences of the unavailable model bundle, rather than 508 independently established implementation defects.
Nevertheless, the complete central pipeline must not be considered validated.
Historical reports describe earlier successful executions, but those results have not been reproduced against the current checkout.
Testing philosophy
The development approach emphasizes:
- Reproducible tests.
- Explicit failure classification.
- Isolation of components.
- Contract verification.
- Data integrity.
- Controlled degradation.
- Distinguishing synthetic tests from hardware qualification.
For the complete evidence and limitations, see [Validation Report](docs/VALIDATION_REPORT.md).



8. Security & Privacy
SYNORA contains several implemented security controls:
- Bearer and session authentication.
- Password hashing using bcrypt.
- CSRF and origin checks.
- Bus access-control lists.
- Peer credential verification.
- Optional HMAC authentication.
- Request size restrictions.
- Replay detection.
- Sensitive-data redaction.
- Fail-closed validation of unsupported or invalid evidence.
The architecture also separates external device-facing components from Core's authoritative information store.
However, these controls have not been independently audited as a complete deployed system.
SYNORA is not currently security-certified or production-hardened.
The current deployment configuration contains privileged services and media-access assumptions that require additional review before exposure to untrusted networks.



9. Getting Started
SYNORA can be compiled and most component tests can be executed on a Linux development system without connected IoT hardware.
Requirements
- Linux
- Go 1.24 or newer
- Python 3
- GNU Make
- Bash
- Git
Node.js and npm are required for the web client.
Additional model artifacts and hardware-specific dependencies are needed for advanced vision processing and full-system demonstrations.
Clone the repository
git clone https://github.com/ANLEXISS/Synora.git
cd Synora
Compile Go components
go build -buildvcs=false ./...
Run Python tests
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=services/vision-worker python3 -B -m unittest discover -s services/vision-worker/tests -p 'test_*.py'
Run Go tests
go test ./...
Note: the complete Go test command is expected to fail at the audited commit because the central harness requires missing model bundles.
To reproduce component-level validation without installing system services, follow the instructions in the dedicated documentation.
Complete setup instructions
See [Getting Started](docs/GETTING_STARTED.md).
Avoid running installation or network configuration targets without first reviewing their effects on the host system.



10. Current Limitations
SYNORA is a functional software prototype with incomplete integration and deployment qualification.
The principal known limitations include:
- Missing cognitive model bundles required for central qualification.
- Incomplete API service deployment configuration.
- Conflicting default HTTP port ownership between Discovery and the separate API executable.
- External MediaMTX executable not bundled with the repository.
- Missing hardware-dependent inference artifacts.
- Lack of complete real-camera and NPU validation.
- Incomplete frontend build and test reproduction.
- No current sustained resource or load benchmarks.
- No independent production security review.
- No validated physical actuator execution.
The cognitive Safety Gate currently operates through dry-run execution boundaries.
These limitations are documented to distinguish implemented capabilities from experimentally supported or planned features.



11. Development Roadmap
Priority 1 — Reproducible Integration
- Make central cognitive model dependencies reproducible.
- Restore successful full-suite validation.
- Resolve API service ownership and deployment topology.
- Introduce automated validation reporting through CI.
Priority 2 — Hardware Qualification
- Validate a complete camera-to-Core processing path.
- Test MediaMTX with real streams.
- Qualify supported ARM64/RK3588 workloads.
- Measure resource usage and recovery behavior.
- Validate failure scenarios under realistic conditions.
Priority 3 — Operational Reliability
- Improve deployment reproducibility.
- Review privileged service boundaries.
- Audit network and media access controls.
- Add sustained load and resource testing.
- Improve monitoring and service recovery.
Priority 4 — Contextual Intelligence
- Expand validated cognitive processing capabilities.
- Evaluate structured representations of system information.
- Explore relationships between devices, events and environmental observations.
- Investigate more advanced local reasoning and automation mechanisms.
Advanced cognitive features remain experimental or planned until their implementations and results can be independently reproduced.



12. What This Project Demonstrates
SYNORA is not simply an exercise in implementing smart-home functionality.
It is an engineering project involving several interconnected software disciplines.
Software Architecture
Designing service boundaries, defining responsibilities and establishing communication contracts.
Backend Engineering
Implementing Go services, APIs, persistent storage and system coordination.
Distributed Systems
Managing concurrency, communication failures, access control and process separation.
Data Integrity
Implementing consistent state ownership, journal-based persistence and idempotent operations.
Python Integration
Separating specialized processing workloads from the main software platform.
Testing & Validation
Building component tests, integration scenarios, contract checks and explicit failure reporting.
Engineering Decision-Making
Evaluating architectural alternatives through their benefits, limitations, operational costs and implementation complexity.
The project reflects a broader engineering principle:
Understand the problem, define its constraints, design the simplest appropriate solution, and continuously evaluate the balance between performance, reliability, complexity and cost.



13. Technical Documentation
The repository contains three complementary technical documents.
[System Architecture](docs/ARCHITECTURE.md)
Detailed service responsibilities, communication protocols, data flows, storage ownership, design decisions and architectural trade-offs.
[Getting Started](docs/GETTING_STARTED.md)
Development prerequisites, build commands, testing procedures, configuration requirements and known setup limitations.
[Validation Report](docs/VALIDATION_REPORT.md)
Audited test results, component maturity, identified failures, security limitations and recommended next validation steps.
These documents describe the codebase as inspected on 22 November 2026.



14. Project Information
Author: Alexis Kratz
GitHub: https://github.com/ANLEXISS
Repository: https://github.com/ANLEXISS/Synora
Development status: Active
Project areas: Software architecture, distributed systems, backend development, Linux, local-first computing, IoT and applied AI.



15. License
Consult the repository's license and contribution files for applicable terms.
The project is currently maintained as a personal software engineering and research initiative.
