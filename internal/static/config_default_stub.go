//go:build !linux

package static

// Default Python lib paths for non-Linux platforms (not used, but needed for compilation)
var DEFAULT_PYTHON_LIB_REQUIREMENTS = []string{}
