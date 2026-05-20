# Governance

This document describes the governance model for Docker Secret Operator (DSO), including how decisions are made, how the community is structured, and how to participate.

---

## Vision & Values

**DSO Vision**: Simplify secret injection and rotation for Docker Compose applications without requiring cloud infrastructure, complex CI/CD pipelines, or specialized knowledge.

**Core Values**:
- 🔒 **Security First** — Secrets must never touch disk in plaintext
- 🤝 **Community-Driven** — Decisions shaped by users and contributors
- 📖 **Transparency** — Public roadmap, open discussions, clear processes
- ⚡ **Simplicity** — Minimal dependencies, easy setup, sensible defaults
- 🔄 **Compatibility** — Backward compatible, no breaking changes without major version bump

---

## Governance Structure

### Project Roles

DSO uses a merit-based governance model with three contributor tiers:

#### Lead Maintainers (✨ Steering)

**Responsibilities**:
- Set project vision and strategy
- Approve major design decisions
- Manage releases and versioning
- Handle governance matters
- Represent DSO at community events

**Powers**:
- Approve/reject RFCs (Requests for Comments)
- Approve/reject major features
- Approve/reject breaking changes
- Create/remove maintainer tiers
- Merge PRs without approval from other maintainers

**Current Lead Maintainers**:
- Umair (Project Lead) — Architecture, design decisions, community leadership

**Expectation**: Quarterly governance check-in, respond to critical issues within 48 hours

#### Core Maintainers (⭐ Active)

**Responsibilities**:
- Review and merge PRs
- Fix bugs and regressions
- Manage releases and documentation
- Answer questions in discussions
- Triage GitHub issues

**Powers**:
- Approve and merge PRs (with another maintainer if significant)
- Create releases (major/minor/patch)
- Approve contributor promotions
- Manage GitHub projects and milestones

**Expectation**: Weekly review activity, respond to PRs within 5 business days

**How to Become Core Maintainer**:
- 10+ merged PRs demonstrating quality and consistency
- 3 months of regular contributions
- Sponsored by existing maintainer
- Approved by lead maintainers
- Commit to governance responsibilities

#### Contributors 👥

**Definition**: Anyone who has submitted a PR or issue to DSO.

**Responsibilities**:
- Follow Code of Conduct
- Provide clear issue reports
- Write quality code/docs
- Participate respectfully in discussions

**Powers**:
- Create issues and PRs
- Participate in discussions
- Suggest features and improvements
- Vote on community polls (non-binding)

---

## Decision-Making Process

### Levels of Decisions

#### 1. Routine Decisions (🟢 Automatic)

**Scope**: Bug fixes, documentation updates, dependency bumps, minor refactoring

**Process**:
- Contributor opens PR
- One core maintainer reviews
- PR merged if approved
- No waiting period required

**Examples**:
- Fix typo in README
- Update GitHub Actions version
- Refactor internal function
- Update CHANGELOG

#### 2. Feature Decisions (🟡 Discussion)

**Scope**: New features, API changes, rotation strategy improvements

**Process**:
1. Author opens GitHub Discussion or Issue
2. Community discusses (minimum 3 days)
3. Core maintainers synthesize feedback
4. Lead maintainer approves/rejects
5. Implementation proceeds with reviews

**Timeline**: 1-2 weeks from discussion to decision

**Examples**:
- New secret injection method
- New provider integration
- API change proposal
- Performance optimization

#### 3. Strategic Decisions (🔴 RFC)

**Scope**: Major features, governance changes, breaking changes, roadmap priorities

**Process**:
1. Author writes RFC (Request for Comments) in Discussions
2. Community comments (minimum 7 days)
3. Core maintainers review feedback
4. Lead maintainers vote
5. Decision documented in GOVERNANCE

**Timeline**: 2-4 weeks from RFC to decision

**Examples**:
- Move from Docker Compose to Kubernetes
- Redesign secret storage format
- Change license
- Merge with another project
- Major version release

### Voting

**Quorum**: At least 50% of active core maintainers

**Approval**: Simple majority (50% + 1) for features, 2/3 for governance changes

**Voting Period**: 48 hours for routine, 7 days for strategic

---

## Release Process

### Versioning

