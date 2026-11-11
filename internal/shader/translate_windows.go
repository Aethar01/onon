package shader

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// Direct3D 12 takes a shader as DXIL, which Microsoft's DirectX Shader
// Compiler makes of HLSL. It is not part of Windows, so dxcompiler.dll is
// shipped beside onon.
var dxcCreateInstance = syscall.NewLazyDLL("dxcompiler.dll").NewProc("DxcCreateInstance")

var (
	clsidDxcCompiler = syscall.GUID{Data1: 0x73e22d93, Data2: 0xe6ce, Data3: 0x47f3, Data4: [8]byte{0xb5, 0xbf, 0xf0, 0x66, 0x4f, 0x39, 0xc1, 0xb0}}
	iidDxcCompiler3  = syscall.GUID{Data1: 0x228b4687, Data2: 0x5a6a, Data3: 0x4730, Data4: [8]byte{0x90, 0x0c, 0x97, 0x02, 0xb2, 0x20, 0x3f, 0x54}}
	iidDxcResult     = syscall.GUID{Data1: 0x58346cda, Data2: 0xdde7, Data3: 0x4497, Data4: [8]byte{0x94, 0x61, 0x6f, 0x87, 0xaf, 0x5e, 0x06, 0x59}}
)

// comObject is a COM object, called through its table of methods, the
// first three of which are IUnknown's.
type comObject struct{ vtbl *[8]uintptr }

func (o *comObject) call(method int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comObject) release() { o.call(2) }

// bytes is what a blob holds: IDxcBlob's methods after IUnknown's are
// GetBufferPointer and GetBufferSize.
func (o *comObject) bytes() []byte {
	data, size := o.call(3), o.call(4)
	// The address is the blob's, not Go's, which vet cannot tell.
	return append([]byte(nil), unsafe.Slice(*(**byte)(unsafe.Pointer(&data)), int(size))...)
}

// dxcBuffer is a DxcBuffer: source text and its encoding.
type dxcBuffer struct {
	ptr      unsafe.Pointer
	size     uintptr
	encoding uint32
}

const dxcUTF8 = 65001

func failed(hr uintptr) bool { return int32(hr) < 0 }

func translate(spirv []uint32, format Format) ([]byte, string, error) {
	if format != FormatDXIL {
		return nil, "", errors.New("the GPU takes shaders in a form Windows has no translator for")
	}
	hlsl, err := cross(spirv, true)
	if err != nil {
		return nil, "", err
	}
	if err := dxcCreateInstance.Find(); err != nil {
		return nil, "", fmt.Errorf("the DirectX Shader Compiler is missing: %w", err)
	}
	var compiler *comObject
	if hr, _, _ := dxcCreateInstance.Call(uintptr(unsafe.Pointer(&clsidDxcCompiler)), uintptr(unsafe.Pointer(&iidDxcCompiler3)), uintptr(unsafe.Pointer(&compiler))); failed(hr) || compiler == nil {
		return nil, "", fmt.Errorf("cannot start the DirectX Shader Compiler (0x%08x)", uint32(hr))
	}
	defer compiler.release()

	source := []byte(hlsl)
	buffer := dxcBuffer{ptr: unsafe.Pointer(&source[0]), size: uintptr(len(source)), encoding: dxcUTF8}
	var args []*uint16
	for _, arg := range []string{"-E", "main", "-T", "ps_6_0"} {
		p, _ := syscall.UTF16PtrFromString(arg)
		args = append(args, p)
	}
	// IDxcCompiler3's first method of its own is Compile.
	var result *comObject
	hr := compiler.call(3, uintptr(unsafe.Pointer(&buffer)), uintptr(unsafe.Pointer(&args[0])), uintptr(len(args)), 0,
		uintptr(unsafe.Pointer(&iidDxcResult)), uintptr(unsafe.Pointer(&result)))
	runtime.KeepAlive(source)
	runtime.KeepAlive(args)
	if failed(hr) || result == nil {
		return nil, "", fmt.Errorf("the DirectX Shader Compiler failed (0x%08x)", uint32(hr))
	}
	defer result.release()

	// IDxcOperationResult's are GetStatus, GetResult and GetErrorBuffer.
	var status int32
	result.call(3, uintptr(unsafe.Pointer(&status)))
	if status < 0 {
		var log *comObject
		if result.call(5, uintptr(unsafe.Pointer(&log))); log != nil {
			defer log.release()
			return nil, "", compilerError(string(log.bytes()))
		}
		return nil, "", errors.New("Direct3D could not compile the shader")
	}
	var code *comObject
	if hr := result.call(4, uintptr(unsafe.Pointer(&code))); failed(hr) || code == nil {
		return nil, "", errors.New("Direct3D compiled no shader")
	}
	defer code.release()
	return code.bytes(), "main", nil
}
