# OpenClaw Security Vulnerabilities Research

> Last Updated: 2025
> Compiled from public security advisories, CVE databases, and security research publications.

---

## Executive Summary

OpenClaw, the popular open-source AI agent platform, has faced significant security scrutiny since its release. Security researchers have identified multiple critical vulnerabilities ranging from remote code execution (RCE) to data leakage and prompt injection attacks.

**Key Statistics:**
- **Total CVEs Filed:** 6+ (as of 2026)
- **Critical Severity:** 2
- **High Severity:** 4+
- **Patched:** All publicly disclosed vulnerabilities
- **Active Exploitation:** No confirmed widespread exploitation at time of disclosure

---

## Vulnerabilities List

### 🔴 Critical Severity (Patched)

#### 1. CVE-2026-25253 - Authentication Token Exfiltration / RCE
| Attribute | Details |
|-----------|---------|
| **CVSS Score** | 8.8 (High) |
| **CWE** | CWE-669: Incorrect Resource Transfer Between Spheres |
| **Discovery** | Mav Levin (DepthFirst Research) |
| **Disclosure Date** | February 3, 2026 |
| **Fixed Version** | 2026.2.25+ |
| **Status** | ✅ Patched |

**Description:** A critical vulnerability that allowed attackers to steal authentication tokens via malicious links. When a victim visited an attacker-controlled webpage, the browser would initiate an outbound connection to the victim's OpenClaw gateway, enabling token exfiltration. Attackers could then connect to the victim's local gateway, modify sandbox settings and tool policies, and achieve one-click remote code execution.

**Impact:** Full agent takeover, RCE, credential theft

---

#### 2. ClawJacked - Full Agent Takeover Vulnerability
| Attribute | Details |
|-----------|---------|
| **CVSS Score** | Critical |
| **Discovery** | Oasis Security |
| **Disclosure Date** | February 2026 |
| **Fixed Version** | 2026.2.25+ |
| **Status** | ✅ Patched |

**Description:** This vulnerability allowed complete compromise of an OpenClaw instance. Attackers could achieve full agent takeover through a single malicious link. The vulnerability exploited weaknesses in how OpenClaw handled gateway URLs and authentication tokens.

**Impact:** Complete system compromise, data exfiltration

---

### 🟠 High Severity (Patched)

#### 3. CVE-2026-25157 - OS Command Injection (SSH)
| Attribute | Details |
|-----------|---------|
| **CVE ID** | CVE-2026-25157 |
| **GitHub Advisory** | GHSA-q284-4pvr-m585 |
| **Discovery** | Endor Labs |
| **Fixed Version** | 2026.1.29+ |
| **Status** | ✅ Patched |

**Description:** OS command injection vulnerability in OpenClaw/Clawdbot macOS application's SSH handling. Improperly escaped inputs in the `sshNodeCommand` function allowed attackers to execute arbitrary commands on the target system.

**Impact:** Remote command execution on host system

---

#### 4. Path Traversal in Browser Upload
| Attribute | Details |
|-----------|---------|
| **Severity** | High |
| **CVSS** | 7.6 |
| **Status** | ✅ Patched |

**Description:** Path traversal vulnerability in OpenClaw's browser file upload functionality. Attackers could potentially escape upload directories to write files to arbitrary locations.

**Impact:** File system access, potential RCE

---

#### 5. SSRF in Image Tool
| Attribute | Details |
|-----------|---------|
| **Severity** | High |
| **CVSS** | 7.6 |
| **GitHub Advisory** | GHSA-56f2-hvwg-5743 |
| **Status** | ✅ Patched |

**Description:** Server-Side Request Forgery (SSRF) vulnerability in OpenClaw's image processing tool. Allowed attackers to make the server perform requests to internal services or cloud metadata endpoints.

**Impact:** Internal network access, cloud credential theft

---

#### 6. SSRF in Urbit Authentication
| Attribute | Details |
|-----------|---------|
| **Severity** | High |
| **Status** | ✅ Patched |

**Description:** Similar SSRF vulnerability in the Urbit authentication module, allowing attackers to probe internal network resources.

---

