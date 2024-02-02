package runner

import (
	"os"
	"os/exec"
	"path"
	"syscall"

	"github.com/google/uuid"
)

type SeccompRunner struct {
}

func (s *SeccompRunner) WithTempDir(paths []string, closures func() error) error {
	uuid, err := uuid.NewRandom()
	if err != nil {
		return err
	}

	// create a tmp dir
	tmp_dir := path.Join("/tmp", "sandbox-"+uuid.String())
	err = os.Mkdir(tmp_dir, 0755)
	if err != nil {
		return err
	}
	defer func() {
		os.RemoveAll(tmp_dir)
		os.Remove(tmp_dir)
	}()

	// copy files to tmp dir
	for _, file_path := range paths {
		// create path in tmp dir
		// check if it's a dir
		file_info, err := os.Stat(file_path)
		if err != nil {
			return err
		}

		if file_info.IsDir() {
			err = os.MkdirAll(path.Join(tmp_dir, file_path), 0755)
			if err != nil {
				return err
			}
		} else {
			err = os.MkdirAll(path.Join(tmp_dir, path.Dir(file_path)), 0755)
			if err != nil {
				return err
			}
		}

		err = exec.Command("cp", "-r", file_path, path.Join(tmp_dir, file_path)).Run()
		if err != nil {
			return err
		}
	}

	// chdir
	err = syscall.Chdir(tmp_dir)
	if err != nil {
		return err
	}

	err = closures()
	if err != nil {
		return err
	}

	return nil
}
