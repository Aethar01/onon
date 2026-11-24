//go:build windows

package platform

import (
	"syscall"

	"github.com/ebitengine/purego"
)

// LoadSDLSymbol points dst, a pointer to a func variable, at the SDL function
// name, for functions the SDL binding leaves out.
func LoadSDLSymbol(name string, dst any) error {
	handle, err := syscall.LoadLibrary("SDL3.dll")
	if err != nil {
		return err
	}
	sym, err := syscall.GetProcAddress(handle, name)
	if err != nil {
		return err
	}
	purego.RegisterFunc(dst, sym)
	return nil
}
