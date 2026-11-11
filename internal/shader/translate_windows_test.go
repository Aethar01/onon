package shader

import (
	"bytes"
	"testing"
)

func TestWrappedShaderTranslatesToDXIL(t *testing.T) {
	if err := dxcCreateInstance.Find(); err != nil {
		t.Skipf("no DirectX Shader Compiler: %v", err)
	}
	spirv, err := SPIRV(Wrap(frosted))
	if err != nil {
		t.Fatal(err)
	}
	code, entry, err := Translate(spirv, FormatDXIL)
	if err != nil {
		t.Fatal(err)
	}
	// A compiled shader is a container, which starts with DXBC whatever it
	// holds, and holds DXIL.
	if len(code) < 4 || string(code[:4]) != "DXBC" || entry != "main" {
		t.Fatalf("bytecode of %d bytes, entry %q", len(code), entry)
	}
	if !bytes.Contains(code, []byte("DXIL")) {
		t.Error("the bytecode holds no DXIL")
	}
}
