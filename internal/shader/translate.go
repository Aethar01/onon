package shader

import (
	"errors"
	"unsafe"
)

// Format is a form a GPU takes a shader in.
type Format int

const (
	FormatSPIRV Format = iota // SPIR-V, for Vulkan
	FormatMSL                 // Metal's shading language
	FormatDXBC                // Direct3D's bytecode
)

// Translate is a fragment shader's SPIR-V in format, with the name of the
// function it starts at.
func Translate(spirv []uint32, format Format) (code []byte, entry string, err error) {
	if len(spirv) == 0 {
		return nil, "", errors.New("no shader")
	}
	if format == FormatSPIRV {
		return unsafe.Slice((*byte)(unsafe.Pointer(&spirv[0])), 4*len(spirv)), "main", nil
	}
	return translate(spirv, format)
}
