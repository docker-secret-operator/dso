package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// GroupExecutor creates Unix groups and manages memberships as declared in the InstallPlan.
// OS interactions are injected via function fields so tests never require root.
type GroupExecutor struct {
	ops     []GroupChange
	emitter *Emitter
	// Injectable OS hooks — defaults call real system commands.
	groupExists func(name string) (bool, error)
	getMembers  func(name string) ([]string, error)
	createGroup func(name string) error
	addMember   func(group, username string) error
}

func newGroupExecutor(ops []GroupChange, emitter *Emitter) *GroupExecutor {
	return &GroupExecutor{
		ops:         ops,
		emitter:     emitter,
		groupExists: groupExistsOS,
		getMembers:  getGroupMembersOS,
		createGroup: groupAddOS,
		addMember:   groupAddMemberOS,
	}
}

func (e *GroupExecutor) execute(ctx context.Context, tx *Transaction) error {
	for i := range e.ops {
		if err := e.executeOne(ctx, &e.ops[i], tx); err != nil {
			return err
		}
	}
	return nil
}

func (e *GroupExecutor) executeOne(_ context.Context, op *GroupChange, tx *Transaction) error {
	txOp := appendOperation(tx, op.ID, "group_"+op.Operation, op.Name)
	markRunning(txOp)
	e.emitter.emit(EventOperationStarted, txOp, nil)

	existed, err := e.groupExists(op.Name)
	if err != nil {
		markFailed(txOp, err)
		e.emitter.emit(EventOperationFailed, txOp, err)
		return fmt.Errorf("%s: group exists check %s: %w", op.ID, op.Name, err)
	}
	var currentMembers []string
	if existed {
		currentMembers, _ = e.getMembers(op.Name)
	}
	txOp.Before = &GroupSnapshot{Existed: existed, Members: currentMembers}

	var execErr error
	switch op.Operation {
	case "create":
		if !existed {
			execErr = e.createGroup(op.Name)
		}
	case "add-member":
		for _, u := range op.Users {
			if err := e.addMember(op.Name, u); err != nil {
				execErr = err
				break
			}
		}
	default:
		execErr = fmt.Errorf("unknown group operation %q", op.Operation)
	}

	if execErr != nil {
		markFailed(txOp, execErr)
		e.emitter.emit(EventOperationFailed, txOp, execErr)
		return fmt.Errorf("%s: group %s %s: %w", op.ID, op.Operation, op.Name, execErr)
	}

	txOp.After = &GroupSnapshot{Existed: true}
	markCompleted(txOp)
	e.emitter.emit(EventOperationCompleted, txOp, nil)
	return nil
}

// ─── Real OS helpers ──────────────────────────────────────────────────────────

// groupExistsOS returns (true, nil) when the group exists, (false, nil) when it
// doesn't, and (false, err) for any real lookup error (e.g. NSS misconfiguration).
func groupExistsOS(name string) (bool, error) {
	_, err := user.LookupGroup(name)
	if err == nil {
		return true, nil
	}
	if _, ok := err.(user.UnknownGroupError); ok {
		return false, nil
	}
	return false, err
}

// getGroupMembersOS reads the current members of a Unix group from /etc/group.
func getGroupMembersOS(name string) ([]string, error) {
	content, err := os.ReadFile("/etc/group")
	if err != nil {
		return nil, fmt.Errorf("read /etc/group: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 4 || parts[0] != name {
			continue
		}
		var members []string
		for _, m := range strings.Split(parts[3], ",") {
			if m != "" {
				members = append(members, m)
			}
		}
		return members, nil
	}
	return nil, nil
}

func groupAddOS(name string) error {
	return exec.Command("groupadd", name).Run()
}

func groupAddMemberOS(group, username string) error {
	return exec.Command("usermod", "-aG", group, username).Run()
}