#### 7. Twilio Webhook Authentication Bypass
| Attribute | Details |
|-----------|---------|
| **Severity** | Medium-High |
| **Status** | ✅ Patched |

**Description:** Authentication bypass in Twilio webhook handling that could allow unauthorized access to webhook endpoints.

---

### 🟡 Medium Severity (Patched)

#### 8. Prompt Injection via Malicious Skills
| Attribute | Details |
|-----------|---------|
| **Severity** | Medium-High |
| **Research** | Cisco Talos / Other researchers |
| **Status** | ✅ Patched (with security scanner improvements) |

**Description:** OpenClaw's skill system was vulnerable to prompt injection attacks. Researchers demonstrated that malicious skills could leak sensitive data across user sessions and through IM channels. Testing with the "What Would Elon Do?" skill revealed 9 security findings, including 2 critical and 5 high-severity issues.

**Impact:** Data leakage, session contamination

---

#### 9. Data Leakage Across User Sessions
| Attribute | Details |
|-----------|---------|
| **Severity** | Medium |
| **Status** | 🏗️ Architectural Issue |

**Description:** Architectural weaknesses in OpenClaw's Control UI and session management allowed sensitive data to potentially leak across user sessions. This is a fundamental design consideration rather than a simple patch.

**Impact:** Cross-session data exposure

---

## Summary Table

| CVE/ID | Vulnerability | Severity | Status |
|--------|---------------|----------|--------|
| CVE-2026-25253 | Authentication Token Exfiltration | Critical (8.8) | ✅ Patched |
| ClawJacked | Full Agent Takeover | Critical | ✅ Patched |
| CVE-2026-25157 | OS Command Injection (SSH) | High | ✅ Patched |
| - | Path Traversal (Upload) | High (7.6) | ✅ Patched |
| - | SSRF (Image Tool) | High (7.6) | ✅ Patched |
| - | SSRF (Urbit Auth) | High | ✅ Patched |
| - | Twilio Webhook Bypass | Medium-High | ✅ Patched |
| - | Prompt Injection | Medium-High | ✅ Patched |
| - | Data Leakage (Sessions) | Medium | 🏗️ Architecture |

---

## Recommendations

1. **Update Immediately:** Ensure OpenClaw is running version 2026.2.25 or later
2. **Treat Localhost as Dangerous:** Security researchers recommend treating localhost links with the same suspicion as external phishing links
3. **Run Security Scans:** Organizations should conduct regular pen testing of AI tools
4. **Monitor Advisories:** Subscribe to OpenClaw's security notifications at security@openclaw.ai

---

## Sources

- GitHub Security Advisories: https://github.com/openclaw/openclaw/security
- CVE-2026-25253 Advisory: https://github.com/openclaw/openclaw/security/advisories/GHSA-g8p2-7wf7-98mq
- Cisco Talos Research
- DepthFirst Research
- Endor Labs
- Oasis Security
- The Hacker News
- Cyberdesserts / Cyera Research

---

Below is a **compiled research summary of known security issues in the OpenClaw ecosystem**. The list includes **confirmed vulnerabilities, architectural weaknesses, supply-chain attacks, and large-scale exposures** reported by security researchers and vendors.

The focus is on **real vulnerabilities or security failures**, not just theoretical concerns.

---

# OpenClaw Security Vulnerabilities and Complaints

Summary of past and current issues

## 1. Confirmed vulnerabilities (CVEs and exploit classes)

### 1.1 One-click Remote Code Execution (CVE-2026-25253)

**Severity:** Critical
**Status:** Reported and patched in many builds, but still widely exploitable in unpatched deployments.

**Description**
A vulnerability allows attackers to execute arbitrary code on the host running OpenClaw through a malicious link or instruction chain. ([UToronto Security][1])

**Impact**

* Complete system takeover
* Access to API keys and secrets
* File system access
* Ability to run shell commands

**Evidence**
Security researchers reported that many instances remain vulnerable even after patches. ([Bitdefender][2])

**Observed exposure**

* ~12,812 instances vulnerable initially
* Later scans showed **50,000+ RCE-vulnerable instances** online. ([The Register][3])

---

### 1.2 Credential and API key leakage

**Severity:** High
**Status:** Ongoing architectural risk.

