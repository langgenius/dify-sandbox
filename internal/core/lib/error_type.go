package lib

import (
	"fmt"
)

type ErrorCoder interface {
	GetCode() int
}

type BaseError struct {
	Err  error
	Code int
}

func (e *BaseError) Error() string {
	return fmt.Sprintf("error code %d: %v", e.Code, e.Err)
}

func (e *BaseError) Unwrap() error {
	return e.Err
}

func (e *BaseError) GetCode() int {
	return e.Code
}

type ChrootError struct {
	BaseError
}

func (e *ChrootError) Error() string {
	return fmt.Sprintf("chroot failed: %v", e.Err)
}

type ChdirError struct {
	BaseError
}

func (e *ChdirError) Error() string {
	return fmt.Sprintf("chdir failed: %v", e.Err)
}

type SetNoNewPrivsError struct {
	BaseError
}

func (e *SetNoNewPrivsError) Error() string {
	return fmt.Sprintf("set no new privs failed: %v", e.Err)
}

type SetuidError struct {
	BaseError
}

func (e *SetuidError) Error() string {
	return fmt.Sprintf("setuid failed: %v", e.Err)
}

type SetgidError struct {
	BaseError
}

func (e *SetgidError) Error() string {
	return fmt.Sprintf("setgid failed: %v", e.Err)
}

type SeccompError struct {
	BaseError
}

func (e *SeccompError) Error() string {
	return fmt.Sprintf("seccomp failed: %v", e.Err)
}

type SetgroupsError struct {
	BaseError
}

func (e *SetgroupsError) Error() string {
	return fmt.Sprintf("setgroups failed: %v", e.Err)
}
