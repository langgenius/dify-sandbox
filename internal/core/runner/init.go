package runner

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

func init() {
	// create sandbox user
	user := static.SANDBOX_USER
	uid := static.SANDBOX_USER_UID

	// check if user exists
	_, err := exec.Command("id", user).Output()
	if err != nil {
		// create user
		output, err := exec.Command("bash", "-c", "useradd -u "+strconv.Itoa(uid)+" "+user).Output()
		if err != nil {
			log.Panic("failed to create user: %v, %v", err, string(output))
		}
	}

	// get gid of sandbox user and setgid
	gid, err := exec.Command("id", "-g", static.SANDBOX_USER).Output()
	if err != nil {
		log.Panic("failed to get gid of user: %v", err)
	}

	static.SANDBOX_GROUP_ID, err = strconv.Atoi(strings.TrimSpace(string(gid)))
	if err != nil {
		log.Panic("failed to convert gid: %v", err)
	}
}