**Description**
OpenClaw has been observed leaking plaintext API keys and credentials due to prompt injection and insecure storage patterns. ([Cisco Blogs][4])

**Impact**

* Stolen OpenAI / LLM API keys
* Access to integrated services
* Potential billing abuse

**Root cause**

* Agents automatically reading prompts from untrusted sources.
* Tools and memory access exposing secrets.

---

### 1.3 Prompt injection leading to agent takeover

**Severity:** Critical
**Status:** Structural vulnerability (not fully solvable).

**Description**
Prompt injection attacks allow external content to modify the agent’s instructions and cause it to reveal secrets or execute tools. ([CrowdStrike][5])

**Impact**

* Data exfiltration
* Tool execution (shell, file system)
* Unauthorized actions

**Why it is serious**
OpenClaw agents can:

* run shell commands
* write files
* access APIs
* control browsers

If prompt-injected, attackers inherit these privileges. ([Cisco Blogs][4])

---

### 1.4 Cross-session data leakage

**Severity:** High
**Status:** Reported in several deployments.

**Description**
Session management issues allowed sensitive data to leak between sessions or channels. ([Giskard][6])

**Impact**

* Users receiving data belonging to other users
* memory store contamination

---

### 1.5 Memory poisoning attacks

**Severity:** High
**Status:** Known architecture problem.

**Description**
Attackers can insert malicious instructions into the agent’s long-term memory store which later influence behavior. ([Passle][7])

**Impact**

* persistent compromise
* altered decision making
* malicious automation

---

## 2. Supply chain vulnerabilities

### 2.1 Malicious skills in ClawHub

**Severity:** Critical
**Status:** ongoing

Security researchers discovered **hundreds of malicious agent skills** distributed in the ecosystem.

**Numbers**

* **341 malicious skills identified**
* Found among **2,857 audited skills**. ([The Hacker News][8])

**Examples**
These skills performed:

* credential harvesting
* downloading malware
* installing Atomic Stealer
* exfiltrating files

**Risk**
Users installing skills grant them system privileges.

---

### 2.2 Skill ecosystem malware campaigns

**Severity:** Critical
**Status:** ongoing

Some reports claim **230+ malicious skills** circulating in the marketplace. ([AuthMind][9])

These attacks mimic legitimate automation tools.

---

## 3. Internet exposure vulnerabilities

### 3.1 Publicly exposed agent servers

**Severity:** Critical

Researchers discovered **tens of thousands of OpenClaw instances exposed to the internet**.

Reported numbers:

* **40,000 exposed instances**
* Later scans showed **135,000+ internet-facing agents**. ([Infosecurity Magazine][10])

**Why this happens**
Default configuration often binds services to public interfaces.

**Impact**

Attackers can:

* connect to agent control panels
* run commands
* inject prompts

---

### 3.2 Unauthorized control panel access

**Severity:** High

Some deployments expose the control UI without authentication.

**Impact**

Attackers can:

* control the agent
* read logs
* trigger tools

This is considered a **misconfiguration vulnerability** rather than a code bug.

---

## 4. Architectural vulnerabilities

These are considered **design risks rather than patchable bugs**.

### 4.1 Tool privilege escalation

OpenClaw agents can:

* run shell commands
* execute scripts
* read/write files

If an attacker compromises the agent, they inherit all privileges. ([Cisco Blogs][4])

---

### 4.2 Autonomous agent attack surface

Researchers describe OpenClaw as a **large attack surface system** because the agent interacts with:

* web pages
* emails
* APIs
* files
* external tools

This exposes it to malicious content automatically. ([techbuddies.io][11])

---

### 4.3 Cascading failure across agents

OpenClaw supports agent-to-agent communication.

Security analysts warn this could enable:

* automated malware propagation
* agent hijacking chains
* coordinated attacks

---

## 5. Security audit findings

A large independent audit of the codebase reported:

**29 critical or high vulnerabilities** across the system. ([GitHub][12])

Examples included:

* unsafe command execution
* improper permission checks
* insecure plugin loading
* secrets exposure

Not all of these are public CVEs yet.

---

# Overall vulnerability statistics

Approximate counts based on public research.

