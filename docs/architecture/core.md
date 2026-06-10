# DSO Core Platform

The core platform represents the stable, production-ready foundation of Docker Secret Operator.

## Stable Capabilities

The core layer provides fundamental runtime secret injection and management capabilities:

- **Secrets**: Secret storage, retrieval, and lifecycle management
- **Execution Engine**: Orchestration of secret injection workflows, execution planning, and verification
- **Scheduler**: Job scheduling for periodic secret rotation and maintenance tasks
- **Auth & RBAC**: Authentication, authorization, role-based access control, and identity management
- **Audit**: Complete audit logging of all operations (append-only, immutable)
- **Metrics**: Operational metrics collection, aggregation, and export
- **Backup & Recovery**: Snapshot-based backups with point-in-time recovery
- **Alerts**: Alert rules, trigger conditions, and notification delivery
- **Plugin Framework**: Extensibility through plugins with lifecycle management
- **Integrations**: Third-party integrations (webhooks, webhooks, external systems)
- **Embedded UI**: Next.js-based web dashboard for operations and management

## Stability Guarantees

The core layer:

- ✓ Maintains backward compatibility
- ✓ Must not depend on advanced or intelligence layers
- ✓ Gracefully handles failures in other layers
- ✓ Can be deployed without optional subsystems
- ✓ Subject to CNCF review and certification

## Branch

Core functionality exists on:
- `main` (stable, under CNCF review)
- `feature/web-ui` (active development)

## Long-Term Support

The core platform is the foundation for DSO's future evolution. All features in this layer are candidates for long-term support and production use.
