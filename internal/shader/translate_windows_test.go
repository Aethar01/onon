package shader

import "testing"

func TestWrappedShaderTranslatesToDXBC(t *testing.T) {
	spirv, err := SPIRV(Wrap(frosted))
	if err != nil {
		t.Fatal(err)
	}
	code, entry, err := Translate(spirv, FormatDXBC)
	if err != nil {
		t.Fatal(err)
	}
	// Direct3D's bytecode starts with its name.
	if len(code) < 4 || string(code[:4]) != "DXBC" || entry != "main" {
		t.Errorf("bytecode of %d bytes, entry %q", len(code), entry)
	}
}
