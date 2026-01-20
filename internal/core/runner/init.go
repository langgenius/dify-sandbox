package runner

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"

	"github.com/langgenius/dify-sandbox/internal/static"
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
			slog.Error("failed to create user", "err", err, "output", string(output))
			panic(fmt.Sprintf("failed to create user: %v, %v", err, string(output)))
		}
	}

	// get gid of sandbox user and setgid
	gid, err := exec.Command("id", "-g", static.SANDBOX_USER).Output()
	if err != nil {
		slog.Error("failed to get gid of user", "err", err)
		panic(fmt.Sprintf("failed to get gid of user: %v", err))
	}

	static.SANDBOX_GROUP_ID, err = strconv.Atoi(strings.TrimSpace(string(gid)))
	if err != nil {
		slog.Error("failed to convert gid", "err", err)
		panic(fmt.Sprintf("failed to convert gid: %v", err))
	}
}
