package shader

import (
	"errors"
	"syscall"
	"unsafe"
)

// Direct3D's compiler, which Windows has.
var d3dCompile = syscall.NewLazyDLL("d3dcompiler_47.dll").NewProc("D3DCompile")

// blob is an ID3DBlob, whose methods follow IUnknown's three.
type blob struct{ vtbl *[5]uintptr }

func (b *blob) bytes() []byte {
	data, _, _ := syscall.SyscallN(b.vtbl[3], uintptr(unsafe.Pointer(b)))
	size, _, _ := syscall.SyscallN(b.vtbl[4], uintptr(unsafe.Pointer(b)))
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(data)), int(size))...)
}

func (b *blob) release() { syscall.SyscallN(b.vtbl[2], uintptr(unsafe.Pointer(b))) }

func translate(spirv []uint32, format Format) ([]byte, string, error) {
	if format != FormatDXBC {
		return nil, "", errors.New("the GPU takes shaders in a form Windows has no translator for")
	}
	hlsl, err := cross(spirv, true)
	if err != nil {
		return nil, "", err
	}
	if err := d3dCompile.Find(); err != nil {
		return nil, "", err
	}
	source := []byte(hlsl)
	entry, target := []byte("main\x00"), []byte("ps_5_1\x00")
	var code, errors_ *blob
	hr, _, _ := d3dCompile.Call(
		uintptr(unsafe.Pointer(&source[0])), uintptr(len(source)), 0, 0, 0,
		uintptr(unsafe.Pointer(&entry[0])), uintptr(unsafe.Pointer(&target[0])), 0, 0,
		uintptr(unsafe.Pointer(&code)), uintptr(unsafe.Pointer(&errors_)))
	if errors_ != nil {
		defer errors_.release()
	}
	if int32(hr) < 0 || code == nil {
		if errors_ != nil {
			return nil, "", compilerError(string(errors_.bytes()))
		}
		return nil, "", errors.New("Direct3D could not compile the shader")
	}
	defer code.release()
	return code.bytes(), "main", nil
}
