package runner

import (
	"os"
	"os/exec"
	"path"

	"github.com/google/uuid"
)

type TempDirRunner struct{}

func (s *TempDirRunner) WithTempDir(basedir string, paths []string, closures func(path string) error) error {
	uuid, err := uuid.NewRandom()
	if err != nil {
		return err
	}

	// create a tmp dir
	tmp_dir := path.Join(basedir, "tmp", "sandbox-"+uuid.String())
	err = os.Mkdir(tmp_dir, 0755)
	if err != nil {
		return err
	}

	// copy files to tmp dir
	for _, file_path := range paths {
		// create path in tmp dir
		// check if it's a dir
		file_info, err := os.Stat(file_path)
		if err != nil {
			continue
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
	err = os.Chdir(tmp_dir)
	if err != nil {
		return err
	}

	err = closures(tmp_dir)
	if err != nil {
		return err
	}

	return nil
}
