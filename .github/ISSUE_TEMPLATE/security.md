---
name: Security Issue
about: Report a security vulnerability (keep it confidential)
title: "[SECURITY] "
labels: security
---

## ⚠️ SECURITY ISSUE REPORT

**PLEASE DO NOT DISCUSS SENSITIVE DETAILS IN PUBLIC ISSUES**

Instead, email: **security@docker-secret-operator.org**

Include:
- Detailed description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested mitigation (if available)

---

## Quick Summary

<!-- Very brief, non-technical summary suitable for public viewing -->

**Severity:**
- [ ] Critical (exploit available, secrets exposed)
- [ ] High (feasible exploit, significant impact)
- [ ] Medium (limited exploitation, moderate impact)
- [ ] Low (theoretical, limited impact)

---

## Disclosure Timeline

When you email security@docker-secret-operator.org, we will:

1. **Acknowledge receipt** within 48 hours
2. **Begin investigation** immediately
3. **Provide status updates** every 7 days
4. **Release fix** as soon as possible (typically within 30 days)
5. **Coordinate disclosure** with security advisory
6. **Credit your discovery** (unless you prefer anonymity)

---

## Responsible Disclosure Guidelines

- Give maintainers 90 days to patch before public disclosure
- Don't test exploit on production systems
- Don't access data beyond what's necessary to confirm the issue
- Don't modify or delete data
- Avoid disrupting DSO operations or other users' services

---

## What We Need

When reporting a security issue, include:

1. **Type of vulnerability**
   - [ ] Authentication bypass
   - [ ] Authorization bypass
   - [ ] Secret exposure
   - [ ] Crypto vulnerability
   - [ ] Injection attack
   - [ ] DOS / Resource exhaustion
   - [ ] Other: ________

2. **Affected versions**

3. **Environment details** (OS, Docker version, deployment mode)

4. **Steps to reproduce** (concise, sanitized version)

5. **Impact assessment**
   - What can an attacker do?
   - Who is affected?
   - How many users/deployments are vulnerable?

6. **Suggested mitigation** (if you have one)

---

## What Happens Next

Once the security team receives your report:

✓ We will confirm receipt  
✓ We will begin investigation  
✓ We will assign a CVE if needed  
✓ We will prepare a security patch  
✓ We will release a public advisory  
✓ We will credit your discovery  

---

## Thank You

Thank you for helping keep DSO secure!

Your responsible disclosure helps protect all users.

🛡️ **Security is a shared responsibility**

---

## Public Issue Tracker

For non-security bugs and features, use the regular issue tracker:
→ https://github.com/docker-secret-operator/dso/issues

