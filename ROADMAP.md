# Public Roadmap

**Last Updated**: May 2026  
**Current Version**: v3.5.17 (May 20, 2026)  
**Status**: Actively Developed

This roadmap outlines Docker Secret Operator's direction over the next 6-12 months. It reflects community feedback, security requirements, and operational priorities.

---

## Vision for 2026

**DSO will become the standard secret injection solution for Docker Compose applications**, whether in development or production, by combining:

- ✅ **Zero-Disk Security** — Secrets never touch disk
- ✅ **Cloud-Agnostic** — Works with AWS, Azure, Vault, Huawei, or local storage
- ✅ **Zero-Downtime Rotation** — Seamless secret updates
- ✅ **Simple Setup** — 5-minute local mode, automated cloud mode
- ✅ **Enterprise-Ready** — Audit logging, compliance, multi-tenancy support

---

## Current Status (v3.5.17 - May 2026)

### ✅ Completed Features

**Core Functionality**:
- ✅ Local mode (encrypted vault, single-host)
- ✅ Agent mode with cloud providers (AWS, Azure, Vault, Huawei)
- ✅ Socket-based IPC for non-root access
- ✅ Zero-downtime rolling rotation
- ✅ Crash recovery and state persistence

**Security & Operations**:
- ✅ AES-256-GCM encryption (local mode)
- ✅ TLS for all provider communication
- ✅ Log redaction (no secrets in logs)
- ✅ Threat model documentation
- ✅ Security.md with guarantees
- ✅ Docker V2 Secret Driver support

**Developer Experience**:
- ✅ Comprehensive documentation
- ✅ Local & cloud mode guides
- ✅ CLI with 30+ commands
- ✅ Health checks (`docker dso doctor`)
- ✅ Real-time monitoring (`docker dso watch`)
- ✅ Multiple secret injection methods (env, file)

**Testing & Quality**:
- ✅ Unit tests (core packages >80%)
- ✅ Integration tests
- ✅ CI/CD pipeline
- ✅ Code scanning (SAST)
- ✅ Dependency checks

---

## Roadmap: Next 6 Months (Jun-Dec 2026)

### Q2 2026 (Jun-Aug): CNCF Sandbox & Enterprise Foundation

**Focus**: CNCF Sandbox submission, enterprise hardening

#### Must-Have ✓
- [ ] **CNCF Sandbox Submission** (June)
  - Status: Preparing documentation
  - Deliverable: Full submission with governance, roadmap, security docs
  - Impact: Higher visibility, community contribution

- [ ] **Code Coverage Pipeline** (June-July)
  - Status: Setting up Codecov integration
  - Target: 70% overall, 85% for critical paths
  - Impact: Improved quality assurance

- [ ] **Secret Rotation Hooks** (July-Aug)
  - Status: Designing extensibility
  - Feature: Pre/post-rotation webhooks for compliance logging
  - Impact: Audit logging for enterprises

- [ ] **Comprehensive Audit Logging** (Aug)
  - Status: RFC in discussion
  - Feature: All secret operations logged with timestamps, users, reasons
  - Impact: HIPAA/PCI compliance

#### Nice-to-Have
- [ ] **Docker Swarm Support** (Optional)
  - Status: Community request
  - Scope: Support Docker Swarm mode in addition to docker-compose
  - Effort: Medium (2-3 weeks)

---

### Q3 2026 (Sep-Nov): Enterprise & Multi-Tenancy

**Focus**: Multi-tenancy, RBAC, audit

#### Must-Have ✓
- [ ] **Multi-Tenant Architecture** (Sep-Oct)
  - Status: Designing namespacing
  - Feature: Isolate secrets by namespace/project
  - Impact: Shared infrastructure support

- [ ] **Role-Based Access Control (RBAC)** (Oct-Nov)
  - Status: RFC planned
  - Feature: Admin, operator, viewer roles with fine-grained permissions
  - Impact: Enterprise security policies

- [ ] **Enhanced Audit Logging** (Nov)
  - Status: Building on logging from Q2
  - Feature: Queryable audit logs, export to Splunk/ELK
  - Impact: Compliance & forensics

