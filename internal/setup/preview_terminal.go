package setup

import (
	"fmt"
	"strings"
)

const termDivider = "--------------------------------------------------"

// TerminalRenderer renders an InstallPlan as human-readable terminal output.
// Style mirrors Terraform's plan format: each operation is prefixed with
// + (create), ~ (modify), or - (delete). No ANSI colour codes are emitted.
type TerminalRenderer struct{}

// Render produces a complete plan summary suitable for printing to a terminal.
func (r *TerminalRenderer) Render(plan InstallPlan) (string, error) {
	var b strings.Builder
	r.writeHeader(&b, plan)
	r.writeSummaryBlock(&b, plan)
	r.writeDirectories(&b, plan)
	r.writeFiles(&b, plan)
	r.writePermissions(&b, plan)
	r.writeServices(&b, plan)
	r.writeGroups(&b, plan)
	r.writeFooter(&b, plan)
	return b.String(), nil
}

func (r *TerminalRenderer) writeHeader(b *strings.Builder, plan InstallPlan) {
	fmt.Fprintf(b, "DSO Setup Plan\n\n")
	fmt.Fprintf(b, "Plan ID:        %s\n", plan.ID)
	fmt.Fprintf(b, "Mode:           %s\n", plan.Mode)
	fmt.Fprintf(b, "Provider:       %s\n", plan.Provider)
	if !plan.Timestamp.IsZero() {
		fmt.Fprintf(b, "Generated At:   %s\n", plan.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC"))
	}
	fmt.Fprintln(b)
}

func (r *TerminalRenderer) writeSummaryBlock(b *strings.Builder, plan InstallPlan) {
	s := plan.Summary
	fmt.Fprintf(b, "Summary\n\n")
	fmt.Fprintf(b, "  Directories:  %d\n", len(plan.Directories))
	fmt.Fprintf(b, "  Files:        %d\n", len(plan.Files))
	fmt.Fprintf(b, "  Permissions:  %d\n", len(plan.Permissions))
	fmt.Fprintf(b, "  Services:     %d\n", len(plan.Services))
	fmt.Fprintf(b, "  Groups:       %d\n", len(plan.Groups))
	fmt.Fprintf(b, "  Operations:   %d total (%d create, %d modify, %d delete)\n",
		s.TotalOperations, s.CreateCount, s.ModifyCount, s.DeleteCount)
	if s.RequiresRoot {
		fmt.Fprintf(b, "  Requires:     root\n")
	}
	if s.EstimatedTime > 0 {
		fmt.Fprintf(b, "  Est. Time:    ~%s\n", s.EstimatedTime)
	}
	fmt.Fprintln(b)
}

func (r *TerminalRenderer) writeDirectories(b *strings.Builder, plan InstallPlan) {
	if len(plan.Directories) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\nDirectories\n\n", termDivider)
	for _, d := range plan.Directories {
		fmt.Fprintf(b, "%s %s\n\n", opPrefix(d.Operation), d.ID)
		fmt.Fprintf(b, "  Operation:    %s\n", d.Operation)
		fmt.Fprintf(b, "  Path:         %s\n", d.Path)
		if d.Mode != 0 {
			fmt.Fprintf(b, "  Permissions:  %04o\n", d.Mode)
		}
		if d.Owner != "" {
			fmt.Fprintf(b, "  Owner:        %s\n", d.Owner)
		}
		fmt.Fprintln(b)
	}
}

func (r *TerminalRenderer) writeFiles(b *strings.Builder, plan InstallPlan) {
	if len(plan.Files) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\nFiles\n\n", termDivider)
	for _, f := range plan.Files {
		fmt.Fprintf(b, "%s %s\n\n", opPrefix(f.Operation), f.ID)
		fmt.Fprintf(b, "  Operation:    %s\n", f.Operation)
		fmt.Fprintf(b, "  Path:         %s\n", f.Path)
		if f.Mode != 0 {
			fmt.Fprintf(b, "  Permissions:  %04o\n", f.Mode)
		}
		if f.Owner != "" {
			fmt.Fprintf(b, "  Owner:        %s\n", f.Owner)
		}
		if len(f.Content) > 0 {
			fmt.Fprintf(b, "  Size:         %d bytes\n", len(f.Content))
		}
		fmt.Fprintln(b)
	}
}

func (r *TerminalRenderer) writePermissions(b *strings.Builder, plan InstallPlan) {
	if len(plan.Permissions) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\nPermissions\n\n", termDivider)
	for _, p := range plan.Permissions {
		fmt.Fprintf(b, "~ %s\n\n", p.ID)
		fmt.Fprintf(b, "  Path:         %s\n", p.Path)
		fmt.Fprintf(b, "  From:         %04o\n", p.Current)
		fmt.Fprintf(b, "  To:           %04o\n", p.Target)
		if p.Owner != "" {
			fmt.Fprintf(b, "  Owner:        %s\n", p.Owner)
		}
		fmt.Fprintln(b)
	}
}

func (r *TerminalRenderer) writeServices(b *strings.Builder, plan InstallPlan) {
	if len(plan.Services) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\nServices\n\n", termDivider)
	for _, s := range plan.Services {
		fmt.Fprintf(b, "%s %s\n\n", opPrefix(s.Operation), s.ID)
		fmt.Fprintf(b, "  Operation:    %s\n", s.Operation)
		fmt.Fprintf(b, "  Name:         %s\n", s.Name)
		fmt.Fprintln(b)
	}
}

func (r *TerminalRenderer) writeGroups(b *strings.Builder, plan InstallPlan) {
	if len(plan.Groups) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\nGroups\n\n", termDivider)
	for _, g := range plan.Groups {
		fmt.Fprintf(b, "%s %s\n\n", opPrefix(g.Operation), g.ID)
		fmt.Fprintf(b, "  Operation:    %s\n", g.Operation)
		fmt.Fprintf(b, "  Name:         %s\n", g.Name)
		if len(g.Users) > 0 {
			fmt.Fprintf(b, "  Members:      %s\n", strings.Join(g.Users, ", "))
		}
		fmt.Fprintln(b)
	}
}

func (r *TerminalRenderer) writeFooter(b *strings.Builder, plan InstallPlan) {
	fmt.Fprintf(b, "%s\n\n", termDivider)
	if plan.DryRun {
		fmt.Fprintf(b, "No changes have been applied.\n")
		fmt.Fprintf(b, "Run setup again without --dry-run to execute this plan.\n")
	} else {
		fmt.Fprintf(b, "Plan generated. Run 'dso setup --approve' to execute.\n")
	}
}

// opPrefix returns a Terraform-style single-character prefix for an operation.
func opPrefix(op string) string {
	switch op {
	case "create", "enable", "start", "add-member":
		return "+"
	case "modify":
		return "~"
	case "delete", "stop", "disable":
		return "-"
	default:
		return "+"
	}
}
