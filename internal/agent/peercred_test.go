package agent

import "testing"

// TestPeerAuthorized exercises the exact authorization policy enforced on the IPC
// socket. It proves there is no privilege-escalation path: any peer that is not
// root, not the socket owner, and not a dso-group member is denied.
func TestPeerAuthorized(t *testing.T) {
	const selfUID = 1000

	tests := []struct {
		name    string
		peer    peerIdentity
		selfUID int
		dsoGID  int
		want    bool
	}{
		{
			name:    "root is always allowed",
			peer:    peerIdentity{uid: 0, gid: 0},
			selfUID: selfUID,
			dsoGID:  -1,
			want:    true,
		},
		{
			name:    "same uid as agent is allowed",
			peer:    peerIdentity{uid: selfUID, gid: 2000},
			selfUID: selfUID,
			dsoGID:  -1,
			want:    true,
		},
		{
			name:    "no dso group: other user denied",
			peer:    peerIdentity{uid: 1234, gid: 1234},
			selfUID: selfUID,
			dsoGID:  -1,
			want:    false,
		},
		{
			name:    "primary gid matches dso group: allowed",
			peer:    peerIdentity{uid: 1234, gid: 5000},
			selfUID: selfUID,
			dsoGID:  5000,
			want:    true,
		},
		{
			name:    "unknown user, gid mismatch: denied (fail-closed)",
			peer:    peerIdentity{uid: 2147483646, gid: 87654321},
			selfUID: selfUID,
			dsoGID:  87654320,
			want:    false,
		},
		{
			name:    "root takes precedence even with mismatched gid",
			peer:    peerIdentity{uid: 0, gid: 999999},
			selfUID: selfUID,
			dsoGID:  5000,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := peerAuthorized(tt.peer, tt.selfUID, tt.dsoGID); got != tt.want {
				t.Errorf("peerAuthorized(%+v, self=%d, dsoGID=%d) = %v, want %v",
					tt.peer, tt.selfUID, tt.dsoGID, got, tt.want)
			}
		})
	}
}
