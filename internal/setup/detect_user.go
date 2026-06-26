package setup

import "os/user"

// detectUser returns facts about the user running the current process.
func detectUser() (UserInfo, []DetectionWarning) {
	u, err := user.Current()
	if err != nil {
		return UserInfo{}, []DetectionWarning{{
			Code:    "user_lookup_failed",
			Message: "cannot determine current user: " + err.Error(),
		}}
	}
	return UserInfo{
		Username: u.Username,
		UID:      u.Uid,
		GID:      u.Gid,
		HomeDir:  u.HomeDir,
		IsRoot:   u.Uid == "0",
	}, nil
}