- [ ] **Policy Engine** (Stretch)
  - Status: Early design
  - Feature: Define rotation policies (frequency, strategy, targets)
  - Impact: Centralized secret management

#### Nice-to-Have
- [ ] **Metrics Export** (Prometheus)
  - Status: Design
  - Feature: Expose rotation metrics, cache stats to Prometheus
  - Impact: Observability

---

### Q4 2026 (Dec 2026 - Jan 2027): Polish & v4.0 Planning

**Focus**: Stabilization, performance, v4.0 strategy

#### Must-Have ✓
- [ ] **Performance Optimization** (Dec)
  - Status: Baseline established
  - Focus: Sub-second rotation, optimized polling
  - Impact: Production readiness

- [ ] **v4.0 Strategy RFC** (Jan 2027)
  - Status: Planning
  - Decision: Breaking changes needed? (API, config format)
  - Impact: Direction for 2027

#### Nice-to-Have
- [ ] **Kubernetes Native Integration** (Optional)
  - Status: Community interest assessment
  - Scope: Native K8s support (Operators, CRDs)
  - Note: Major effort, deferred to 2027 if needed

- [ ] **Provider Plugins** (Optional)
  - Status: Design phase
  - Feature: Custom provider plugins
  - Impact: Third-party integrations

---

## Roadmap: Next 12 Months (2027)

### v4.0 (2027)

Tentative major release focusing on:
- Cloud-native features (Kubernetes support)
- Advanced multi-tenancy
- Breaking API improvements (if needed)
- Further performance optimization

**Depends on**: Community feedback, production usage patterns, resource availability

---

## Community-Requested Features (Backlog)

These are frequently requested and under consideration:

### High Priority
- 🟨 **Kubernetes Integration** (requested by 20+ users)
  - Status: Early research phase
  - Challenge: Significantly different architecture
  - Timeline: Possible in v4.0 (2027)

- 🟨 **Encrypted Backups** (enterprise request)
  - Status: Designing backup/restore mechanism
  - Use Case: Disaster recovery, vault portability
  - Effort: Medium (3-4 weeks)

- 🟨 **Custom Providers via SDK** (developer request)
  - Status: RFC planned for Q3
  - Use Case: Integration with proprietary secret systems
  - Effort: High (6-8 weeks)

### Medium Priority
- 🟦 **Docker Swarm Support** (niche use case)
  - Status: Waiting for demand
  - Effort: Medium
  - Timeline: If there's demand, Q3 2026

- 🟦 **GUI Dashboard** (nice-to-have)
  - Status: Low priority, community contribution welcome
  - Use Case: Visual secret management
  - Effort: High

- 🟦 **Automatic Secret Generation** (advanced feature)
  - Status: Design phase
  - Use Case: Generate passwords, API keys automatically
  - Effort: Medium-high

### Low Priority
- 🟩 **Slack Integration** (nice-to-have)
- 🟩 **Webhook Support** (partial: rotation hooks in Q2)
- 🟩 **Metrics Dashboard** (would use Grafana)
- 🟩 **Web UI** (community contribution?)

---

## Won't Do (Explicit Non-Goals)

To stay focused, DSO explicitly does **not** aim to:

- ❌ **Kubernetes-First** — DSO is for Docker Compose / single-host. K8s has ExternalSecrets.
- ❌ **GitOps Tool** — Not a deployment tool, doesn't manage infrastructure.
- ❌ **Secrets Generation** — Doesn't create initial credentials (use your provider or manual setup).
- ❌ **SSL/TLS Cert Management** — Use Cert-Manager or your provider.
- ❌ **Full Compliance Suite** — Compliance is shared responsibility (policy, monitoring, audit logging included).

---

## Quality & Performance Goals

### Code Quality (2026)
- 📊 Code coverage: 70%+ (core), 85%+ (critical packages)
- 🔒 Zero unpatched CVEs (security patches within 24h)
- 📈 No memory leaks on 30-day+ deployments
- ⚡ Sub-second rotation (P99 < 1s)

### Testing Standards
- ✅ Unit tests for all new code (>80% coverage)
- ✅ Integration tests for provider integrations
- ✅ Chaos testing for crash recovery
- ✅ Load testing (1000+ rotating secrets)

