# CLAUDE.md

## Modes

### PLAN MODE (trigger: "plan:" or "/plan")

Output ONLY:

Goal: <one line>

Files:
- path/file.ts — change reason

Steps:
- action + file
- action + file

Risks:
- short bullets

Ready: yes/no

Rules:
- No code
- No explanations
- No extra text
- No assumptions
- Stop after output
- Do not create files during planning
- Do not propose unnecessary files
- **Never** generate extra `.md` files unless explicitly requested
- **Never** create backup, temp, draft, example, or duplicate files

Wait for confirmation before implementation.

---

### ACT MODE (default)

Format:
- reading path/file.ts
- editing path/file.ts — <what changed>
- creating path/file.ts — <reason>
- deleting path/file.ts
- running <command>
- error: <one line>
- retry: <one line>
- done <file:line>

Rules:
- One line per step
- No paragraphs
- No explanations
- No filler words
- No summaries
- Create files only when strictly required
- **Never** create unnecessary `.md` files (e.g., status, analysis, or summary files)
- **Never** create duplicate, backup, temp, example, or unused files
- **Never** generate placeholder files
- Do not add documentation files unless explicitly requested
- Reuse existing files whenever possible

---

## Global Rules: Token Efficiency & Focus

- **Logic > Documentation**: Focus exclusively on fixing code and implementing features.
- **No Meta-Docs**: Never create analysis, status, roadmap, or summary `.md` files unless the user asks for a report.
- **Output minimal tokens**: Be extremely brief. Use "surgical" edits to minimize output size.
- **Never explain** unless asked.
- **Never repeat** user input.
- **Never write full files** unless asked or the file is new.
- **Prefer diff** over full code snippets.
- If change ≤3 lines → show exact lines only.
- If ambiguous → ask ONE question, then stop.
- Preserve existing architecture.
- Avoid unrelated changes.
- Never touch unrelated files.

---

## Code Rules

- Surgical edits only.
- No refactors unless asked.
- No comments unless requested.
- No formatting-only changes.
- Keep existing structure.
- Preserve naming conventions.
- Avoid unnecessary dependencies.

---

## File Creation Rules

- Create new files only if implementation requires them.
- **Prohibited Files**: `backup.*`, `temp.*`, `draft.*`, `example.*`, `ANALYSIS.md`, `SUMMARY.md`, `PROGRESS.md`, etc.
- Prefer modifying existing files over creating new ones.

---

## Logging Rules

- Log only meaningful actions
- Skip trivial steps
- No duplicate logs
- Keep logs concise

---

## Error Handling

- error: <what failed>
- fix: <next action>

If blocked:
- blocked: <reason>

---

## Done Criteria

- done <what changed> in <file:line>
- Stop immediately after done.
