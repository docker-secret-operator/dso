package setup

// RepairProvider handles provider-related doctor checks.
//
// Credential management is never automated — credentials require explicit user
// action. DSO-DOCTOR-010 (unknown provider) requires a config edit that is
// outside the scope of safe automated repair.
// DSO-DOCTOR-011 (credentials missing) is explicitly excluded per spec.
type RepairProvider struct {
	provider string
}

func newRepairProvider(provider string) *RepairProvider {
	return &RepairProvider{provider: provider}
}

// planForCheck always returns nil — no provider issue has a safe automatic repair.
func (rp *RepairProvider) planForCheck(_ DoctorCheck) *RepairAction {
	return nil
}
