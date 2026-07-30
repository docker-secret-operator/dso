package setup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// groupRollback undoes group_create and group_add-member operations.
type groupRollback struct {
	emitter      *Emitter
	groupDel     func(name string) error
	removeMember func(group, username string) error
}

func newGroupRollback(emitter *Emitter) *groupRollback {
	return &groupRollback{
		emitter:      emitter,
		groupDel:     groupDelOS,
		removeMember: removeMemberOS,
	}
}

func (r *groupRollback) rollback(_ context.Context, op *TxOperation) error {
	before, ok := op.Before.(*GroupSnapshot)
	if !ok || op.Before == nil {
		return fmt.Errorf("missing GroupSnapshot for %s", op.OperID)
	}

	opName := strings.TrimPrefix(op.Type, "group_")
	switch opName {
	case "create":
		if !before.Existed {
			if err := r.groupDel(op.Target); err != nil {
				return fmt.Errorf("rollback group delete %s: %w", op.Target, err)
			}
		}
		return nil
	case "add-member":
		after, ok := op.After.(*GroupSnapshot)
		if !ok || op.After == nil {
			return fmt.Errorf("missing After GroupSnapshot for add-member rollback on %s", op.OperID)
		}
		// Remove members that were added: those in After but not in Before.
		beforeSet := make(map[string]bool, len(before.Members))
		for _, m := range before.Members {
			beforeSet[m] = true
		}
		for _, m := range after.Members {
			if !beforeSet[m] {
				if err := r.removeMember(op.Target, m); err != nil {
					return fmt.Errorf("rollback remove member %s from %s: %w", m, op.Target, err)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown group operation for rollback: %s", op.Type)
	}
}

func groupDelOS(name string) error {
	// #nosec G204 -- name comes from the local InstallPlan being rolled
	// back (the same plan that created it), not remote/attacker input
	return exec.Command("groupdel", name).Run()
}

func removeMemberOS(group, username string) error {
	// #nosec G204 -- group/username come from the local InstallPlan/OS-validated
	// account being rolled back, not remote/attacker input
	return exec.Command("gpasswd", "--delete", username, group).Run()
}
