---
name: Bug Report
about: Report a bug or issue in DSO
title: "[BUG] "
labels: bug
assignees: ''
---

## Description

<!-- Clear and concise description of the bug -->

**What's the expected behavior?**

<!-- What should happen -->

**What's the actual behavior?**

<!-- What actually happens -->

---

## Environment

**DSO Version:**
<!-- e.g., v3.5.17 -->

**Deployment Mode:**
- [ ] Local Mode
- [ ] Agent Mode

**If Agent Mode, which provider?**
- [ ] AWS Secrets Manager
- [ ] Azure Key Vault
- [ ] HashiCorp Vault
- [ ] Huawei Cloud KMS
- [ ] Other: ________

**Host OS:**
<!-- e.g., Linux Ubuntu 22.04 -->

**Docker Version:**
<!-- Run: docker version -->

**Go Version (if applicable):**
<!-- Run: go version -->

---

## Steps to Reproduce

1. 
2. 
3. 

---

## Error Output / Logs

```
# Run: docker dso doctor --level full
# Paste relevant logs below:

```

---

## docker-compose.yml (if applicable)

```yaml
# Paste your docker-compose.yml here
# Remove any sensitive information!

```

---

## Configuration (dso.yaml excerpt, if applicable)

```yaml
# Paste relevant dso.yaml config here
# REMOVE all secrets, tokens, passwords!

```

---

## Troubleshooting Checklist

- [ ] Ran `docker dso doctor` - all checks passed
- [ ] Checked logs with `docker compose logs -f`
- [ ] Verified secrets exist with `docker dso secret list`
- [ ] Confirmed socket exists: `ls -la /run/dso/dso.sock`
- [ ] Searched closed issues - bug not already reported
- [ ] Tried with latest version of DSO

---

## Additional Context

<!-- Any other context, screenshots, or information -->

<!-- 
Thank you for reporting! We'll investigate and prioritize based on impact.
If this is a security issue, please use the security report template instead.
-->