### Performance Targets
- ✅ Local mode: Setup <1s
- ✅ Agent mode: Setup <5s (cloud detection)
- ✅ Rotation: <30s (blue-green swap + health check)
- ✅ Secret fetch: <100ms (from cache), <1s (from provider)

---

## Dependencies & Known Constraints

### Resource Limitations
- **Time**: 1-2 FTE lead maintainers (Umair)
- **Infrastructure**: Using GitHub (free tier), no cloud resources yet
- **Testing**: Limited access to some cloud providers

### Technical Debt
- [ ] Provider plugin system needs refactoring (Q3)
- [ ] Configuration merging logic could be simpler (Q2)
- [ ] Some race conditions in state machine (rare, Q2 fixes)

### External Factors
- **Docker/Moby**: New security features or APIs may enable better integration
- **Cloud Providers**: API changes in AWS, Azure, etc. require updates
- **Community**: Dependent on adoption and feedback
- **CNCF**: Sandbox requirements may shape priorities

---

## How to Influence This Roadmap

### Request a Feature
1. Open a [Feature Request](https://github.com/docker-secret-operator/dso/issues/new?template=feature.md)
2. Describe your use case and why it matters
3. Upvote similar requests if they exist
4. Discuss in [GitHub Discussions](https://github.com/docker-secret-operator/dso/discussions)

### Vote on Priorities
- 👍 Upvote issues you care about
- 💬 Comment with your use case
- 🤝 Offer to help implement (PRs welcome!)

### Contribute
- 🚀 Implement features yourself
- 🧪 Add tests and documentation
- 🐛 Fix bugs
- 📚 Improve documentation

### Sponsor Development
- 💰 Fund features (contact maintainers)
- 🤵 Contribute developer time
- 🎓 Share knowledge and expertise

---

## Release Schedule

| Version | Release Date | Status |
|---------|-------------|--------|
| v3.5.15 | May 18, 2026 | ✅ Released |
| v3.5.16 | May 19, 2026 | ✅ Released |
| v3.5.17 | May 20, 2026 | ✅ Released (current) |
| v3.6.0 | Jun 30, 2026 | 🔵 Planned (CNCF + features) |
| v3.6.1 | Jul 15, 2026 | 🔵 Planned (bug fixes) |
| v3.7.0 | Sep 30, 2026 | 🔵 Planned (RBAC + multi-tenancy) |
| v4.0.0 | Jan 2027 | 🟡 Tentative (major features) |

---

## Roadmap Review & Updates

This roadmap is reviewed:
- ✅ **Quarterly** — Check progress, adjust priorities
- ✅ **After Major Events** — CNCF acceptance, critical issues
- ✅ **Annually** — Rewrite for next year

### Feedback Loop

**Community Input**: Each quarter we'll:
1. Post a roadmap update in Discussions
2. Ask for feedback on priorities
3. Adjust based on feedback
4. Document decisions

**Next Roadmap Review**: August 2026

---

## Contact & Questions

**Questions about the roadmap?**
- 💬 [GitHub Discussions](https://github.com/docker-secret-operator/dso/discussions)
- 📧 Email: [maintainers@docker-secret-operator.org](mailto:maintainers@docker-secret-operator.org)
- 🐦 [Project updates in announcements](https://github.com/docker-secret-operator/dso/discussions/categories/announcements)

**Want to contribute?**
- See [CONTRIBUTING.md](CONTRIBUTING.md) and [GOVERNANCE.md](GOVERNANCE.md)
- Check out [Help Wanted](https://github.com/docker-secret-operator/dso/issues?q=label%3A%22help+wanted%22) issues
- Join the next community sync (TBD)

---

## Acknowledgments

This roadmap reflects:
- 📋 User feedback and feature requests
- 🐛 Bug reports and production experience
- 🤝 Community contributions
- 💡 Maintainer vision and expertise
- 🎯 CNCF Sandbox requirements

**Thank you to everyone who makes DSO better! 🙏**

---

**Maintained by**: Umair (Project Lead)  
**Last Updated**: May 2026  
**Next Review**: August 2026  
**Status**: Active ✅