DSO follows [Semantic Versioning](https://semver.org/):

```
MAJOR.MINOR.PATCH(-prerelease)(+metadata)
  3  .  5  .  17
```

- **MAJOR**: Breaking changes (v3 → v4)
- **MINOR**: New features, backward compatible (v3.5 → v3.6)
- **PATCH**: Bug fixes only (v3.5.17 → v3.5.18)

### Release Cadence

- **Security patches**: As needed (typically within 24 hours)
- **Bug fix releases**: Every 1-2 weeks
- **Feature releases**: Every 4-6 weeks
- **Major releases**: Yearly (or as needed)

### Release Checklist

Before releasing:

```
[ ] Merge all approved PRs
[ ] Update CHANGELOG.md
[ ] Update version numbers (version.go, package.json)
[ ] Run full test suite
[ ] Run security scan
[ ] Build release artifacts
[ ] Create GitHub Release
[ ] Tag git commit
[ ] Push to main
[ ] Announce in discussions
```

### Security Patch Process

1. **Report**: Email security@docker-secret-operator.org
2. **Confirm**: Acknowledge within 48 hours
3. **Fix**: Create fix in private fork
4. **Review**: Core maintainers review
5. **Release**: Publish patch + advisory
6. **Credit**: Thank reporter (unless they request anonymity)

---

## Code Review Standards

### PR Review Requirements

| Change Type | Reviewers Needed | Timeline |
|-------------|------------------|----------|
| Documentation | 1 | 3 days |
| Bug fix | 1 | 5 days |
| Small feature | 2 | 7 days |
| Large feature | 2 + lead | 10 days |
| API change | 2 + lead | 14 days |
| Security fix | 2 + lead | ASAP |

### Code Review Guidelines

✅ **Good Reviews**:
- Test the code locally
- Check for security implications
- Verify documentation updates
- Confirm tests are adequate
- Provide constructive feedback

❌ **Poor Reviews**:
- Approving without testing
- Blocking on style preferences
- Demanding unnecessary rewrites
- Making decisions unilaterally
- Missing security concerns

### Addressing Review Feedback

- **Acknowledge**: Thank reviewer for feedback
- **Explain**: If disagreeing, explain reasoning
- **Iterate**: Make changes or request discussion
- **Escalate**: Ask lead maintainer if blocked
- **Merge**: Once approved, maintainer merges

---

## Conflict Resolution

### Levels of Escalation

**Level 1: Discussion** (Default)
- Author and reviewer discuss in PR comments
- Aim for consensus through dialogue
- Timeline: 48 hours

**Level 2: Maintainer Mediation**
- Ask third core maintainer to weigh in
- Present both perspectives fairly
- Timeline: 5 business days

**Level 3: Lead Maintainer Decision**
- Lead maintainer makes final call
- Decision is binding
- Documented in PR
- Timeline: 48 hours

### Examples

**Dispute**: "Should we support Go 1.17?"
→ Level 1: Discuss tradeoffs (maintenance burden, user base)
→ If stuck → Level 2: Ask another maintainer
→ If still stuck → Level 3: Lead maintainer decides

**Dispute**: "Is this API design consistent?"
→ Level 1: Review API RFC and design guidelines
→ If not clear → Level 2: Ask architecture reviewer
→ If blocking → Level 3: Lead maintainer decides

### Appealing Decisions

If you disagree with a decision:

1. **Document your reasoning** in a GitHub Discussion
2. **Request reconsideration** with new information (if applicable)
3. **Wait 30 days** for community feedback
4. **Escalate to lead maintainers** if you believe the decision violates DSO values

---

## Contribution Ladder

DSO recognizes contributions in many forms:

### Level 0️⃣: Everyone
- File issue with details
- Participate in discussions
- Try DSO and provide feedback

### Level 1️⃣: Regular Contributors
- 3+ merged PRs
- Attend discussions
- Help answer questions
- Recognized in CONTRIBUTORS.md

**Promotion**: Automatic after 3 merged PRs

### Level 2️⃣: Core Contributors
- 10+ merged PRs
- Own a subsystem (CLI, agents, providers)
- Review other PRs consistently
- Participate in governance

**Promotion**: Requires core maintainer sponsorship + lead approval

### Level 3️⃣: Core Maintainers
- 20+ merged PRs
- 3+ months of consistent work
- Demonstrate judgment and communication skills
- Sponsor own PRs

**Promotion**: Requires lead maintainer decision

### Level 4️⃣: Lead Maintainers
- Strategic vision and execution
- Community leadership
- Final decision authority

**Promotion**: Requires unanimous lead maintainer vote

---

## Communication Channels

### Synchronous

- **GitHub Issues**: Report bugs, request features, discuss proposals
- **GitHub Discussions**: Q&A, announcements, community conversation
- **GitHub PRs**: Code review and merged discussions

### Asynchronous

- **Email**: security@docker-secret-operator.org for security issues
- **Slack** (if applicable): For real-time discussion and collaboration

### Office Hours (Future)

- Monthly video call for community questions
- TBD timing based on community feedback
- Recorded and posted to discussions

---

## Governance Changes

Changes to this document require:

1. **Proposal**: Open GitHub Discussion with rationale
2. **Discussion**: 7 days of community comment
3. **RFC**: Core maintainers discuss
4. **Vote**: Lead maintainers approve (2/3 majority)
5. **Implementation**: Update document and announce

### Proposed Governance Changes

Currently under discussion:
- Add steering committee (when project reaches 5+ core maintainers)
- Establish technical advisory board
- Create provider governance subgroup

---

## Committee Structures (Future)

As DSO grows, we may establish:

### Technical Steering Committee

**When**: When 5+ core maintainers exist
**Members**: All active core maintainers
**Meetings**: Quarterly
**Purpose**: Strategic technical decisions

### Provider Governance Group

**When**: When 3+ provider integrations reach maturity
**Members**: Provider maintainers + lead maintainers
**Meetings**: Monthly
**Purpose**: Provider API standards, compatibility

### Security Committee

**When**: When security issues increase significantly
**Members**: Security-focused maintainers
**Meetings**: As needed
**Purpose**: Security policy, vulnerability handling, advisories

---

## Transparency & Accountability

### Public Records

DSO maintains transparency through:

- 📋 **Public Roadmap** ([ROADMAP.md](ROADMAP.md)): What's planned, in progress, completed
- 📊 **GitHub Projects**: Milestone tracking and board status
- 📰 **Release Notes**: Detailed CHANGELOG for each release
- 🎤 **Community Discussions**: Decision rationale visible to all
- 📈 **Metrics**: Test coverage, performance, security scanning

### Accountability

- **Maintainers** are accountable to the community
- **Contributors** are accountable for code quality
- **Lead maintainers** are accountable for strategic decisions
- **Everyone** is accountable for Code of Conduct

### Annual Review

Once per year (or when circumstances change):
- Review maintainer activity and engagement
- Update contribution guidelines
- Discuss governance improvements
- Celebrate community growth

---

## FAQ

**Q: How do I get involved?**  
A: Start by reporting issues, answering questions, or submitting a PR. See the Contribution Ladder above.

**Q: What if I disagree with a decision?**  
A: Respectfully raise your concerns in the discussion and follow the escalation process above.

**Q: Can I use DSO for commercial work?**  
A: Yes! DSO is MIT-licensed. You can use it commercially, but must include the license.

**Q: How are decisions made if maintainers disagree?**  
A: We discuss until consensus, escalate to lead maintainer if needed, then document the decision.

**Q: What if a maintainer becomes inactive?**  
A: Core maintainers are expected to maintain activity. After 3 months of inactivity, we'll reach out. After 6 months, we'll transition them to emeritus status.

**Q: Can maintainers be removed?**  
A: Yes, for Code of Conduct violations or abandonment. Removal requires lead maintainer approval.

---

## References

- **[Code of Conduct](CODE_OF_CONDUCT.md)** — Community standards
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — How to contribute
- **[SECURITY.md](SECURITY.md)** — Security policy
- **[ROADMAP.md](ROADMAP.md)** — Development roadmap
- **[MAINTAINERS.md](MAINTAINERS.md)** — Active maintainers list

---

## History

| Date | Event |
|------|-------|
| May 2026 | Governance model v1.0 adopted |

---

**Last Updated**: May 2026  
**Next Review**: May 2027  
**Status**: Active ✅

---

For questions about governance, please open a [Discussion](https://github.com/docker-secret-operator/dso/discussions) or email security@docker-secret-operator.org.

Together we build better secret management! 🚀
