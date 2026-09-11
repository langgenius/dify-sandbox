package runner

import (
	"os"
	"os/exec"
	"path"

	"github.com/google/uuid"
)

type TempDirRunner struct{}

// PrepareTempDir creates an isolated sandbox root and copies required paths into it
// without changing the process-wide working directory. Callers that spawn subprocesses
// should set cmd.Dir to the returned path so each run keeps its own chroot root.
func (s *TempDirRunner) PrepareTempDir(basedir string, paths []string) (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}

	tmpDir := path.Join(basedir, "tmp", "sandbox-"+id.String())
	if err := os.Mkdir(tmpDir, 0755); err != nil {
		return "", err
	}

	for _, file_path := range paths {
		file_info, err := os.Stat(file_path)
		if err != nil {
			continue
		}

		if file_info.IsDir() {
			err = os.MkdirAll(path.Join(tmpDir, file_path), 0755)
			if err != nil {
				return "", err
			}
		} else {
			err = os.MkdirAll(path.Join(tmpDir, path.Dir(file_path)), 0755)
			if err != nil {
				return "", err
			}
		}

		err = exec.Command("cp", "-r", file_path, path.Join(tmpDir, file_path)).Run()
		if err != nil {
			return "", err
		}
	}

	return tmpDir, nil
}

func (s *TempDirRunner) WithTempDir(basedir string, paths []string, closures func(path string) error) error {
	tmpDir, err := s.PrepareTempDir(basedir, paths)
	if err != nil {
		return err
	}

	// chdir
	err = os.Chdir(tmpDir)
	if err != nil {
		return err
	}

	err = closures(tmpDir)
	if err != nil {
		return err
	}

	return nil
}
