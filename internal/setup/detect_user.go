package setup

import (
	"os/user"
)

// detectUser returns facts about the user running the current process.
func detectUser() UserInfo {
	u, err := user.Current()
	if err != nil {
		return UserInfo{}
	}
	return UserInfo{
		Username: u.Username,
		UID:      u.Uid,
		GID:      u.Gid,
		HomeDir:  u.HomeDir,
		IsRoot:   u.Uid == "0",
	}
}