| Category                        | Count                  |
| ------------------------------- | ---------------------- |
| Confirmed CVEs                  | At least 1 major (RCE) |
| High/critical audit findings    | ~29                    |
| Malicious skills discovered     | 230–341                |
| Internet-exposed instances      | 40k–135k               |
| RCE-vulnerable instances online | ~50k                   |

---

# Most common security complaints from users

Security researchers and users repeatedly highlight:

1. Agents have **too much system access**
2. Skills ecosystem lacks **security vetting**
3. Default installs are **internet exposed**
4. Prompt injection can **steal secrets**
5. Agents can run **arbitrary shell commands**
6. Logs and memory may leak sensitive data
7. Supply chain attacks via skills

Many analysts describe the system as a **"security nightmare" if run without strict isolation**. ([Trending Topics][13])

---

# Key takeaway

OpenClaw’s security issues come from two sources:

### Traditional software bugs

Examples:

* RCE
* credential leaks
* unsafe plugin loading

### Architectural agent risks

Examples:

* prompt injection
* tool abuse
* malicious skills
* internet-exposed agents

These architectural risks are **inherent to autonomous AI agents**, meaning they cannot be fully eliminated with patches.

---

If you want, I can also compile:

• a **timeline of OpenClaw security incidents**
• a **comparison with other agent frameworks (AutoGPT, OpenDevin, etc.)**
• a **technical breakdown of the attack surface** for systems like the one you are building (or3-intern).

[1]: https://security.utoronto.ca/advisories/openclaw-vulnerability-notification/?utm_source=chatgpt.com "OpenClaw vulnerability notification - Information Security"
[2]: https://www.bitdefender.com/en-us/blog/hotforsecurity/135k-openclaw-ai-agents-exposed-online?utm_source=chatgpt.com "135K OpenClaw AI Agents Exposed to Internet"
[3]: https://www.theregister.com/2026/02/09/openclaw_instances_exposed_vibe_code/?utm_source=chatgpt.com "OpenClaw instances open to the internet present ripe targets"
[4]: https://blogs.cisco.com/ai/personal-ai-agents-like-openclaw-are-a-security-nightmare?utm_source=chatgpt.com "Personal AI Agents like OpenClaw Are a Security Nightmare"
[5]: https://www.crowdstrike.com/en-us/blog/what-security-teams-need-to-know-about-openclaw-ai-super-agent/?utm_source=chatgpt.com "What Security Teams Need to Know About OpenClaw ..."
[6]: https://www.giskard.ai/knowledge/openclaw-security-vulnerabilities-include-data-leakage-and-prompt-injection-risks?utm_source=chatgpt.com "OpenClaw security issues include data leakage & prompt ..."
[7]: https://ourtake.bakerbotts.com/post/102mfdm/what-is-openclaw-and-why-should-you-care?utm_source=chatgpt.com "What is OpenClaw, and Why Should You Care? - Our Take"
[8]: https://thehackernews.com/2026/02/researchers-find-341-malicious-clawhub.html?utm_source=chatgpt.com "Researchers Find 341 Malicious ClawHub Skills Stealing ..."
[9]: https://www.authmind.com/post/openclaw-malicious-skills-agentic-ai-supply-chain?utm_source=chatgpt.com "OpenClaw's 230 Malicious Skills: What Agentic AI Supply ..."
[10]: https://www.infosecurity-magazine.com/news/researchers-40000-exposed-openclaw/?utm_source=chatgpt.com "Researchers Find 40,000+ Exposed OpenClaw Instances"
[11]: https://www.techbuddies.io/2026/02/02/openclaw-and-the-new-agentic-ai-attack-surface-a-practical-guide-for-security-leaders/?utm_source=chatgpt.com "OpenClaw and the New Agentic AI Attack Surface"
[12]: https://github.com/openclaw/openclaw/issues/8394?utm_source=chatgpt.com "Security Audit: 29 Critical/High Vulnerabilities Identified"
[13]: https://www.trendingtopics.eu/security-nightmare-how-openclaw-is-fighting-malware-in-its-ai-agent-marketplace/?utm_source=chatgpt.com "\"Security Nightmare\": How OpenClaw Is Fighting Malware in Its AI Agent Marketplace"
