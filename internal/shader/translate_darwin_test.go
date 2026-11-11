package shader

import (
	"strings"
	"testing"
)

func TestWrappedShaderTranslatesToMSL(t *testing.T) {
	spirv, err := SPIRV(Wrap(frosted))
	if err != nil {
		t.Fatal(err)
	}
	code, entry, err := Translate(spirv, FormatMSL)
	if err != nil {
		t.Fatal(err)
	}
	msl := string(code)
	// SDL binds the texture drawn, its sampler and the uniforms at 0.
	for _, want := range []string{"fragment ", entry + "(", "[[texture(0)]]", "[[sampler(0)]]", "[[buffer(0)]]", "[[user(locn0)]]", "[[user(locn1)]]"} {
		if !strings.Contains(msl, want) {
			t.Errorf("MSL lacks %q:\n%s", want, msl)
		}
	}
}
