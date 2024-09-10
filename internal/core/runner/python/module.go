package python

import (
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
	"os/exec"
	"regexp"
	"strings"
)

type PyModules struct {
	name       string
	isImported bool
}

func checkNotInstallModuleWithCode(code string) string {
	var modules []PyModules
	// 使用正则表达式检查 import 语句
	importPattern := regexp.MustCompile(`(?m)^import\s+(\w+)`)
	fromImportPattern := regexp.MustCompile(`(?m)^from\s+(\w+)`)

	// 查找所有导入的模块
	matches := importPattern.FindAllStringSubmatch(code, -1)
	for _, match := range matches {
		if len(match) > 1 {
			modules = append(modules, PyModules{match[1], false})
		}
	}

	matches = fromImportPattern.FindAllStringSubmatch(code, -1)
	for _, match := range matches {
		if len(match) > 1 {
			modules = append(modules, PyModules{match[1], false})
		}
	}

	// 检查每个模块是否存在
	for i := range modules {
		if checkModuleExists(modules[i].name) {
			modules[i].isImported = true
		}
	}
	log.Info("%v\n", modules)
	return concatModules(modules)
}

func concatModules(modules []PyModules) string {
	var notImportedModules []string
	for _, m := range modules {
		if !m.isImported {
			notImportedModules = append(notImportedModules, m.name)
		}
	}

	return strings.Join(notImportedModules, " ")
}

// checkModuleExists 函数调用 Python 脚本检查模块是否存在
func checkModuleExists(module string) bool {
	configuration := static.GetDifySandboxGlobalConfigurations()
	cmd := exec.Command(configuration.PythonPath, "-c", fmt.Sprintf("import %s", module))
	err := cmd.Run()
	return err == nil
}
