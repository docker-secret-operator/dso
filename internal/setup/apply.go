package setup

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"
)

// applyIface is the internal contract between the Engine and the apply layer.
// The real Applier and test stubs both satisfy this interface.
type applyIface interface {
	Apply(ctx context.Context, plan *InstallPlan) (*Transaction, error)
}

// executor is the internal contract for a single class of plan operations.
// Each implementation knows how to execute one type of change.
type executor interface {
	execute(ctx context.Context, tx *Transaction) error
}

// Applier executes an InstallPlan operation by operation inside a Transaction.
// It emits events for every operation state change; it never modifies the plan.
type Applier struct {
	emitter *Emitter
}

// newApplier constructs an Applier wired to the given event emitter.
func newApplier(emitter *Emitter) *Applier {
	return &Applier{emitter: emitter}
}

// Apply executes the plan in deterministic order:
// Directories → Files → Permissions → Services → Groups.
// The first failure aborts execution; rollback is Phase 7.
func (a *Applier) Apply(ctx context.Context, plan *InstallPlan) (*Transaction, error) {
	tx := newTransaction(plan.ID)
	a.emitter.emit(EventTransactionStarted, tx, nil)

	executors := []executor{
		newDirectoryExecutor(plan.Directories, a.emitter),
		newFileExecutor(plan.Files, a.emitter),
		newPermissionExecutor(plan.Permissions, a.emitter),
		newServiceExecutor(plan.Services, a.emitter),
		newGroupExecutor(plan.Groups, a.emitter),
	}

	for _, exec := range executors {
		if err := exec.execute(ctx, tx); err != nil {
			tx.Status = StatusFailed
			tx.EndTime = time.Now()
			a.emitter.emit(EventTransactionFailed, tx, err)
			return tx, err
		}
	}

	tx.Status = StatusCompleted
	tx.EndTime = time.Now()
	a.emitter.emit(EventTransactionCompleted, tx, nil)
	return tx, nil
}

// ─── Shared OS helpers ────────────────────────────────────────────────────────

// chownPath applies "user:group" ownership to a path.
// Accepts "user", "user:", or "user:group" formats.
// A no-op when owner is empty.
func chownPath(path, owner string) error {
	if owner == "" {
		return nil
	}
	parts := strings.SplitN(owner, ":", 2)
	u, err := user.Lookup(parts[0])
	if err != nil {
		return fmt.Errorf("lookup user %q: %w", parts[0], err)
	}
	var g *user.Group
	if len(parts) == 2 && parts[1] != "" {
		g, err = user.LookupGroup(parts[1])
		if err != nil {
			return fmt.Errorf("lookup group %q: %w", parts[1], err)
		}
	}
	uid, gid, err := parseOwnerIDs(u, g)
	if err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return os.Lchown(path, uid, gid)
}

// parseOwnerIDs converts user.User and optional user.Group to numeric uid/gid.
// Returns an error if either UID or GID string is not a valid integer.
func parseOwnerIDs(u *user.User, g *user.Group) (uid, gid int, err error) {
	uid, err = strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse UID %q: not a valid integer: %w", u.Uid, err)
	}
	if g == nil {
		return uid, -1, nil
	}
	gid, err = strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse GID %q: not a valid integer: %w", g.Gid, err)
	}
	return uid, gid, nil
}
