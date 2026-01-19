package runner

import (
	"os"
	"os/exec"
	"path"

	"github.com/google/uuid"
)

type TempDirRunner struct{}

func (s *TempDirRunner) WithTempDir(basedir string, paths []string, closures func(path string) error) error {
	tmpDir, err := s.createTempDir(basedir)
	if err != nil {
		return err
	}

	if err := s.copyPaths(paths, tmpDir); err != nil {
		return err
	}

	if err := os.Chdir(tmpDir); err != nil {
		return err
	}

	return closures(tmpDir)
}

func (s *TempDirRunner) createTempDir(basedir string) (string, error) {
	uuid, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}

	tmpDir := path.Join(basedir, "tmp", "sandbox-"+uuid.String())
	if err := os.Mkdir(tmpDir, 0755); err != nil {
		return "", err
	}

	return tmpDir, nil
}

func (s *TempDirRunner) copyPaths(paths []string, tmpDir string) error {
	for _, filePath := range paths {
		if err := s.copySinglePath(filePath, tmpDir); err != nil {
			return err
		}
	}

	return nil
}

func (s *TempDirRunner) copySinglePath(filePath, tmpDir string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil
	}

	destPath := path.Join(tmpDir, filePath)
	if fileInfo.IsDir() {
		if err := os.MkdirAll(destPath, 0755); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(path.Dir(destPath), 0755); err != nil {
			return err
		}
	}

	return exec.Command("cp", "-r", filePath, destPath).Run()
}
