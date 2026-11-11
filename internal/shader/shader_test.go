package shader

import (
	"strings"
	"testing"
)

const frosted = `
void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	vec2 uv = fragCoord / iResolution.xy;
	fragColor = texture(iChannel0, uv) * (0.9 + 0.1 * sin(iTime));
}
`

func TestWrappedShaderCompilesToSPIRV(t *testing.T) {
	spirv, err := SPIRV(Wrap(frosted))
	if err != nil {
		t.Fatal(err)
	}
	code, entry, err := Translate(spirv, FormatSPIRV)
	if err != nil {
		t.Fatal(err)
	}
	// SPIR-V starts with its magic number.
	if len(code) != 4*len(spirv) || [4]byte(code[:4]) != [4]byte{0x03, 0x02, 0x23, 0x07} || entry != "main" {
		t.Errorf("SPIR-V of %d bytes starting % x, entry %q", len(code), code[:4], entry)
	}
}

func TestShaderErrorNamesTheThemesLine(t *testing.T) {
	_, err := SPIRV(Wrap("void mainImage(out vec4 c, in vec2 p) {\n\tc = vec4(nope);\n}\n"))
	if err == nil {
		t.Fatal("no error for an undeclared name")
	}
	if !strings.Contains(err.Error(), "0:2:") || !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name line 2 and the name", err)
	}
}

func TestUniformsPackAsTheBlock(t *testing.T) {
	b := Uniforms{Width: 1, Alt: true}.Bytes()
	if got := [4]byte(b[0:4]); got != [4]byte{0, 0, 0x80, 0x3f} {
		t.Errorf("width packed as %v", got)
	}
	if got := [4]byte(b[36:40]); got != [4]byte{0, 0, 0x80, 0x3f} {
		t.Errorf("alt packed as %v", got)
	}
}
